package installation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// npxLocateScript asks npx where the pinned CLI lives; it is a constant with no user input.
const npxLocateScript = "command -v skills"

// CLI is a resolved upstream Skills CLI that is safe to start with the workspace as its working directory.
type CLI struct {
	Name    string
	Args    []string
	Backend string
}

func (c CLI) argv(args ...string) (string, []string) {
	return c.Name, append(slices.Clone(c.Args), args...)
}

// resolveCLI prefers an installed CLI of the pinned version and otherwise lets npx fetch it.
// Discovery runs from Nosey's cache directory so a workspace's node_modules and .npmrc are never consulted.
func (s Service) resolveCLI(ctx context.Context) (CLI, error) {
	if s.Resolver != nil {
		return s.Resolver(ctx)
	}
	neutral := s.Store.Cache
	if err := os.MkdirAll(neutral, 0o700); err != nil {
		return CLI{}, PrerequisiteError{Reason: "skills CLI cannot be located: " + err.Error()}
	}
	if path, err := exec.LookPath("skills"); err == nil {
		cli := CLI{Name: path, Backend: "installed"}
		if s.verifyCLI(ctx, neutral, cli, path) == nil {
			return cli, nil
		}
	}
	return s.resolveNpx(ctx, neutral)
}

func (s Service) resolveNpx(ctx context.Context, neutral string) (CLI, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return CLI{}, PrerequisiteError{Reason: "skills@" + Version + " requires Node >=22.20.0, but node was not found"}
	}
	out, err := s.run(ctx, neutral, s.env(), "npx", "--yes", "--package=skills@"+Version, "-c", npxLocateScript)
	if err != nil {
		return CLI{}, PrerequisiteError{Reason: "skills@" + Version + " could not be started with npx; install it or make npx available"}
	}
	path := lastLine(string(out.Stdout))
	if !filepath.IsAbs(path) {
		return CLI{}, PrerequisiteError{Reason: "npx did not report a usable skills@" + Version + " path"}
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return CLI{}, PrerequisiteError{Reason: "npx reported a missing skills@" + Version + " path"}
	}
	cli := CLI{Name: node, Args: []string{target}, Backend: "npx"}
	if err = s.verifyCLI(ctx, neutral, cli, target); err != nil {
		return CLI{}, err
	}
	return cli, nil
}

// verifyCLI rejects a CLI inside the workspace before running it, then confirms the pinned version.
func (s Service) verifyCLI(ctx context.Context, dir string, cli CLI, path string) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return PrerequisiteError{Reason: "skills CLI path cannot be resolved"}
	}
	if rel, relErr := filepath.Rel(s.Store.Workspace, target); relErr == nil && filepath.IsLocal(rel) {
		return PrerequisiteError{Reason: "refusing to run a skills CLI from inside the workspace"}
	}
	name, args := cli.argv("--version")
	out, err := s.run(ctx, dir, s.env(), name, args...)
	if err != nil {
		return PrerequisiteError{Reason: fmt.Sprintf("skills CLI at %s failed its version check", path)}
	}
	if version := strings.TrimSpace(string(out.Stdout)); version != Version {
		return PrerequisiteError{Reason: "expected skills@" + Version + ", found " + version}
	}
	return nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
