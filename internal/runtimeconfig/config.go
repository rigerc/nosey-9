// Package runtimeconfig validates application configuration at process startup.
package runtimeconfig

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-cli-template/internal/config"
)

// Environment is a validated application execution environment.
type Environment string

const (
	// Development is the default local environment.
	Development Environment = "development"
	// Test is the environment for automated tests.
	Test Environment = "test"
	// Production is the production environment.
	Production Environment = "production"
)

// Config contains validated application settings.
type Config struct {
	Environment Environment
	LogLevel    slog.Level
}

// LookupFunc provides environment values without coupling validation to os.Environ.
type LookupFunc func(string) (string, bool)

// Load validates environment values. Defaults apply only when a key is absent.
// Invalid settings return errors naming the offending variable.
func Load(lookup LookupFunc) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup must not be nil")
	}

	raw, err := config.Load(settings{Environment: "development", LogLevel: "info"}, config.Options[settings]{
		Env: &config.EnvSource{Lookup: lookup, Variables: map[string]string{"APP_ENV": "environment", "LOG_LEVEL": "log_level"}},
	})
	if err != nil {
		return Config{}, err
	}
	return validateSettings(raw)
}

type settings struct {
	Environment string `toml:"environment"`
	LogLevel    string `toml:"log_level"`
}

func validateSettings(raw settings) (Config, error) {
	cfg := Config{Environment: Environment(raw.Environment)}
	switch cfg.Environment {
	case Development, Test, Production:
	default:
		return Config{}, fmt.Errorf("APP_ENV: must be development, test, or production; got %q", raw.Environment)
	}
	switch raw.LogLevel {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL: must be debug, info, warn, or error; got %q", raw.LogLevel)
	}
	return cfg, nil
}
