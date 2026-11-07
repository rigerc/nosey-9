package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-cli-template/internal/runtimeconfig"
)

func TestNewDevelopmentUsesText(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := New(&out, runtimeconfig.Config{Environment: runtimeconfig.Development, LogLevel: slog.LevelInfo})
	logger.Info("example", "request_id", "abc")

	text := out.String()
	if !strings.Contains(text, "msg=example") || !strings.Contains(text, "request_id=abc") {
		t.Fatalf("text log = %q; want structured attributes", text)
	}
}

func TestNewProductionUsesJSONAndRespectsLevel(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := New(&out, runtimeconfig.Config{Environment: runtimeconfig.Production, LogLevel: slog.LevelWarn})
	logger.Info("filtered")
	logger.Warn("warning", "request_id", "xyz")

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("log output is not JSON: %v", err)
	}
	if event["msg"] != "warning" || event["request_id"] != "xyz" || event["level"] != "WARN" {
		t.Fatalf("JSON event = %v; want warning attributes", event)
	}
	if strings.Contains(out.String(), "filtered") {
		t.Fatal("info event emitted below configured warning level")
	}
}
