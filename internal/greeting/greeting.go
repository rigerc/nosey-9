// Package greeting contains an example of independently testable business logic.
package greeting

import (
	"log/slog"

	"github.com/go-cli-template/internal/runtimeconfig"
)

// Greet returns a greeting for the supplied name.
func Greet(name string) string {
	return "Hello, " + name + "!"
}

// Run uses an injected logger and validated configuration, making the
// application behavior testable without global state or module-level mocks.
func Run(logger *slog.Logger, cfg runtimeconfig.Config, name string) string {
	greeting := Greet(name)
	logger.Info("greeting generated", "app_env", cfg.Environment, "greeting", greeting)

	return greeting
}
