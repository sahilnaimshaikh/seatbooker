package logctx

import (
	"context"
	"os"

	"github.com/rs/zerolog"
)

type ctxKey struct{}

func New(ctx context.Context, serviceName string) (context.Context, zerolog.Logger) {
	logger := zerolog.New(os.Stdout).
		With().
		Timestamp().
		Str("service", serviceName).
		Logger()
	return context.WithValue(ctx, ctxKey{}, logger), logger
}

func WithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

func FromContext(ctx context.Context) zerolog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		return logger
	}
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}
