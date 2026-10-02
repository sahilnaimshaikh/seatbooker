package config

import "time"

const (
	defaultAppPort          = 8080
	defaultLogLevel         = "info"
	defaultMigrationsFolder = "migrations"
	defaultDBMaxOpenConns   = 20
	defaultDBMaxIdleConns   = 10
	defaultDBConnTimeout    = 5 * time.Second
	defaultJWTExpiry        = 24 * time.Hour
)

const (
	envEnvironment      = "ENVIRONMENT"
	envLogLevel         = "LOG_LEVEL"
	envAppPort          = "APP_PORT"
	envDatabaseURL      = "DATABASE_URL"
	envMigrationsFolder = "MIGRATIONS_FOLDER"
	envDBMaxOpenConns   = "DB_MAX_OPEN_CONNS"
	envDBMaxIdleConns   = "DB_MAX_IDLE_CONNS"
	envJWTSecret        = "JWT_SECRET"
	envAdminToken       = "ADMIN_TOKEN"
)
