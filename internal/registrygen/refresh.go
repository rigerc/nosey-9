package registrygen

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/gitpolicy"
)

const (
	upstreamBudget    = 45 * time.Second
	upstreamFileLimit = 16 << 20
)

// Fetcher resolves one declared upstream file to a full revision and its bytes.
type Fetcher interface {
	Fetch(ctx context.Context, upstream Upstream) (revision string, data []byte, err error)
}

// GitFetcher fetches a single file through a hardened temporary repository.
type GitFetcher struct {
	// Client performs the retrieval; nil uses a Hardened client under TempDir.
	Client  *gitclient.Client
	TempDir string
}

// Fetch resolves the upstream ref to a commit, then reads only the declared path.
func (g GitFetcher) Fetch(ctx context.Context, upstream Upstream) (string, []byte, error) {
	if err := gitpolicy.Validate(upstream.URL, upstream.Ref); err != nil {
		return "", nil, fmt.Errorf("upstream %s: %w", upstream.URL, err)
	}
	ctx, cancel := context.WithTimeout(ctx, upstreamBudget)
	defer cancel()
	client := g.Client
	if client == nil {
		client = gitclient.New(gitclient.Options{Policy: gitclient.Hardened(), Credentials: gitclient.SharedCredentials(), TempDir: g.TempDir})
	}
	repo, err := client.Temporary(ctx, upstream.URL, gitclient.TemporaryOptions{})
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = repo.Close() }()
	revision, err := repo.FetchRef(ctx, upstream.Ref)
	if err != nil {
		return "", nil, err
	}
	if len(revision) != 40 {
		return "", nil, fmt.Errorf("upstream %s: invalid resolved revision %q", upstream.URL, revision)
	}
	data, err := repo.ReadFile(ctx, revision, upstream.Path, upstreamFileLimit)
	if err != nil {
		return "", nil, err
	}
	return revision, data, nil
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
