// Command setup customizes a checkout of the CLI starter without external generators.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type options struct {
	root, name, module, description, author, repository, branch, version string
	apply, verify, check, skills                                         bool
}
type edit struct {
	path          string
	before, after []byte
	mode          os.FileMode
}
type move struct{ from, to string }
type plan struct {
	root      string
	edits     []edit
	moves     []move
	leftovers []string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
}

func run(args []string, out, errOut io.Writer) error {
	var o options
	f := flag.NewFlagSet("setup", flag.ContinueOnError)
	f.SetOutput(errOut)
	f.StringVar(&o.root, "root", ".", "directory within the template checkout")
	f.StringVar(&o.name, "name", "", "lowercase executable name (required)")
	f.StringVar(&o.module, "module", "", "Go module path (required)")
	f.StringVar(&o.description, "description", "", "application description (required)")
	f.StringVar(&o.author, "author", "", "application author (required)")
	f.StringVar(&o.repository, "repository", "", "HTTPS repository URL (required)")
	f.StringVar(&o.branch, "default-branch", "main", "default Git branch")
	f.StringVar(&o.version, "version", "0.1.0", "initial semantic version")
	f.BoolVar(&o.apply, "apply", false, "write the displayed changes")
	f.BoolVar(&o.verify, "verify", true, "after apply: format check, tests, build, CLI smoke checks")
	f.BoolVar(&o.check, "check", false, "after apply: also run make check")
	f.BoolVar(&o.skills, "skills", false, "after apply: also run make skills (optional CLI skill generation)")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	if err := o.validate(); err != nil {
		return err
	}
	root, err := findRoot(o.root)
	if err != nil {
		return err
	}
	o.root = root
	p, err := makePlan(o)
	if err != nil {
		return err
	}
	if err := p.print(out); err != nil {
		return err
	}
	if !o.apply {
		_, err := fmt.Fprintln(out, "Dry run: no files changed. Repeat with --apply to write these changes.")
		return err
	}
	if err := p.apply(atomicWrite); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Setup applied. Review git diff before committing."); err != nil {
		return err
	}
	if o.verify || o.check || o.skills {
		if err := verify(o, out, errOut); err != nil {
			return fmt.Errorf("setup changes remain applied; verification failed: %w", err)
		}
	}
	_, err = fmt.Fprintln(out, "Next: replace demo commands and the directory list with application behavior. README.md and AGENTS.md now describe the application. Git remotes and repository settings were not changed.")
	return err
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func (o options) validate() error {
	if !namePattern.MatchString(o.name) || o.name == "go-cli-template" {
		return errors.New("--name must be a new lowercase executable name using letters, numbers, and hyphens")
	}
	// Keep values safe in the template's shell recipes, TOML parser, and Markdown.
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`).MatchString(o.module) || !strings.Contains(strings.Split(o.module, "/")[0], ".") || strings.Contains(o.module, "..") || strings.Contains(o.module, "//") || strings.HasSuffix(o.module, "/") {
		return errors.New("--module must be a full Go module path without shell metacharacters")
	}
	for label, value := range map[string]string{"description": o.description, "author": o.author} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00\"\\") {
			return fmt.Errorf("--%s is required and must be one line without quotes or backslashes", label)
		}
	}
	for label, value := range map[string]string{"repository": o.repository} {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !regexp.MustCompile(`^https://[A-Za-z0-9._:/-]+$`).MatchString(value) {
			return fmt.Errorf("--%s must be a plain HTTPS URL", label)
		}
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_/-]*$`).MatchString(o.branch) || strings.Contains(o.branch, "//") || strings.HasSuffix(o.branch, "/") {
		return errors.New("invalid --default-branch")
	}
	if !versionPattern.MatchString(o.version) {
		return errors.New("--version must be a semantic version such as 0.1.0")
	}
	return nil
}

func findRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "internal/package/package.toml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("cannot locate template root (internal/package/package.toml)")
		}
		dir = parent
	}
}

func metadata(data []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, " = ")
		if ok {
			if v, err := strconv.Unquote(strings.TrimSpace(value)); err == nil {
				values[strings.TrimSpace(key)] = v
			}
		}
	}
	return values
}

func makePlan(o options) (plan, error) {
	p := plan{root: o.root}
	raw, err := os.ReadFile(filepath.Join(o.root, "internal/package/package.toml"))
	if err != nil {
		return p, err
	}
	old := metadata(raw)
	if old["name"] == "" || old["module"] == "" {
		return p, errors.New("metadata is missing name or module")
	}
	if old["name"] != "go-cli-template" && (old["name"] != o.name || old["module"] != o.module) {
		return p, errors.New("application is already customized; setup only supports the initial rename or an identical rerun")
	}
	mod, err := os.ReadFile(filepath.Join(o.root, "go.mod"))
	if err != nil {
		return p, err
	}
	if !strings.Contains(string(mod), "module "+old["module"]+"\n") {
		return p, errors.New("go.mod and package metadata disagree")
	}
	if old["name"] != o.name {
		p.moves = []move{{"cmd/" + old["name"], "cmd/" + o.name}}
		for _, m := range p.moves {
			if _, err := os.Lstat(filepath.Join(o.root, m.to)); !os.IsNotExist(err) {
				return p, fmt.Errorf("destination already exists or is inaccessible: %s", m.to)
			}
			if _, err := os.Lstat(filepath.Join(o.root, m.from)); err != nil {
				return p, err
			}
		}
	}
	rootFS, err := os.OpenRoot(o.root)
	if err != nil {
		return p, err
	}
	defer func() { _ = rootFS.Close() }()
	err = filepath.WalkDir(o.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(o.root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			// Only visit application-owned source, examples and workflow trees.
			top := strings.Split(rel, "/")[0]
			if !map[string]bool{"cmd": true, "internal": true, "examples": true, "tests": true, ".github": true}[top] {
				return filepath.SkipDir
			}
			return nil
		}
		if !editable(rel) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in setup scope: %s", rel)
		}
		before, e := rootFS.ReadFile(rel)
		if e != nil {
			return e
		}
		after, e := transform(rel, before, old, o)
		if e != nil {
			return fmt.Errorf("%s: %w", rel, e)
		}
		if !bytes.Equal(before, after) {
			info, e := d.Info()
			if e != nil {
				return e
			}
			p.edits = append(p.edits, edit{rel, before, after, info.Mode()})
		}
		if bytes.Contains(after, []byte("go-cli-template")) || bytes.Contains(after, []byte("devan.gg")) {
			p.leftovers = append(p.leftovers, rel)
		}
		return nil
	})
	if err != nil {
		return p, err
	}
	if err := p.checkDirty(); err != nil {
		return p, err
	}
	return p, nil
}

func editable(path string) bool {
	if strings.Contains(strings.ToLower(path), "license") || strings.Contains(strings.ToLower(filepath.Base(path)), "notice") || strings.Contains(path, "/third_party/") {
		return false
	}
	if !strings.Contains(path, "/") {
		switch path {
		case "README.md", "CONTRIBUTING.md", "AGENTS.md", "CLAUDE.md", "copilot-instructions.md", "Makefile", "justfile", "go.mod", ".golangci.yml", "example-config.toml":
			return true
		}
		return false
	}
	switch filepath.Ext(path) {
	case ".go", ".md", ".mjs", ".yml", ".yaml", ".toml":
		return true
	}
	return false
}

func transform(path string, before []byte, old map[string]string, o options) ([]byte, error) {
	// Generate application-oriented entry points exactly once. A rerun must not
	// replace documentation subsequently written by the application team.
	if old["name"] == "go-cli-template" {
		switch path {
		case "README.md":
			return []byte(appReadme(o)), nil
		case "AGENTS.md":
			return []byte(appAgentRules(o)), nil
		case "CONTRIBUTING.md":
			return []byte(appContributing(o)), nil
		}
	}
	s := string(before)
	if path == "internal/package/package.toml" {
		vals := map[string]string{"name": o.name, "package_name": o.name, "module": o.module, "short": o.description, "description": o.description, "version": o.version, "homepage": strings.TrimRight(o.repository, "/"), "repository": strings.TrimRight(o.repository, "/"), "author": o.author}
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			key, _, ok := strings.Cut(line, " = ")
			if value, exists := vals[key]; ok && exists {
				lines[i] = key + " = " + strconv.Quote(value)
			}
		}
		return []byte(strings.Join(lines, "\n")), nil
	}
	lines := strings.Split(s, "\n")
	provenance := false
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			provenance = strings.Contains(strings.ToLower(line), "provenance")
		}
		if provenance || strings.Contains(strings.ToLower(line), "upstream releases") {
			continue
		}
		// Replace complete old values in one pass so replacement text is never rewritten.
		pairs := []string{}
		if old["repository"] != "" {
			pairs = append(pairs, strings.TrimRight(old["repository"], "/"), strings.TrimRight(o.repository, "/"))
		}
		if old["homepage"] != "" {
			pairs = append(pairs, old["homepage"], strings.TrimRight(o.repository, "/"))
		}
		pairs = append(pairs, old["module"], o.module)
		pairs = append(pairs, old["name"], o.name)
		line = strings.NewReplacer(pairs...).Replace(line)
		repository := strings.TrimRight(o.repository, "/")
		line = strings.ReplaceAll(line, repository+"/blob/main/", repository+"/blob/"+o.branch+"/")
		line = strings.ReplaceAll(line, repository+"/tree/main/", repository+"/tree/"+o.branch+"/")
		if path == "README.md" && strings.HasPrefix(line, "# Go CLI Starter") {
			line = "# " + o.name + "\n\n" + o.description
		}
		if strings.HasPrefix(path, ".github/workflows/") {
			line = strings.ReplaceAll(line, "branches: [main]", "branches: ["+o.branch+"]")
		}
		lines[i] = line
	}
	s = strings.Join(lines, "\n")
	if filepath.Ext(path) == ".go" && s != string(before) {
		return format.Source([]byte(s))
	}
	return []byte(s), nil
}

// These are application entry points, not starter documentation. Package and
// contributor notices remain in their original licensed locations.
func appReadme(o options) string {
	return fmt.Sprintf(`# %s

