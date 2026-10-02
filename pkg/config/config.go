package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Getter interface {
	GetEnvironment() string
	GetAppPort() int
	GetLogLevel() string
	GetDatabaseURL() string
	GetMigrationsFolder() string
	GetDBMaxOpenConns() int
	GetDBMaxIdleConns() int
	GetDBConnTimeout() time.Duration
	GetJWTSecret() string
	GetJWTExpiry() time.Duration
	GetAdminToken() string
}

var _ Getter = (*Config)(nil)

type Config struct {
	Environment      string        `json:"environment"`
	LogLevel         string        `json:"log_level"`
	AppPort          int           `json:"app_port"`
	DatabaseURL      string        `json:"-"`
	MigrationsFolder string        `json:"migrations_folder"`
	DBMaxOpenConns   int           `json:"db_max_open_conns"`
	DBMaxIdleConns   int           `json:"db_max_idle_conns"`
	DBConnTimeout    time.Duration `json:"db_conn_timeout"`
	JWTSecret        string        `json:"-"`
	JWTExpiry        time.Duration `json:"jwt_expiry"`
	AdminToken       string        `json:"-"`
}

func (c Config) GetEnvironment() string          { return c.Environment }
func (c Config) GetAppPort() int                 { return c.AppPort }
func (c Config) GetLogLevel() string             { return c.LogLevel }
func (c Config) GetDatabaseURL() string          { return c.DatabaseURL }
func (c Config) GetMigrationsFolder() string     { return c.MigrationsFolder }
func (c Config) GetDBMaxOpenConns() int          { return c.DBMaxOpenConns }
func (c Config) GetDBMaxIdleConns() int          { return c.DBMaxIdleConns }
func (c Config) GetDBConnTimeout() time.Duration { return c.DBConnTimeout }
func (c Config) GetJWTSecret() string            { return c.JWTSecret }
func (c Config) GetJWTExpiry() time.Duration     { return c.JWTExpiry }
func (c Config) GetAdminToken() string           { return c.AdminToken }

func New() (*Config, error) {
	databaseURL, err := mandatory(envDatabaseURL)
	if err != nil {
		return nil, err
	}
	jwtSecret, err := mandatory(envJWTSecret)
	if err != nil {
		return nil, err
	}
	adminToken, err := mandatory(envAdminToken)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Environment:      optional(envEnvironment, "dev"),
		LogLevel:         optional(envLogLevel, defaultLogLevel),
		AppPort:          optionalInt(envAppPort, defaultAppPort),
		DatabaseURL:      databaseURL,
		MigrationsFolder: optional(envMigrationsFolder, ""),
		DBMaxOpenConns:   optionalInt(envDBMaxOpenConns, defaultDBMaxOpenConns),
		DBMaxIdleConns:   optionalInt(envDBMaxIdleConns, defaultDBMaxIdleConns),
		DBConnTimeout:    defaultDBConnTimeout,
		JWTSecret:        jwtSecret,
		JWTExpiry:        defaultJWTExpiry,
		AdminToken:       adminToken,
	}

	return cfg, nil
}

func (c Config) Loggable() []byte {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Appendf(nil, `{"error":%q}`, err.Error())
	}
	return data
}

func mandatory(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("config: %s is required", key)
	}
	return v, nil
}

func optional(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func optionalInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func optionalBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
