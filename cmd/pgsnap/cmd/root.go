package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/common"
)

var (
	// Version info - set via ldflags at build time
	version = "dev"
	commit  = "none"
	date    = "unknown"

	globalConfig *common.GlobalConfig

	// Global flags
	kubeconfig string
	k8sContext string
	namespace  string
	podName    string
)

var rootCmd = &cobra.Command{
	Use:   "pgsnap",
	Short: "PostgreSQL backup and restore tool for Kubernetes",
	Long: `pgsnap is a CLI tool for backing up and restoring PostgreSQL databases
running in Kubernetes clusters. It automatically discovers pods and extracts
database connection information from environment variables.

Usage:
  pgsnap backup <app-identifier>
  pgsnap restore <app-identifier> --file <backup.zip>
  pgsnap sql <app-identifier> --sql "SELECT 1"
  pgsnap list <app-identifier>
  pgsnap info <app-identifier>`,
	Version: version,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("pgsnap %s (commit: %s, built: %s)\n", version, commit, date))
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	rootCmd.PersistentFlags().StringVar(&k8sContext, "context", "", "Kubernetes context to use")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	rootCmd.PersistentFlags().StringVar(&podName, "pod", "", "Exact pod name (skips auto-discovery)")
}

func initConfig() {
	globalConfig = common.NewGlobalConfig()

	// Override with flags if provided
	if kubeconfig != "" {
		globalConfig.Kubeconfig = kubeconfig
	}
	if k8sContext != "" {
		globalConfig.Context = k8sContext
	}
	if namespace != "" {
		globalConfig.Namespace = namespace
	}
}

// getAppIdentifier gets the app identifier from args
func getAppIdentifier(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("app identifier is required")
	}
	return args[0], nil
}
