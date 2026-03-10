package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
)

var (
	sqlInline     string
	sqlFile       string
	sqlImage      string
	sqlJobTimeout int
)

var sqlCmd = &cobra.Command{
	Use:   "sql <app-identifier>",
	Short: "Run SQL against a discovered PostgreSQL database in Kubernetes",
	Long: `Run SQL against a PostgreSQL database from inside the Kubernetes cluster.

The command will:
  1. Discover the application pod in Kubernetes
  2. Extract database connection info from environment variables
  3. Create a Kubernetes Job that runs psql in the cluster
  4. Wait for the Job to finish
  5. Leave the Job in place so you can inspect the output with kubectl logs

Examples:
  # Run inline SQL
  pgsnap sql yamtrack --sql "SELECT datname FROM pg_database;"

  # Run SQL from file
  pgsnap sql yamtrack --file ./query.sql

  # Run piped SQL
  cat ./query.sql | pgsnap sql yamtrack`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSQL,
}

func init() {
	sqlCmd.Flags().StringVar(&sqlInline, "sql", "", "Inline SQL to execute")
	sqlCmd.Flags().StringVarP(&sqlFile, "file", "f", "", "Path to a SQL file to execute")
	sqlCmd.Flags().StringVar(&sqlImage, "image", "postgres:16-alpine", "PostgreSQL container image")
	sqlCmd.Flags().IntVar(&sqlJobTimeout, "job-timeout", 300, "Job timeout in seconds")

	rootCmd.AddCommand(sqlCmd)
}

func runSQL(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	var appIdentifier string
	var err error
	if podName != "" {
		appIdentifier = "custom"
	} else {
		appIdentifier, err = getAppIdentifier(args)
		if err != nil {
			return err
		}
	}

	sqlData, err := readSQLInput(sqlInline, sqlFile, os.Stdin, stdinProvided(os.Stdin))
	if err != nil {
		return err
	}

	fmt.Printf("🔍 Starting SQL job for: %s\n", appIdentifier)
	fmt.Println()

	fmt.Println("📦 Connecting to Kubernetes...")
	client, restConfig, defaultNamespace, err := k8s.NewClient(globalConfig.Kubeconfig, globalConfig.Context)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	if globalConfig.Namespace == "" {
		globalConfig.Namespace = defaultNamespace
	}

	fmt.Printf("   Namespace: %s\n", globalConfig.Namespace)
	fmt.Println()

	fmt.Println("🔎 Discovering pod...")
	discovery := k8s.NewDiscovery(client, globalConfig.Namespace, appIdentifier, podName)
	pod, err := discovery.FindPod(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover pod: %w", err)
	}

	fmt.Printf("   Found pod: %s\n", pod.Name)
	fmt.Println()

	fmt.Println("🔐 Extracting database configuration...")
	envVars, err := k8s.ExtractEnvVars(ctx, client, pod)
	if err != nil {
		return fmt.Errorf("failed to extract environment variables: %w", err)
	}

	dbConfig, err := postgres.ParseFromEnvVars(envVars)
	if err != nil {
		return fmt.Errorf("failed to parse database config: %w", err)
	}

	fmt.Printf("   Host: %s:%d\n", dbConfig.Host, dbConfig.Port)
	fmt.Printf("   Database: %s\n", dbConfig.Database)
	fmt.Printf("   User: %s\n", dbConfig.User)
	fmt.Println()

	fmt.Println("🚀 Creating SQL job in cluster...")
	job, err := k8s.CreateSQLJob(ctx, client, restConfig, globalConfig.Namespace, dbConfig, sqlData, sqlImage)
	if err != nil {
		return fmt.Errorf("failed to create SQL job: %w", err)
	}

	fmt.Printf("   Job created: %s\n", job.Name())
	fmt.Printf("   ConfigMap: %s\n", job.ConfigMapName())
	fmt.Println()

	fmt.Println("⏳ Waiting for SQL job to complete...")
	timeout := time.Duration(sqlJobTimeout) * time.Second
	if err := job.WaitForCompletion(ctx, timeout); err != nil {
		logs, logErr := job.GetPodLogs(ctx)
		if logErr == nil && strings.TrimSpace(logs) != "" {
			fmt.Println()
			fmt.Println("📄 Job logs")
			fmt.Println(logs)
		}

		fmt.Println("🧹 Resources preserved for inspection")
		fmt.Printf("   View logs: kubectl logs job/%s -n %s\n", job.Name(), globalConfig.Namespace)
		fmt.Printf("   Cleanup: kubectl delete job/%s configmap/%s -n %s\n", job.Name(), job.ConfigMapName(), globalConfig.Namespace)

		return fmt.Errorf("sql execution failed: %w", err)
	}

	fmt.Println("   ✓ SQL job completed successfully")
	fmt.Println()
	fmt.Println("✅ SQL job finished")
	fmt.Printf("   View logs: kubectl logs job/%s -n %s\n", job.Name(), globalConfig.Namespace)
	fmt.Printf("   Cleanup: kubectl delete job/%s configmap/%s -n %s\n", job.Name(), job.ConfigMapName(), globalConfig.Namespace)

	return nil
}

func readSQLInput(inlineSQL, filePath string, stdin io.Reader, stdinIsPipe bool) ([]byte, error) {
	sourceCount := 0
	if strings.TrimSpace(inlineSQL) != "" {
		sourceCount++
	}
	if filePath != "" {
		sourceCount++
	}
	if stdinIsPipe {
		sourceCount++
	}

	if sourceCount == 0 {
		return nil, fmt.Errorf("provide SQL via --sql, --file, or piped stdin")
	}
	if sourceCount > 1 {
		return nil, fmt.Errorf("provide SQL from exactly one source: --sql, --file, or piped stdin")
	}

	var (
		sqlData []byte
		err     error
	)

	switch {
	case strings.TrimSpace(inlineSQL) != "":
		sqlData = []byte(inlineSQL)
	case filePath != "":
		sqlData, err = os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read SQL file: %w", err)
		}
	case stdinIsPipe:
		sqlData, err = io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read SQL from stdin: %w", err)
		}
	}

	if strings.TrimSpace(string(sqlData)) == "" {
		return nil, fmt.Errorf("sql input is empty")
	}

	return sqlData, nil
}

func stdinProvided(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice == 0
}
