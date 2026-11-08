package main

import (
	"os"
	"strings"
	"testing"
)

func TestRootCommandHasVersion(t *testing.T) {
	cmd := newRootCmd()
	versionFlag := cmd.Flags().Lookup("version")
	if versionFlag == nil {
		t.Fatal("expected --version flag to be registered")
	}
	if versionFlag.Shorthand != "v" {
		t.Fatalf("expected shorthand -v, got %q", versionFlag.Shorthand)
	}
}

func TestRootCommandHasConfig(t *testing.T) {
	cmd := newRootCmd()
	configFlag := cmd.PersistentFlags().Lookup("config")
	if configFlag == nil {
		t.Fatal("expected --config flag to be registered")
	}
}

func TestResolvedVersion(t *testing.T) {
	ver := resolvedVersion()
	if ver == "" {
		t.Fatal("expected version to be non-empty")
	}
}

func TestRootCommandRejectsPositionalArguments(t *testing.T) {
	cmd := newRootCmd()
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("root should accept no arguments: %v", err)
	}
	if err := cmd.Args(cmd, []string{"typo"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}

func TestRequireInteractiveRejectsPipes(t *testing.T) {
	in, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create input pipe: %v", err)
	}
	defer func() { _ = in.Close() }()
	defer func() { _ = inWriter.Close() }()

	out, outReader, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	defer func() { _ = out.Close() }()
	defer func() { _ = outReader.Close() }()

	err = requireInteractive(in, out)
	if err == nil {
		t.Fatal("expected a non-terminal pipe to be rejected")
	}
	if !strings.Contains(err.Error(), "interactive terminal required") {
		t.Fatalf("error = %q, want an interactive terminal message", err)
	}
}
