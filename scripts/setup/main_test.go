package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) options {
	t.Helper()
	o := options{root: t.TempDir(), name: "acme", module: "github.com/acme/acme", description: "Acme tools", author: "Acme", repository: "https://github.com/acme/acme", branch: "develop", version: "0.1.0"}
	files := map[string]string{
		"go.mod": "module github.com/go-cli-template\n\ngo 1.26.10\n",
		"internal/package/package.toml": `name = "go-cli-template"
package_name = "go-cli-template"
module = "github.com/go-cli-template"
description = "Template"
short = "Template"
version = "0.2.3"
homepage = "https://devan.gg/go-cli-template"
repository = "https://github.com/imdevan/go-cli-template/"
author = "hi@devan.gg"
`,
		"cmd/go-cli-template/main.go":        "package main\nimport _ \"github.com/go-cli-template/internal/app\"\n// go-cli-template completion bash\n",
		"internal/app/app.go":                "package app\n",
		"internal/clack/LICENSE":             "MIT go-cli-template\n",
		"internal/clack/third_party/LICENSE": "Upstream\n",
		"README.md":                          "# Go CLI Starter (`go-cli-template`)\n\ngo-cli-template --help\n\n## Provenance and licenses\nhttps://github.com/imdevan/go-cli-template\n",
		"AGENTS.md":                          "Template maintenance rules for go-cli-template\n",
		"CONTRIBUTING.md":                    "Keep the template guide current\n",
		"Makefile":                           "BIN := dist/go-cli-template\nbuild:\n\tgo build -o $(BIN) ./cmd/go-cli-template\n",
		"justfile":                           "PACKAGE := \"go-cli-template\"\n_install-pack:\n go install github.com/imdevan/go-cli-package/cmd/go-cli-package@v0.3.7\n",
		".github/workflows/release.yml":      "binary: go-cli-template_linux_amd64\n",
		".golangci.yml":                      "- github.com/go-cli-template/internal/clack/reflectutil\n",
		".agents/skills/upstream/SKILL.md":   "go-cli-template\n",
		"skills/clack/SKILL.md":              "go-cli-template\n",
		"scripts/setup/example.go":           "// go-cli-template\n",
	}
	for path, data := range files {
		put(t, o.root, path, data)
	}
	return o
}
func put(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path)) // #nosec G304 -- reads only test-owned fixture paths.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			files[rel] = read(t, root, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func assertSnapshot(t *testing.T, root string, want map[string]string) {
	t.Helper()
	got := snapshot(t, root)
	if len(got) != len(want) {
		t.Fatalf("file count changed: got %d want %d", len(got), len(want))
	}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("unexpected change: %s", p)
		}
	}
}
func args(o options) []string {
	return []string{"--root", o.root, "--name", o.name, "--module", o.module, "--description", o.description, "--author", o.author, "--repository", o.repository, "--default-branch", o.branch}
}

func TestDryRun(t *testing.T) {
	o := fixture(t)
	before := snapshot(t, o.root)
	var out bytes.Buffer
	if err := run(args(o), &out, &out); err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, o.root, before)
	for _, want := range []string{"--- a/go.mod", "+++ b/go.mod", "rename cmd/", "Dry run"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}
