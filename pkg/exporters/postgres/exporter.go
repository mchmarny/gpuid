package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/mchmarny/gpuid/pkg/gpu"
)

// Environment variable names for PostgreSQL configuration
const (
	// Required environment variables
	EnvPostgresHost        = "POSTGRES_HOST"
	EnvPostgresPort        = "POSTGRES_PORT"
	EnvPostgresDB          = "POSTGRES_DB"
	EnvPostgresUser        = "POSTGRES_USER"
	EnvPostgresPassword    = "POSTGRES_PASSWORD" // #nosec G101 - This is an environment variable name, not a credential
	EnvPostgresSSLMode     = "POSTGRES_SSLMODE"
	EnvPostgresTable       = "POSTGRES_TABLE"
	EnvPostgresAutoMigrate = "POSTGRES_AUTO_MIGRATE"

	// Default values
	defaultPostgresPort  = 5432
	defaultPostgresTable = "gpu"
	defaultSSLMode       = "require"

	// SQL query template for inserting GPU serial readings.
	// Retained for fallback / tests; production writes use pq.CopyIn for batch throughput.
	insertQueryTemplate = `INSERT INTO %s (cluster, node, machine, source, chassis, gpu, read_time, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
)

// Config holds PostgreSQL-specific configuration parameters.
type Config struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
	Database string `json:"database" yaml:"database"`
	User     string `json:"user" yaml:"user"`
	Password string `json:"password" yaml:"password"`
	SSLMode  string `json:"sslmode" yaml:"sslmode"`
	Table    string `json:"table" yaml:"table"`
}

// Exporter implements the ExporterBackend interface for PostgreSQL.
// This exporter supports batch inserts and connection pooling for high-throughput.
type Exporter struct {
	db          *sql.DB
	config      Config
	autoMigrate bool
}

// New creates a new PostgreSQL exporter with configuration loaded from environment variables.
// Required environment variables:
//   - POSTGRES_HOST: PostgreSQL server hostname
//   - POSTGRES_DB: Database name
//   - POSTGRES_USER: Database username
//   - POSTGRES_PASSWORD: Database password
//
// Optional environment variables:
//   - POSTGRES_PORT: Database port (defaults to 5432)
//   - POSTGRES_SSLMODE: SSL mode (defaults to require)
//   - POSTGRES_TABLE: Table name (defaults to gpu_serial_readings)
func New(ctx context.Context) (*Exporter, error) {
	config := loadConfigFromEnv()

	if validationErr := config.Validate(); validationErr != nil {
		return nil, fmt.Errorf("PostgreSQL configuration validation failed: %w", validationErr)
	}

	// Create connection string
	connStr := config.ConnectionString()

	// Open database connection with connection pooling
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	// Configure connection pool for production use
	db.SetMaxOpenConns(25)                 // Maximum number of open connections
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Connection lifetime

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL database: %w", err)
	}

	exporter := &Exporter{
		db:          db,
		config:      config,
		autoMigrate: strings.EqualFold(os.Getenv(EnvPostgresAutoMigrate), "true"),
	}

	// Schema bootstrap is opt-in. Most deployments should manage schema out-of-band
	// (migrations / DBA tooling) and run the controller with limited DDL grants.
	if exporter.autoMigrate {
		if err := exporter.initializeSchema(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to initialize database schema: %w", err)
		}
	}

	return exporter, nil
}

// Write inserts GPU serial number readings into PostgreSQL using the COPY protocol
// for true batch throughput (one round-trip + bulk stream instead of N round-trips).
func (e *Exporter) Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				log.Error("transaction rollback failed", "error", rbErr)
			}
		}
	}()

	// pq.CopyIn is the documented entry-point for the COPY FROM STDIN protocol
	// under database/sql; lib/pq's deprecation note refers to library callers
	// who can use the pgx driver instead. We stay on database/sql, so this is
	// the correct API.
	stmt, err := tx.PrepareContext(ctx, pq.CopyIn(e.config.Table, //nolint:staticcheck // SA1019: see comment above
		"cluster", "node", "machine", "source", "chassis", "gpu", "read_time", "created_at"))
	if err != nil {
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}

	now := time.Now().UTC()
	rows := 0
	for _, record := range records {
		if record == nil {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			record.Cluster,
			record.Node,
			record.Machine,
			record.Source,
			setDefaultIfEmpty(record.Chassis, "unknown"),
			record.GPU,
			record.Time,
			now,
		); err != nil {
			stmt.Close()
			return fmt.Errorf("failed to buffer record for COPY: %w", err)
		}
		rows++
	}

	// Flush the COPY buffer.
	if _, err := stmt.ExecContext(ctx); err != nil {
		stmt.Close()
		return fmt.Errorf("failed to flush COPY: %w", err)
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close COPY statement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	log.Info("export completed",
		"table", e.config.Table,
		"records", rows,
		"database", e.config.Database)

	return nil
}

// Close performs cleanup of PostgreSQL connection resources.
func (e *Exporter) Close(_ context.Context) error {
	if e.db != nil {
		return e.db.Close()
	}
	return nil
}

// Health performs a health check by pinging the PostgreSQL database.
func (e *Exporter) Health(ctx context.Context) error {
	if e.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	// Use a context with timeout for health check
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := e.db.PingContext(healthCtx); err != nil {
		return fmt.Errorf("postgreSQL health check failed: %w", err)
	}

	return nil
}

// initializeSchema creates the necessary table structure if it doesn't exist.
func (e *Exporter) initializeSchema(ctx context.Context) error {
	createTableQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			cluster VARCHAR(255) NOT NULL,
			node VARCHAR(255) NOT NULL,
			machine VARCHAR(255) NOT NULL,
			source VARCHAR(255) NOT NULL,
			chassis VARCHAR(255) NOT NULL,
			gpu VARCHAR(255) NOT NULL,
			read_time TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			UNIQUE(cluster, node, machine, source, chassis, gpu, read_time)
		)`, e.config.Table)

	_, err := e.db.ExecContext(ctx, createTableQuery)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", e.config.Table, err)
	}

	// Create indexes for efficient querying
	indexQueries := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_cluster ON %s (cluster)", e.config.Table, e.config.Table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_node ON %s (node)", e.config.Table, e.config.Table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_read_time ON %s (read_time)", e.config.Table, e.config.Table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s (created_at)", e.config.Table, e.config.Table),
	}

	for _, query := range indexQueries {
		if _, err := e.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// loadConfigFromEnv extracts PostgreSQL configuration from environment variables.
func loadConfigFromEnv() Config {
	config := Config{
		Host:     getEnv(EnvPostgresHost, ""),
		Port:     getEnvAsInt(EnvPostgresPort, defaultPostgresPort),
		Database: getEnv(EnvPostgresDB, ""),
		User:     getEnv(EnvPostgresUser, ""),
		Password: os.Getenv(EnvPostgresPassword), // No default for security
		SSLMode:  getEnv(EnvPostgresSSLMode, defaultSSLMode),
		Table:    getEnv(EnvPostgresTable, defaultPostgresTable),
	}

	return config
}

// ConnectionString builds a PostgreSQL connection string from the configuration.
func (c *Config) ConnectionString() string {
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.Port, c.Database, c.User, c.Password, c.SSLMode)
}

// tableNameRegex validates that a table name contains only safe characters.
var tableNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Validate ensures the PostgreSQL configuration is valid for operations.
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("postgres host is required (set %s)", EnvPostgresHost)
	}
	if c.Database == "" {
		return fmt.Errorf("postgres database name is required (set %s)", EnvPostgresDB)
	}
	if c.User == "" {
		return fmt.Errorf("postgres user is required (set %s)", EnvPostgresUser)
	}
	if c.Password == "" {
		return fmt.Errorf("postgres password is required (set %s)", EnvPostgresPassword)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("postgres port must be between 1 and 65535, got %d", c.Port)
	}
	if c.Table == "" {
		return fmt.Errorf("postgres table name cannot be empty")
	}
	if !tableNameRegex.MatchString(c.Table) {
		return fmt.Errorf("postgres table name contains invalid characters: %s", c.Table)
	}

	return nil
}

// getEnv retrieves an environment variable value with a fallback default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt retrieves an environment variable as an integer with a fallback default.
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// setDefaultIfEmpty returns defaultValue if value is an empty string.
func setDefaultIfEmpty(value string, defaultValue string) string {
	if value == "" {
		value = defaultValue
	}
	return value
}
