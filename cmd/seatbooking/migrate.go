package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"

	"github.com/paytm-hack/seatbooking/migrations"
	"github.com/paytm-hack/seatbooking/pkg/config"
)

func runMigrations() error {
	appConfig, err := config.New()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	_, logger, err := newLogger(appConfig)
	if err != nil {
		return err
	}
	return migrateDatabase(appConfig, logger)
}

func migrateDatabase(appConfig config.Getter, logger zerolog.Logger) error {
	database, err := sql.Open("pgx", appConfig.GetDatabaseURL())
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	database.SetMaxOpenConns(1)

	var migrationSource source.Driver
	sourceName := "iofs"
	if folder := appConfig.GetMigrationsFolder(); folder == "" {
		migrationSource, err = iofs.New(migrations.Files, ".")
	} else {
		folder, absErr := filepath.Abs(folder)
		if absErr != nil {
			_ = database.Close()
			return fmt.Errorf("resolve migration folder: %w", absErr)
		}
		sourceName = "file"
		migrationSource, err = (&file.File{}).Open((&url.URL{Scheme: "file", Path: folder}).String())
	}
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("open migration source: %w", err)
	}

	databaseDriver, err := postgres.WithInstance(database, &postgres.Config{})
	if err != nil {
		_ = migrationSource.Close()
		_ = database.Close()
		return fmt.Errorf("initialize postgres migration driver: %w", err)
	}

	migration, err := migrate.NewWithInstance(sourceName, migrationSource, "postgres", databaseDriver)
	if err != nil {
		_ = migrationSource.Close()
		_ = databaseDriver.Close()
		return fmt.Errorf("initialize migration runner: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := migration.Close()
		if sourceErr != nil {
			logger.Error().Err(sourceErr).Msg("close migration source")
		}
		if databaseErr != nil {
			logger.Error().Err(databaseErr).Msg("close migration database")
		}
	}()

	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run pending migrations: %w", err)
	}
	logger.Info().Msg("database schema is up to date")
	return nil
}
