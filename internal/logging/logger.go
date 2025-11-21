package logging

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// FromContext returns a zerolog.Logger stored in context, or the global logger.
func FromContext(ctx context.Context) zerolog.Logger {
	if ctx == nil {
		return zerolog.Nop()
	}
	if logger, ok := ctx.Value(loggerKey{}).(zerolog.Logger); ok {
		return logger
	}
	return zerolog.Nop()
}

type loggerKey struct{}

// New builds a structured logger with sane defaults for JSON logs.
func New(appName, env string) zerolog.Logger {
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339Nano,
		NoColor:    env == "production",
	}
	logger := zerolog.New(output).With().
		Timestamp().
		Str("app", appName).
		Str("env", env).
		Logger()
	return logger
}

// IntoContext injects a logger into context for downstream use.
func IntoContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}
