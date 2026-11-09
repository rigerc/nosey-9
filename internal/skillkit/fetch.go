package skillkit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

// fetchBudget bounds one remote fetch, including materializing the tree.
const fetchBudget = 5 * time.Minute

var fullSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// Checkout is a fetched source tree. Close removes it; it is safe to call more than once.
type Checkout struct {
	// Dir is the working tree to discover skills in. For a local source it is the source directory itself.
	Dir string
	// Ref is the ref the caller asked for, unchanged. Only this belongs in a lock entry.
	Ref string
	// Commit is the resolved commit hash; empty for local sources.
	Commit string
	// Parsed is the classified source, for lock entries.
	Parsed ParsedSource

	closer func() error
}

// Close deletes the temporary checkout.
func (c *Checkout) Close() error {
	if c == nil || c.closer == nil {
		return nil
	}
	f := c.closer
	c.closer = nil
	return f()
}

// FetchError is a typed fetch failure.
type FetchError struct {
	// Stage is init, remote, fetch, checkout or verify.
	Stage string
	Ref   string
	Err   error
	// Stderr is additional Git error output, if any. The native client reports
	// everything through Err, so it is normally empty.
	Stderr string
}

func (e *FetchError) Error() string {
	msg := "git " + e.Stage + " failed: " + e.Err.Error()
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}
func (e *FetchError) Unwrap() error { return e.Err }

// ErrCommitMismatch is wrapped by a FetchError when a full-SHA ref resolves to another commit.
var ErrCommitMismatch = errors.New("fetched commit does not match the requested SHA")

// Fetcher fetches sources into temporary directories with the native Git client.
type Fetcher struct {
	// TempDir is the parent of temporary checkouts; empty uses os.TempDir.
	TempDir string
	// Client performs the Git operations; nil uses a Hardened client (https and ssh).
	Client *gitclient.Client

	// reportedCommit replaces the resolved commit before verification, so tests can
	// exercise the mismatch check.
	reportedCommit func(string) string
}

func (f Fetcher) client() *gitclient.Client {
	if f.Client != nil {
		return f.Client
	}
	return gitclient.New(gitclient.Options{Policy: gitclient.Hardened(), Credentials: gitclient.SharedCredentials(), TempDir: f.TempDir})
}

// Fetch resolves src and returns a checkout of the requested ref: a plain tree
// without a .git directory. It fetches the ref shallowly into a private
// repository and writes the commit's tree, running no hooks, filters or
// submodule recursion, within a five minute budget. A full-SHA ref must equal the
// resulting commit or the fetch fails; there is no fallback to another revision.
// Temporary directories are removed on any failure and by Checkout.Close.
func (f Fetcher) Fetch(ctx context.Context, src Source) (*Checkout, error) {
	parsed, err := ParseSource(src)
	if err != nil {
		return nil, err
	}
	if parsed.Type == "local" {
		info, serr := os.Stat(parsed.LocalPath)
		if serr != nil {
			return nil, serr
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("local source %s is not a directory", parsed.LocalPath)
		}
		return &Checkout{Dir: parsed.LocalPath, Ref: src.Ref, Parsed: parsed}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, fetchBudget)
	defer cancel()
	dir, err := os.MkdirTemp(f.TempDir, "nosey-skills-")
	if err != nil {
		return nil, err
	}
	cleanup := func() error { return os.RemoveAll(dir) }
	commit, err := f.run(ctx, dir, parsed)
	if err != nil {
		_ = cleanup()
		return nil, err
	}
	return &Checkout{Dir: dir, Ref: src.Ref, Commit: commit, Parsed: parsed, closer: cleanup}, nil
}

func (f Fetcher) run(ctx context.Context, dir string, p ParsedSource) (string, error) {
	failed := func(stage string, err error) error {
		return &FetchError{Stage: stage, Ref: p.Ref, Err: err}
	}
	repo, err := f.client().Temporary(ctx, p.URL, gitclient.TemporaryOptions{})
	if err != nil {
		return "", failed("init", err)
	}
	defer func() { _ = repo.Close() }()
	commit, err := repo.FetchRef(ctx, p.Ref)
	if err != nil {
		return "", failed("fetch", err)
	}
	// Links stay verbatim, as a Git checkout left them: Discover confines reads with
	// os.Root and the copier warns about and skips links that leave the skill.
	if err = repo.MaterializeVerbatimLinks(ctx, commit, dir); err != nil {
		return "", failed("checkout", err)
	}
	if f.reportedCommit != nil {
		commit = f.reportedCommit(commit)
	}
	if fullSHA.MatchString(p.Ref) && !strings.EqualFold(commit, p.Ref) {
		return "", failed("verify", fmt.Errorf("%w: requested %s, got %s", ErrCommitMismatch, p.Ref, commit))
	}
	return commit, nil
}