func TestApplyAndRerun(t *testing.T) {
	o := fixture(t)
	before := snapshot(t, o.root)
	p, err := makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.apply(atomicWrite); err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{"go.mod": "module github.com/acme/acme", "cmd/acme/main.go": "github.com/acme/acme/internal/app", "README.md": "# acme", "AGENTS.md": "not the ClackBT starter", "CONTRIBUTING.md": "Contributing to acme", ".github/workflows/release.yml": "acme_linux_amd64", ".golangci.yml": "github.com/acme/acme/internal/clack/reflectutil", "internal/package/package.toml": "version = \"0.1.0\""}
	for path, want := range checks {
		if !strings.Contains(read(t, o.root, path), want) {
			t.Errorf("%s missing %q", path, want)
		}
	}
	for _, path := range []string{"internal/clack/LICENSE", "internal/clack/third_party/LICENSE", ".agents/skills/upstream/SKILL.md", "skills/clack/SKILL.md", "scripts/setup/example.go", "justfile"} {
		if path == "justfile" {
			if !strings.Contains(read(t, o.root, path), "github.com/imdevan/go-cli-package/") {
				t.Error("generator changed")
			}
			continue
		}
		if read(t, o.root, path) != before[filepath.FromSlash(path)] {
			t.Errorf("protected file changed: %s", path)
		}
	}
	for _, path := range []string{"INSTALL.md", "CONFIG.md", "template/history/PROJECT_ANALYSIS.md"} {
		if _, err := os.Stat(filepath.Join(o.root, path)); !os.IsNotExist(err) {
			t.Errorf("obsolete documentation remains: %s", path)
		}
	}
	if !strings.Contains(read(t, o.root, "README.md"), "https://github.com/imdevan/go-cli-template") {
		t.Error("provenance changed")
	}
	for _, path := range []string{"cmd/go-cli-template"} {
		if _, err := os.Stat(filepath.Join(o.root, path)); !os.IsNotExist(err) {
			t.Errorf("old path remains: %s", path)
		}
	}
	next, err := makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.edits) != 0 || len(next.moves) != 0 {
		t.Fatalf("rerun has %d edits, %d moves", len(next.edits), len(next.moves))
	}
	o.name = "other"
	if _, err := makePlan(o); err == nil {
		t.Fatal("allowed second identity")
	}
}
func TestInvalidOptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*options)
	}{
		{"name", func(o *options) { o.name = "../oops" }}, {"module", func(o *options) { o.module = "github.com/acme/$(id)" }}, {"description", func(o *options) { o.description = "a\nb" }}, {"quote", func(o *options) { o.author = "a\"b" }}, {"repository", func(o *options) { o.repository = "https://host/repo?x=1" }}, {"branch", func(o *options) { o.branch = "main]" }}, {"version", func(o *options) { o.version = "latest" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := fixture(t)
			tc.change(&o)
			if err := o.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
func TestCollision(t *testing.T) {
	o := fixture(t)
	put(t, o.root, "cmd/acme/existing.go", "precious")
	before := snapshot(t, o.root)
	if _, err := makePlan(o); err == nil {
		t.Fatal("expected collision")
	}
	assertSnapshot(t, o.root, before)
}
func TestRollback(t *testing.T) {
	o := fixture(t)
	before := snapshot(t, o.root)
	p, err := makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = p.apply(func(path string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 3 {
			return errors.New("disk failure")
		}
		return atomicWrite(path, data, mode)
	})
	if err == nil {
		t.Fatal("expected error")
	}
	assertSnapshot(t, o.root, before)
}
func TestChangedSincePlanning(t *testing.T) {
	o := fixture(t)
	p, err := makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	put(t, o.root, "go.mod", "changed")
	if err := p.apply(atomicWrite); err == nil {
		t.Fatal("overwrote changed file")
	}
	if read(t, o.root, "go.mod") != "changed" {
		t.Fatal("edit lost")
	}
}
func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- test-owned Git arguments; no shell.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
func TestDirtyProtection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	o := fixture(t)
	git(t, o.root, "init")
	git(t, o.root, "add", ".")
	git(t, o.root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	put(t, o.root, "unrelated.txt", "keep")
	if _, err := makePlan(o); err != nil {
		t.Fatalf("unrelated file blocked: %v", err)
	}
	put(t, o.root, "go.mod", "module github.com/go-cli-template\n\ngo 1.26.10\n// my change\n")
	if _, err := makePlan(o); err == nil || !strings.Contains(err.Error(), "existing changes") {
		t.Fatalf("expected dirty protection, got %v", err)
	}
}
func TestRootAndSymlink(t *testing.T) {
	o := fixture(t)
	root, err := findRoot(filepath.Join(o.root, "internal"))
	if err != nil || root != o.root {
		t.Fatalf("root %q: %v", root, err)
	}
	if err := os.Symlink(filepath.Join(o.root, "go.mod"), filepath.Join(o.root, "internal/link.go")); err != nil {
		t.Skip(err)
	}
	if _, err := makePlan(o); err == nil {
		t.Fatal("accepted symlink")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }
func TestOutputFailureDoesNotApply(t *testing.T) {
	o := fixture(t)
	before := snapshot(t, o.root)
	if err := run(append(args(o), "--apply", "--verify=false"), failedWriter{}, failedWriter{}); err == nil {
		t.Fatal("expected output failure")
	}
	assertSnapshot(t, o.root, before)
}
func TestMissingModuleAgreement(t *testing.T) {
	o := fixture(t)
	put(t, o.root, "go.mod", "module github.com/elsewhere/app\n")
	if _, err := makePlan(o); err == nil {
		t.Fatal("accepted inconsistent module metadata")
	}
}

func TestApplyEntryPoint(t *testing.T) {
	o := fixture(t)
	var out bytes.Buffer
	if err := run(append(args(o), "--apply", "--verify=false"), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Setup applied") {
		t.Fatal("missing success output")
	}
	if _, err := os.Stat(filepath.Join(o.root, "cmd/acme/main.go")); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationFailureLeavesAppliedChanges(t *testing.T) {
	o := fixture(t)
	// This fixture intentionally has no format-check target.
	var out bytes.Buffer
	err := run(append(args(o), "--apply"), &out, &out)
	if err == nil || !strings.Contains(err.Error(), "changes remain applied") {
		t.Fatalf("expected verification failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.root, "cmd/acme/main.go")); err != nil {
		t.Fatal("verification rolled back the rename", err)
	}
}

func TestAppDocsNotRegeneratedOnRerun(t *testing.T) {
	o := fixture(t)
	p, err := makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.apply(atomicWrite); err != nil {
		t.Fatal(err)
	}
	put(t, o.root, "README.md", "# acme\n\nReal product docs\n")
	put(t, o.root, "AGENTS.md", "Application policy, maintained by the team\n")
	p, err = makePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.edits) != 0 || len(p.moves) != 0 {
		t.Fatalf("unexpected rerun changes: %+v", p)
	}
	if err := p.apply(atomicWrite); err != nil {
		t.Fatal(err)
	}
	if read(t, o.root, "README.md") != "# acme\n\nReal product docs\n" {
		t.Fatal("overwrote app README")
	}
	if read(t, o.root, "AGENTS.md") != "Application policy, maintained by the team\n" {
		t.Fatal("overwrote app agent policy")
	}
}

func TestRemovedDocsFlags(t *testing.T) {
	o := fixture(t)
	var out bytes.Buffer
	for _, flag := range []string{"--docs", "--docs-site", "--docs-base"} {
		out.Reset()
		if err := run(append(args(o), flag), &out, &out); err == nil {
			t.Errorf("accepted obsolete option %s", flag)
		}
	}
}
