package skillkit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/process"
)

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
	// Stderr is the bounded Git error output, if any.
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

// Fetcher clones sources into temporary directories with Git.
type Fetcher struct {
	// Exec runs Git; nil uses a process.Runner with a five minute timeout.
	Exec process.Executor
	// TempDir is the parent of temporary checkouts; empty uses os.TempDir.
	TempDir string
	// Git is the git binary; empty uses "git".
	Git string

	// allowFile lets the credentialed fetch use the file transport. It is unexported so
	// only this package's tests can enable it; production code cannot.
	allowFile bool
}

func (f Fetcher) exec() process.Executor {
	if f.Exec != nil {
		return f.Exec
	}
	return process.Runner{Timeout: 5 * time.Minute, Limit: 1 << 20}
}

func (f Fetcher) git() string {
	if f.Git != "" {
		return f.Git
	}
	return "git"
}

// Fetch resolves src and returns a checkout of the requested ref. Git runs
// as: a hardened init, a hardened remote add, a credentialed shallow fetch of
// the ref (the only command that sees the user's Git configuration) and a
// hardened detached checkout of FETCH_HEAD. A full-SHA ref must equal the
// checked-out HEAD or the fetch fails; there is no fallback to another revision.
// The temporary directory is removed on any failure and by Checkout.Close.
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

// pinned makes git use exactly this repository and working tree, ignoring any
// GIT_DIR or discovery from the caller's environment.
func pinned(dir string) []string {
	return []string{"GIT_DIR=" + filepath.Join(dir, ".git"), "GIT_WORK_TREE=" + dir, "GIT_CONFIG_COUNT=0"}
}

func (f Fetcher) run(ctx context.Context, dir string, p ParsedSource) (string, error) {
	hardenedEnv := append(gitpolicy.HardenedEnv(), pinned(dir)...)
	step := func(stage string, env []string, args []string) (string, error) {
		out, err := f.exec().Run(ctx, dir, env, f.git(), args...)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				err = cerr
			}
			return "", &FetchError{Stage: stage, Ref: p.Ref, Err: err, Stderr: string(out.Stderr)}
		}
		return strings.TrimSpace(string(out.Stdout)), nil
	}
	if _, err := step("init", hardenedEnv, gitpolicy.HardenedArgs("init", "--quiet")); err != nil {
		return "", err
	}
	if _, err := step("remote", hardenedEnv, gitpolicy.HardenedArgs("remote", "add", "origin", p.URL)); err != nil {
		return "", err
	}
	ref := p.Ref
	if ref == "" {
		ref = "HEAD"
	}
	fetchEnv := append(gitpolicy.CredentialedEnv(), pinned(dir)...)
	if f.allowFile {
		fetchEnv = append(fetchEnv, "GIT_ALLOW_PROTOCOL=file:https:ssh")
	}
	if _, err := step("fetch", fetchEnv, gitpolicy.CredentialedArgs("fetch", "--quiet", "--depth=1", "--no-tags", "--no-recurse-submodules", "origin", ref)); err != nil {
		return "", err
	}
	if _, err := step("checkout", hardenedEnv, gitpolicy.HardenedArgs("checkout", "--quiet", "--detach", "FETCH_HEAD")); err != nil {
		return "", err
	}
	head, err := step("verify", hardenedEnv, gitpolicy.HardenedArgs("rev-parse", "HEAD"))
	if err != nil {
		return "", err
	}
	if fullSHA.MatchString(p.Ref) && !strings.EqualFold(head, p.Ref) {
		return "", &FetchError{Stage: "verify", Ref: p.Ref, Err: fmt.Errorf("%w: requested %s, got %s", ErrCommitMismatch, p.Ref, head)}
	}
	return head, nil
}
