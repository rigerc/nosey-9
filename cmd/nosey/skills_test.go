package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsPrint(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"skills", "print"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills print: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"name: \"nosey\"",
		"`nosey scan`",
		"`nosey config`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("skills print missing %q", want)
		}
	}
	if strings.Contains(text, "`nosey skills`") {
		t.Error("skill generator must not document itself")
	}
}

func TestSkillsGenerateNameOverridePreservesUnrelatedSkills(t *testing.T) {
	dir := t.TempDir()
	untouched := filepath.Join(dir, "charmed-bubbles", "NOTICE.md")
	if err := os.MkdirAll(filepath.Dir(untouched), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untouched, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"skills", "generate", "--dir", dir, "--skill-name", "custom-cli"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills generate: %v", err)
	}

	// #nosec G304 -- this reads a generated fixture inside t.TempDir.

	got, err := os.ReadFile(filepath.Join(dir, "custom-cli", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "name: \"custom-cli\"") {
		t.Error("generated skill must use the overridden name")
	}
	if !strings.Contains(string(got), "`nosey scan`") {
		t.Error("generated skill should include Cobra subcommands")
	}
	if strings.Contains(string(got), "`nosey skills`") {
		t.Error("generated skill must exclude the generator")
	}
	// #nosec G304 -- this reads a generated fixture inside t.TempDir.
	saved, err := os.ReadFile(untouched)
	if err != nil || string(saved) != "preserve me" {
		t.Fatalf("unrelated skill was modified: contents %q, error %v", saved, err)
	}
}

func TestSkillsRejectsInvalidNameOverride(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"skills", "print", "--skill-name", "../other"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("expected invalid skill name error, got %v", err)
	}
}
