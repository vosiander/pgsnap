package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
)

var infoCmd = &cobra.Command{
	Use:   "info <app-identifier>",
	Short: "Show information about discovered pod and database configuration",
	Long: `Display information about the discovered pod and database configuration.

This is useful for debugging discovery issues and verifying database connection details.

Examples:
  # Show info for yamtrack
  pgsnap info yamtrack

  # Show info for specific namespace
  pgsnap info yamtrack --namespace production

  # Show info for specific pod
  pgsnap info --pod yamtrack-deployment-abc123`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInfo,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get app identifier (or use pod name if specified)
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

	fmt.Printf("ℹ️  Information for: %s\n", appIdentifier)
	fmt.Println()

	// Create Kubernetes client
	fmt.Println("📦 Kubernetes Configuration")
	client, _, defaultNamespace, err := k8s.NewClient(globalConfig.Kubeconfig, globalConfig.Context)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Use default namespace if not specified
	if globalConfig.Namespace == "" {
		globalConfig.Namespace = defaultNamespace
	}

	fmt.Printf("   Kubeconfig: %s\n", globalConfig.Kubeconfig)
	if globalConfig.Context != "" {
		fmt.Printf("   Context: %s\n", globalConfig.Context)
	}
	fmt.Printf("   Namespace: %s\n", globalConfig.Namespace)
	fmt.Println()

	// Discover pod
	fmt.Println("🔎 Pod Discovery")
	discovery := k8s.NewDiscovery(client, globalConfig.Namespace, appIdentifier, podName)
	pod, err := discovery.FindPod(ctx)
	if err != nil {
		fmt.Printf("   ✗ Failed to discover pod: %v\n", err)
		return err
	}

	fmt.Printf("   ✓ Pod found: %s\n", pod.Name)
	fmt.Printf("   Status: %s\n", pod.Status.Phase)
	if len(pod.Spec.Containers) > 0 {
		fmt.Printf("   Container: %s\n", pod.Spec.Containers[0].Name)
		fmt.Printf("   Image: %s\n", pod.Spec.Containers[0].Image)
	}
	fmt.Println()

	// Extract environment variables
	fmt.Println("🔐 Database Configuration")
	envVars, err := k8s.ExtractEnvVars(ctx, client, pod)
	if err != nil {
		fmt.Printf("   ✗ Failed to extract environment variables: %v\n", err)
		return err
	}

	// Parse database config
	dbConfig, err := postgres.ParseFromEnvVars(envVars)
	if err != nil {
		fmt.Printf("   ✗ Failed to parse database config: %v\n", err)
		fmt.Println()
		fmt.Println("   Available environment variables:")
		for key := range envVars {
			if containsDBKeyword(key) {
				fmt.Printf("     - %s\n", key)
			}
		}
		return err
	}

	fmt.Printf("   ✓ Configuration found\n")
	fmt.Printf("   Host: %s\n", dbConfig.Host)
	fmt.Printf("   Port: %d\n", dbConfig.Port)
	fmt.Printf("   Database: %s\n", dbConfig.Database)
	fmt.Printf("   User: %s\n", dbConfig.User)
	fmt.Printf("   SSL Mode: %s\n", dbConfig.SSLMode)
	fmt.Println()

	// Connection type
	fmt.Println("🔌 Connection Type")
	if isClusterInternal(dbConfig.Host) {
		fmt.Println("   Internal (cluster service)")
		fmt.Printf("   Service: %s\n", dbConfig.Host)
	} else {
		fmt.Println("   External (outside cluster)")
		fmt.Printf("   Endpoint: %s:%d\n", dbConfig.Host, dbConfig.Port)
	}
	fmt.Println()

	// Tool paths
	fmt.Println("🛠️  Tool Configuration")
	fmt.Printf("   pg_dump: %s\n", globalConfig.PgDumpPath)
	fmt.Printf("   psql: %s\n", globalConfig.PsqlPath)
	fmt.Printf("   Backup dir: %s\n", globalConfig.OutputDir)
	fmt.Println()

	fmt.Println("✅ All checks passed!")

	return nil
}

func containsDBKeyword(key string) bool {
	keywords := []string{"DB", "DATABASE", "POSTGRES", "PG", "SQL"}
	for _, keyword := range keywords {
		if contains(key, keyword) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isClusterInternal(host string) bool {
	// Check if host is a Kubernetes service
	return contains(host, ".svc.cluster.local") ||
		contains(host, ".svc") ||
		!contains(host, ".")
}
