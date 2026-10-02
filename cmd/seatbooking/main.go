package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/paytm-hack/seatbooking/pkg/config"
	"github.com/paytm-hack/seatbooking/pkg/constants"
	"github.com/paytm-hack/seatbooking/pkg/db"
	"github.com/paytm-hack/seatbooking/pkg/factory"
	"github.com/paytm-hack/seatbooking/pkg/logctx"
	"github.com/paytm-hack/seatbooking/pkg/metrics"
	"github.com/paytm-hack/seatbooking/pkg/router"
	"github.com/paytm-hack/seatbooking/pkg/service"
)

const help = `seatbooking runs the seat booking HTTP service.

Usage:
  seatbooking serve    Start the HTTP server.
  seatbooking migrate  Apply pending database migrations.
  seatbooking help     Print this help.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, help)
		os.Exit(2)
	}

	var err error
	switch strings.ToLower(os.Args[1]) {
	case "serve":
		err = serve()
	case "migrate":
		err = runMigrations()
	case "help":
		fmt.Print(help)
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		logger.Fatal().Err(err).Msg("application stopped")
	}
}

func serve() error {
	appConfig, err := config.New()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	baseContext, logger, err := newLogger(appConfig)
	if err != nil {
		return err
	}
	serverContext, stop := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer stop()

	appFactory, err := factory.New(appConfig)
	if err != nil {
		return fmt.Errorf("initialize database factory: %w", err)
	}
	defer func() {
		if err := appFactory.Close(); err != nil {
			logger.Error().Err(err).Msg("close database connection pool")
		}
	}()

	database := appFactory.DB()
	requestMetrics := metrics.New(db.NewShowTable(database))
	handler := router.New(&router.Router{
		Shows:        service.NewShowService(database),
		Reservations: service.NewReservationService(database),
		Database:     database,
		Metrics:      requestMetrics,
		AppConfig:    appConfig,
		Context:      serverContext,
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", appConfig.GetAppPort()),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info().Str("address", server.Addr).Msg("starting HTTP server")
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-serverContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serverErrors; err != nil {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		logger.Info().Msg("HTTP server stopped")
		return nil
	}
}

func newLogger(appConfig *config.Config) (context.Context, zerolog.Logger, error) {
	level, err := zerolog.ParseLevel(appConfig.GetLogLevel())
	if err != nil {
		return nil, zerolog.Logger{}, fmt.Errorf("parse log level: %w", err)
	}
	zerolog.SetGlobalLevel(level)
	ctx, logger := logctx.New(context.Background(), constants.ServiceName)
	return ctx, logger, nil
}
