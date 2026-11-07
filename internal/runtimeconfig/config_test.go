package runtimeconfig

import (
	"log/slog"
	"strings"
	"testing"
)

func lookupFrom(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(lookupFrom(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != Development || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("Load() = %+v; want development and info", cfg)
	}
}

func TestLoadValues(t *testing.T) {
	t.Parallel()

	levels := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for input, want := range levels {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(lookupFrom(map[string]string{
				"APP_ENV":   "production",
				"LOG_LEVEL": input,
			}))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Environment != Production || cfg.LogLevel != want {
				t.Fatalf("Load() = %+v; want production and %s", cfg, want)
			}
		})
	}
}

func TestLoadTestEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := Load(lookupFrom(map[string]string{"APP_ENV": "test"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != Test {
		t.Fatalf("Environment = %q; want %q", cfg.Environment, Test)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		values map[string]string
		field  string
	}{
		{name: "unsupported environment", values: map[string]string{"APP_ENV": "staging"}, field: "APP_ENV"},
		{name: "empty environment", values: map[string]string{"APP_ENV": ""}, field: "APP_ENV"},
		{name: "unsupported level", values: map[string]string{"LOG_LEVEL": "trace"}, field: "LOG_LEVEL"},
		{name: "empty level", values: map[string]string{"LOG_LEVEL": ""}, field: "LOG_LEVEL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(lookupFrom(tc.values))
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("Load() error = %v; want error naming %s", err, tc.field)
			}
		})
	}
}

func TestLoadRejectsNilLookup(t *testing.T) {
	t.Parallel()

	_, err := Load(nil)
	if err == nil {
		t.Fatal("Load(nil) succeeded; want error")
	}
}

func TestLoadRejectsCaseAndWhitespaceVariants(t *testing.T) {
	for _, values := range []map[string]string{{"APP_ENV": "Production"}, {"APP_ENV": " production"}, {"LOG_LEVEL": "WARN"}, {"LOG_LEVEL": "warn+1"}, {"LOG_LEVEL": "info "}} {
		if _, err := Load(lookupFrom(values)); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
}
func TestLoadIgnoresUnrelatedVariables(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{"app_env": "invalid", "OTHER": "invalid"}))
	if err != nil || cfg.Environment != Development || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected configuration: %+v %v", cfg, err)
	}
}
