package registrygen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/process"
)

// Fetcher resolves one declared upstream file to a full revision and its bytes.
type Fetcher interface {
	Fetch(ctx context.Context, upstream Upstream) (revision string, data []byte, err error)
}

// GitFetcher fetches a single file from a hardened temporary Git repository.
type GitFetcher struct {
	Runner  process.Executor
	TempDir string
}

// Fetch resolves the upstream ref to a commit, then reads only the declared path.
func (g GitFetcher) Fetch(ctx context.Context, upstream Upstream) (string, []byte, error) {
	if err := gitpolicy.Validate(upstream.URL, upstream.Ref); err != nil {
		return "", nil, fmt.Errorf("upstream %s: %w", upstream.URL, err)
	}
	runner := g.Runner
	if runner == nil {
		runner = process.Runner{Limit: 16 << 20}
	}
	dir, err := os.MkdirTemp(g.TempDir, "nosey-refresh-")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	run := func(args ...string) (process.Output, error) {
		return runner.Run(ctx, dir, gitpolicy.HardenedEnv(), "git", gitpolicy.HardenedArgs(args...)...)
	}
	if _, err = run("init", "--quiet"); err != nil {
		return "", nil, err
	}
	if _, err = run("remote", "add", "origin", upstream.URL); err != nil {
		return "", nil, err
	}
	requested := upstream.Ref
	if requested == "" {
		requested = "HEAD"
	}
	if _, err = run("fetch", "--quiet", "--depth=1", "origin", requested); err != nil {
		return "", nil, err
	}
	out, err := run("rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", nil, err
	}
	revision := strings.TrimSpace(string(out.Stdout))
	if len(revision) != 40 {
		return "", nil, fmt.Errorf("upstream %s: invalid resolved revision %q", upstream.URL, revision)
	}
	out, err = run("show", revision+":"+upstream.Path)
	if err != nil {
		return "", nil, err
	}
	return revision, out.Stdout, nil
}

// Refresh fetches every declared upstream, then generates and relocks the
// registry. With no upstreams it is a successful no-op.
func Refresh(ctx context.Context, root string, fetcher Fetcher) (Result, error) {
	target, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	cfg, err := Load(target)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Version:      1,
		Command:      "refresh",
		Path:         target,
		ChangedFiles: []string{},
		RemovedFiles: []string{},
		Inputs:       []InputInfo{},
		Diagnostics:  []domain.Diagnostic{},
	}
	lock, _, lockErr := readLock(target)
	if lockErr != nil {
		return result, lockErr
	}
	result.Summary = lock.Summary
	result.Inputs = inputInfo(cfg, lock)
	if len(cfg.Upstreams) == 0 {
		return result, nil
	}
	if fetcher == nil {
		fetcher = GitFetcher{}
	}
	roles := make([]string, 0, len(cfg.Upstreams))
	for role := range cfg.Upstreams {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	overrides := map[string][]byte{}
	revisions := map[string]string{}
	for _, role := range roles {
		revision, data, err := fetcher.Fetch(ctx, cfg.Upstreams[role])
		if err != nil {
			return result, refreshError("%s: %w", role, err)
		}
		doc, err := canonicalize(role, data)
		if err != nil {
			return result, refreshError("%s: %w", role, err)
		}
		schema, err := schemaFor(role, doc.version)
		if err != nil {
			return result, refreshError("%s: %w", role, err)
		}
		if err = contracts.Validate(schema, doc.canonical); err != nil {
			return result, refreshError("%s: %w", role, err)
		}
		overrides[role] = doc.canonical
		revisions[role] = revision
	}
	return Run(target, Options{Command: "refresh", Overrides: overrides, Revisions: revisions})
}
