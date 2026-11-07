// Package logging constructs the application's structured logger.
package logging

import (
	"io"
	"log/slog"

	"github.com/go-cli-template/internal/runtimeconfig"
)

// New returns a logger that writes human-readable text locally and JSON in
// non-development environments. The writer is injected to make logging testable.
func New(out io.Writer, cfg runtimeconfig.Config) *slog.Logger {
	options := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Environment == runtimeconfig.Development {
		return slog.New(slog.NewTextHandler(out, options))
	}

	return slog.New(slog.NewJSONHandler(out, options))
}
