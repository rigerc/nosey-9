package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGreetCommandRegistered(t *testing.T) {
	t.Parallel()
	root := newDemoRootCmd()
	command, _, err := root.Find([]string{"greet"})
	if err != nil || command.Name() != "greet" {
		t.Fatalf("Find(greet) = %v, %v; want greet", command, err)
	}
}

func TestRunGreetProducesJSONInProduction(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	lookup := func(name string) (string, bool) {
		value, ok := map[string]string{"APP_ENV": "production"}[name]
		return value, ok
	}

	if err := runGreet(&out, lookup, "Ada"); err != nil {
		t.Fatalf("runGreet() error = %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("runGreet() output = %q; want JSON: %v", out.String(), err)
	}
	if event["greeting"] != "Hello, Ada!" || event["app_env"] != "production" {
		t.Fatalf("runGreet() event = %v; want greeting and environment", event)
	}
}

func TestRunGreetFailsBeforeLoggingInvalidEnvironment(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	lookup := func(name string) (string, bool) {
		if name == "APP_ENV" {
			return "invalid", true
		}
		return "", false
	}

	err := runGreet(&out, lookup, "Ada")
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("runGreet() error = %v; want APP_ENV validation error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("runGreet() output = %q; expected no log on error", out.String())
	}
}

func TestGreetSubcommandExecutes(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOG_LEVEL", "info")

	var out bytes.Buffer
	root := newDemoRootCmd()
	root.SetOut(&out)
	root.SetArgs([]string{"greet", "Ada"})
	if err := root.Execute(); err != nil {
		t.Fatalf("greet command failed: %v", err)
	}

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("greet output = %q; expected JSON: %v", out.String(), err)
	}
	if event["greeting"] != "Hello, Ada!" {
		t.Fatalf("greet event = %v; expected Ada's greeting", event)
	}
}
