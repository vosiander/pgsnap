package postgres

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/vosiander/pgsnap/pkg/common"
)

// DBConfig holds database connection configuration
type DBConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
}

// ParseFromEnvVars parses database configuration from environment variables
func ParseFromEnvVars(envVars map[string]string) (*DBConfig, error) {
	// Try full URL formats first
	urlKeys := []string{"DATABASE_URL", "POSTGRES_URL", "DB_URL"}
	for _, key := range urlKeys {
		if urlStr, ok := envVars[key]; ok && urlStr != "" {
			config, err := parseURL(urlStr)
			if err == nil {
				return config, nil
			}
		}
	}

	// Try individual components
	config := &DBConfig{
		Host:     getEnvVar(envVars, "POSTGRES_HOST", "DB_HOST", "DATABASE_HOST"),
		Database: getEnvVar(envVars, "POSTGRES_DB", "POSTGRES_DATABASE", "DB_NAME", "DATABASE_NAME"),
		User:     getEnvVar(envVars, "POSTGRES_USER", "DB_USER", "DATABASE_USER"),
		Password: getEnvVar(envVars, "POSTGRES_PASSWORD", "DB_PASSWORD", "DATABASE_PASSWORD"),
		SSLMode:  getEnvVar(envVars, "POSTGRES_SSL_MODE", "DB_SSL_MODE", "PGSSLMODE"),
	}

	// Parse port
	portStr := getEnvVar(envVars, "POSTGRES_PORT", "DB_PORT", "DATABASE_PORT")
	if portStr == "" {
		config.Port = 5432
	} else {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			config.Port = 5432
		} else {
			config.Port = port
		}
	}

	// Set default SSL mode if not specified
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}

	// Validate required fields
	if config.Host == "" || config.Database == "" || config.User == "" {
		return nil, common.ErrNoDatabaseConfig
	}

	return config, nil
}

// parseURL parses a PostgreSQL connection URL
func parseURL(urlStr string) (*DBConfig, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	config := &DBConfig{
		Host:     u.Hostname(),
		Database: u.Path,
		SSLMode:  "disable",
	}

	// Remove leading slash from database name
	if len(config.Database) > 0 && config.Database[0] == '/' {
		config.Database = config.Database[1:]
	}

	// Parse port
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil {
			config.Port = 5432
		} else {
			config.Port = port
		}
	} else {
		config.Port = 5432
	}

	// Parse user and password
	if u.User != nil {
		config.User = u.User.Username()
		config.Password, _ = u.User.Password()
	}

	// Parse SSL mode from query params
	query := u.Query()
	if sslMode := query.Get("sslmode"); sslMode != "" {
		config.SSLMode = sslMode
	}

	// Validate
	if config.Host == "" || config.Database == "" || config.User == "" {
		return nil, common.ErrNoDatabaseConfig
	}

	return config, nil
}

// ConnectionString returns a PostgreSQL connection string
func (db *DBConfig) ConnectionString() string {
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=%s",
		db.Host, db.Port, db.Database, db.User, db.SSLMode)

	if db.Password != "" {
		connStr += fmt.Sprintf(" password=%s", db.Password)
	}

	return connStr
}

// PGPASSWORD returns the password for PGPASSWORD environment variable
func (db *DBConfig) PGPASSWORD() string {
	return db.Password
}

// getEnvVar tries multiple keys and returns the first non-empty value
func getEnvVar(envVars map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := envVars[key]; ok && value != "" {
			return value
		}
	}
	return ""
}