%s

## Run

Requires the Go version declared in go.mod.

`+"```sh"+`
go run ./cmd/%s --help
make build
./dist/%s --help
`+"```"+`

The included commands and interactive directory list are examples. Replace them
with application behavior. Use `+"`--help`"+` for command-specific usage.

## Development

`+"```sh"+`
go test ./...
make format-check
`+"```"+`

See AGENTS.md for the concise coding policy. No documentation website or
command-reference pages are required. Update this README only for meaningful
onboarding or user-workflow changes.

## Attribution

The starting point was [go-cli-template](https://github.com/imdevan/go-cli-template)
and [ClackBT](https://github.com/rigerc/clackbt). Preserve licenses and notices in
`+"`internal/clack/`"+`, `+"`third_party/`"+`, and `+"`.agents/skills/`"+`.
`, o.name, o.description, o.name, o.name)
}

func appAgentRules(o options) string {
	return fmt.Sprintf(`# Agent instructions — %s

This repository is the **%s application**, not the ClackBT starter.
Bundled example commands and agent skills are references, not product
requirements. Favor application behavior over starter maintenance.

- Implement the requested behavior, keep Cobra parsing thin, and use testable
  packages under `+"`internal/`"+`.
- Add relevant success/error-path tests and run `+"`go test ./...`"+` when possible.
- Keep flags, Cobra command descriptions, examples, and `+"`--help`"+` accurate.
- Do **not** update README or other Markdown for routine commands, bug fixes,
  and refactors. Update README when installation or essential user-facing
  workflows materially change or the task explicitly asks for docs.
- Do not create flow specifications, storyboards, documentation sites, or
  generated artifacts unless explicitly requested. The optional
  `+"`clackbt-flow-spec`"+` skill writes to ignored `+"`.work/flow-spec/`"+`.
- Treat demonstration commands, prompts, and the read-only directory-list UI
  as replaceable examples, not product requirements.
- Preserve licenses and upstream notices in `+"`internal/clack/`"+`,
  `+"`third_party/`"+`, and bundled skills.
- Do not run publishing, sync or external generators unless requested.
- Summarize changed behavior and verification in chat; don't produce a new
  Markdown report for every task.
`, o.name, o.name)
}

func appContributing(o options) string {
	return fmt.Sprintf(`# Contributing to %s

Implement changes in the application source, add focused tests, and run:

`+"```sh"+`
go test ./...
make format-check
make build
`+"```"+`

`+"`make check`"+` also requires optional lint tooling. Keep Cobra `+"`--help`"+`
accurate. Update README only when essential user workflows or onboarding
change. Do not maintain template documentation, generate planning artifacts, or
publish unless the task explicitly requires it. Preserve third-party notices.
`, o.name)
}

func (p plan) checkDirty() error {
	// Archives without Git are supported. A broken Git checkout is an error.
	probe := exec.Command("git", "-C", p.root, "rev-parse", "--show-toplevel") // #nosec G204 -- fixed command and separate root argument, no shell.
	root, err := probe.Output()
	if err != nil {
		if _, e := os.Stat(filepath.Join(p.root, ".git")); e == nil {
			return fmt.Errorf("cannot inspect Git checkout: %w", err)
		}
		return nil
	}
	if filepath.Clean(strings.TrimSpace(string(root))) != filepath.Clean(p.root) {
		return errors.New("setup root must be the Git repository root")
	}
	cmd := exec.Command("git", "-C", p.root, "status", "--porcelain=v1", "-z", "--untracked-files=all") // #nosec G204 -- fixed command and separate root argument, no shell.
	data, err := cmd.Output()
	if err != nil {
		return err
	}
	affected := func(path string) bool {
		for _, e := range p.edits {
			if path == e.path {
				return true
			}
		}
		for _, m := range p.moves {
			if path == m.from || strings.HasPrefix(path, m.from+"/") || path == m.to || strings.HasPrefix(path, m.to+"/") {
				return true
			}
		}
		return false
	}
	entries := bytes.Split(data, []byte{0})
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		path := string(entry[3:])
		if affected(path) {
			return fmt.Errorf("affected path has existing changes: %s; commit or stash it first", path)
		}
		if entry[0] == 'R' || entry[0] == 'C' || entry[1] == 'R' || entry[1] == 'C' {
			i++
			if i < len(entries) && affected(string(entries[i])) {
				return errors.New("affected path has an existing Git rename")
			}
		}
	}
	return nil
}

func (p plan) target(path string) string {
	for _, m := range p.moves {
		if path == m.from {
			return m.to
		}
		if strings.HasPrefix(path, m.from+"/") {
			return m.to + strings.TrimPrefix(path, m.from)
		}
	}
	return path
}
func (p plan) print(out io.Writer) error {
	var report strings.Builder
	for _, m := range p.moves {
		fmt.Fprintf(&report, "rename %s -> %s\n", m.from, m.to)
	}
	for _, e := range p.edits {
		fmt.Fprintf(&report, "--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", e.path, p.target(e.path), lineCount(e.before), lineCount(e.after))
		for _, part := range []struct {
			data   []byte
			prefix string
		}{{e.before, "-"}, {e.after, "+"}} {
			for _, line := range strings.SplitAfter(string(part.data), "\n") {
				if line != "" {
					report.WriteString(part.prefix)
					report.WriteString(line)
					if !strings.HasSuffix(line, "\n") {
						report.WriteString("\n\\ No newline at end of file\n")
					}
				}
			}
		}
	}
	fmt.Fprintf(&report, "%d file edits, %d renames.\n", len(p.edits), len(p.moves))
	if len(p.leftovers) > 0 {
		report.WriteString("Retained template references (review attribution and any application references):\n")
		for _, path := range p.leftovers {
			report.WriteString("  " + path + "\n")
		}
	}
	report.WriteString("Excluded: archived template history, licenses, third-party skills, generated assets, and local configuration.\n")
	_, err := io.WriteString(out, report.String())
	return err
}

func lineCount(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".setup-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err = f.Chmod(mode.Perm()); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (p plan) apply(write func(string, []byte, os.FileMode) error) (result error) {
	if err := p.checkDirty(); err != nil {
		return err
	}
	for _, e := range p.edits {
		now, err := os.ReadFile(filepath.Join(p.root, e.path))
		if err != nil {
			return err
		}
		if !bytes.Equal(now, e.before) {
			return fmt.Errorf("file changed since planning: %s", e.path)
		}
	}
	for _, m := range p.moves {
		if _, err := os.Lstat(filepath.Join(p.root, m.to)); !os.IsNotExist(err) {
			return fmt.Errorf("destination appeared since planning: %s", m.to)
		}
	}
	moved, written := 0, 0
	defer func() {
		if result == nil {
			return
		}
		var rollback []error
		for i := written - 1; i >= 0; i-- {
			e := p.edits[i]
			if err := atomicWrite(filepath.Join(p.root, p.target(e.path)), e.before, e.mode); err != nil {
				rollback = append(rollback, err)
			}
		}
		for i := moved - 1; i >= 0; i-- {
			m := p.moves[i]
			if err := os.Rename(filepath.Join(p.root, m.to), filepath.Join(p.root, m.from)); err != nil {
				rollback = append(rollback, err)
			}
		}
		if len(rollback) > 0 {
			result = errors.Join(result, fmt.Errorf("rollback failed: %w", errors.Join(rollback...)))
		}
	}()
	for _, m := range p.moves {
		if err := os.Rename(filepath.Join(p.root, m.from), filepath.Join(p.root, m.to)); err != nil {
			return err
		}
		moved++
	}
	for _, e := range p.edits {
		written++
		if err := write(filepath.Join(p.root, p.target(e.path)), e.after, e.mode); err != nil {
			return err
		}
	}
	return nil
}

func verify(o options, out, errOut io.Writer) error {
	commands := [][]string{}
	if o.verify {
		commands = append(commands, []string{"make", "format-check"}, []string{"go", "test", "./..."}, []string{"make", "build"}, []string{"./dist/" + o.name, "--help"}, []string{"./dist/" + o.name, "-v"}, []string{"./dist/" + o.name, "completion", "bash"})
	}
	if o.check {
		commands = append(commands, []string{"make", "check"})
	}
	if o.skills {
		commands = append(commands, []string{"make", "skills"})
	}
	for _, args := range commands {
		if _, err := fmt.Fprintln(out, "Running:", strings.Join(args, " ")); err != nil {
			return err
		}
		cmd := exec.Command(args[0], args[1:]...) // #nosec G204 -- commands are selected above; CLI name is validated and no shell is used.
		cmd.Dir = o.root
		cmd.Stdout = out
		cmd.Stderr = errOut
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w (check toolchain/dependency availability)", strings.Join(args, " "), err)
		}
	}
	return nil
}
