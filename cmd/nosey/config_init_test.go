package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/rigerc/nosey-9/internal/utils"
)

func TestConfigInitChecksOnlyTheTargetPath(t *testing.T) {
	t.Run("refuses an existing global target", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
		project := filepath.Join(root, "project")
		if err := os.MkdirAll(project, 0o750); err != nil {
			t.Fatalf("mkdir project: %v", err)
		}
		t.Chdir(project)

		globalPath := utils.ConfigPathGlobal()
		if err := os.MkdirAll(filepath.Dir(globalPath), 0o750); err != nil {
			t.Fatalf("mkdir global config dir: %v", err)
		}
		if err := os.WriteFile(globalPath, []byte("editor = \"vim\"\n"), 0o600); err != nil {
			t.Fatalf("write global config: %v", err)
		}

		err := runConfigInitForTest(t, &configInitOptions{})
		if err == nil {
			t.Fatal("expected existing global config to be refused")
		}
		if !strings.Contains(err.Error(), globalPath) {
			t.Fatalf("error %q does not mention %s", err, globalPath)
		}
	})

	t.Run("ignores an existing local file when writing global", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
		project := filepath.Join(root, "project")
		if err := os.MkdirAll(project, 0o750); err != nil {
			t.Fatalf("mkdir project: %v", err)
		}
		t.Chdir(project)

		localPath := utils.ConfigPathLocal(project)
		if err := os.MkdirAll(filepath.Dir(localPath), 0o750); err != nil {
			t.Fatalf("mkdir local config dir: %v", err)
		}
		if err := os.WriteFile(localPath, []byte("editor = \"vim\"\n"), 0o600); err != nil {
			t.Fatalf("write local config: %v", err)
		}

		if err := runConfigInitForTest(t, &configInitOptions{}); err != nil {
			t.Fatalf("init global config: %v", err)
		}
		if _, err := os.Stat(utils.ConfigPathGlobal()); err != nil {
			t.Fatalf("expected global config to be written: %v", err)
		}

		data, err := os.ReadFile(localPath) // #nosec G304 -- path is created in the test's temporary directory.
		if err != nil {
			t.Fatalf("read local config: %v", err)
		}
		if string(data) != "editor = \"vim\"\n" {
			t.Fatalf("local config was modified: %q", data)
		}
	})
}

func TestConfigInitLocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Chdir(project)

	if err := runConfigInitForTest(t, &configInitOptions{local: true}); err != nil {
		t.Fatalf("init local config: %v", err)
	}

	localPath := utils.ConfigPathLocal(project)
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("expected local config to be written: %v", err)
	}
	if _, err := os.Stat(utils.ConfigPathGlobal()); !os.IsNotExist(err) {
		t.Fatalf("expected no global config, stat error = %v", err)
	}

	err := runConfigInitForTest(t, &configInitOptions{local: true})
	if err == nil {
		t.Fatal("expected an existing local target to be refused")
	}
	if err := runConfigInitForTest(t, &configInitOptions{local: true, force: true}); err != nil {
		t.Fatalf("force local config: %v", err)
	}
}

func runConfigInitForTest(t *testing.T, opts *configInitOptions) error {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	return runConfigInit(cmd, opts)
}
