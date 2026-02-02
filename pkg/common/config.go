package common

import (
	"os"
	"path/filepath"
)

// GlobalConfig holds global configuration
type GlobalConfig struct {
	Kubeconfig string
	Context    string
	Namespace  string
	PgDumpPath string
	PsqlPath   string
	OutputDir  string
}

// S3Config holds S3 storage configuration
type S3Config struct {
	Endpoint  string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	Region    string
}

// NewGlobalConfig creates a new global config with defaults
func NewGlobalConfig() *GlobalConfig {
	homeDir, _ := os.UserHomeDir()
	defaultKubeconfig := filepath.Join(homeDir, ".kube", "config")
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		defaultKubeconfig = kubeconfigEnv
	}

	return &GlobalConfig{
		Kubeconfig: defaultKubeconfig,
		Context:    "",
		Namespace:  "",
		PgDumpPath: getEnvOrDefault("PG_DUMP_PATH", "pg_dump"),
		PsqlPath:   getEnvOrDefault("PSQL_PATH", "psql"),
		OutputDir:  ".backup",
	}
}

// LoadS3Config loads S3 configuration from environment variables
func LoadS3Config() (*S3Config, error) {
	config := &S3Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		Bucket:    os.Getenv("S3_BUCKET"),
		Prefix:    os.Getenv("S3_PREFIX"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Region:    getEnvOrDefault("S3_REGION", "us-east-1"),
	}

	// Validate required fields
	if config.Endpoint == "" || config.Bucket == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, ErrS3ConfigIncomplete
	}

	return config, nil
}

// IsS3Configured checks if S3 is configured
func IsS3Configured() bool {
	_, err := LoadS3Config()
	return err == nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
