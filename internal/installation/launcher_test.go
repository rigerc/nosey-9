package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G306 -- test executable in a temporary directory.
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func launcherService(t *testing.T) Service {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Service{Store: settings.Store{Workspace: workspace, Cache: filepath.Join(t.TempDir(), "cache")}, Runner: process.Runner{}}
}

func TestResolveCLIPrefersInstalledPinnedVersion(t *testing.T) {
	s := launcherService(t)
	bin := t.TempDir()
	script(t, filepath.Join(bin, "skills"), "echo "+Version)
	script(t, filepath.Join(bin, "npx"), "exit 1")
	t.Setenv("PATH", bin)
	cli, err := s.resolveCLI(context.Background())
	if err != nil || cli.Backend != "installed" || cli.Name != filepath.Join(bin, "skills") {
		t.Fatal(cli, err)
	}
}

func TestResolveCLIFallsBackToNpxFromNeutralDirectory(t *testing.T) {
	s := launcherService(t)
	bin := t.TempDir()
	target := filepath.Join(t.TempDir(), "cli.mjs")
	cwd := filepath.Join(t.TempDir(), "npx-cwd")
	if err := storage.Write(target, []byte("cli")); err != nil {
		t.Fatal(err)
	}
	script(t, filepath.Join(bin, "skills"), "echo 0.0.1")
	script(t, filepath.Join(bin, "node"), "echo "+Version)
	script(t, filepath.Join(bin, "npx"), fmt.Sprintf("pwd > %q\necho notice\necho %q", cwd, target))
	t.Setenv("PATH", bin)
	cli, err := s.resolveCLI(context.Background())
	if err != nil || cli.Backend != "npx" || cli.Name != filepath.Join(bin, "node") || len(cli.Args) != 1 || cli.Args[0] != target {
		t.Fatal(cli, err)
	}
	// #nosec G304 -- temporary path created by this test.
	ran, err := os.ReadFile(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if string(ran) != s.Store.Cache+"\n" {
		t.Fatalf("npx ran in %q, want the neutral cache directory", ran)
	}
}

func TestResolveCLINeverRunsWorkspaceCode(t *testing.T) {
	s := launcherService(t)
	marker := filepath.Join(s.Store.Workspace, "MARKER")
	planted := filepath.Join(s.Store.Workspace, "node_modules", ".bin", "skills")
	script(t, planted, fmt.Sprintf("touch %q\necho %s", marker, Version))
	bin := t.TempDir()
	script(t, filepath.Join(bin, "node"), fmt.Sprintf("touch %q\necho %s", marker, Version))
	script(t, filepath.Join(bin, "npx"), fmt.Sprintf("echo %q", planted))
	t.Setenv("PATH", filepath.Dir(planted)+string(os.PathListSeparator)+bin)
	_, err := s.resolveCLI(context.Background())
	var prerequisite PrerequisiteError
	if !errors.As(err, &prerequisite) {
		t.Fatal("workspace CLI accepted:", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("workspace CLI was executed")
	}
}

func TestResolveCLIStopsWhenNothingIsAvailable(t *testing.T) {
	s := launcherService(t)
	t.Setenv("PATH", t.TempDir())
	_, err := s.resolveCLI(context.Background())
	var prerequisite PrerequisiteError
	if !errors.As(err, &prerequisite) {
		t.Fatal("missing CLI was not a prerequisite error:", err)
	}
	bin := t.TempDir()
	script(t, filepath.Join(bin, "node"), "echo "+Version)
	script(t, filepath.Join(bin, "npx"), "echo relative/skills")
	t.Setenv("PATH", bin)
	if _, err = s.resolveCLI(context.Background()); !errors.As(err, &prerequisite) {
		t.Fatal("relative npx path accepted:", err)
	}
}
