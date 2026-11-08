package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNewAndCheck(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "registry")
	files := map[string][]byte{"skills.json": []byte("{}\n"), "authoring/input.json": []byte("[]\n")}
	if err := writeNew(dest, files); err != nil {
		t.Fatal(err)
	}
	if err := checkFiles(dest, files); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(dest, files); err == nil {
		t.Fatal("existing output must not be overwritten")
	}
	if err := os.WriteFile(filepath.Join(dest, "skills.json"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkFiles(dest, files); err == nil {
		t.Fatal("check must detect drift")
	}
}

func TestWriteRejectsTraversalWithoutPartialOutput(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "registry")
	if err := writeNew(dest, map[string][]byte{"../escape": []byte("bad")}); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial destination exists")
	}
}

func TestHelpAndArguments(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("check")) {
		t.Fatal("help missing check mode")
	}
	if err := run([]string{"unexpected"}, &out); err == nil {
		t.Fatal("expected usage failure")
	}
}
