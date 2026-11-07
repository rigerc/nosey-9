package greeting

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-cli-template/internal/runtimeconfig"
)

func TestGreet(t *testing.T) {
	t.Parallel()

	if got := Greet("world"); got != "Hello, world!" {
		t.Fatalf("Greet(world) = %q; want %q", got, "Hello, world!")
	}
}

func TestRunUsesInjectedLogger(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	cfg := runtimeconfig.Config{Environment: runtimeconfig.Test}

	if got := Run(logger, cfg, "Ada"); got != "Hello, Ada!" {
		t.Fatalf("Run() = %q; want Hello, Ada!", got)
	}
	if text := out.String(); !strings.Contains(text, "greeting=\"Hello, Ada!\"") || !strings.Contains(text, "app_env=test") {
		t.Fatalf("log = %q; want greeting and environment", text)
	}
}
