package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/storage"
)

// snapshotBudget bounds one retrieval: what the clone and fetch commands it
// replaces were each allowed, 45 seconds.
const snapshotBudget = 90 * time.Second

// Snapshot identifies one validated data source and immutable Git revision.
type Snapshot struct {
	Root     string
	Revision *string
	Manifest domain.Manifest
	Stale    bool
}

type lock struct{ path string }

func acquire(ctx context.Context, path string) (lock, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return lock{}, err
	}
	for {
		// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return lock{path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return lock{}, err
		}
		select {
		case <-ctx.Done():
			return lock{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (l lock) close() { _ = os.Remove(l.path) }

// Resolve loads local data or promotes a validated immutable Git snapshot.
func (s Service) Resolve(ctx context.Context, ref domain.RegistryRef, ttl int, refresh bool) (Snapshot, error) {
	if ref.Source.Kind == "local" {
		root, err := filepath.EvalSymlinks(ref.Source.Path)
		if err != nil {
			return Snapshot{}, err
		}
		m, err := readManifest(root)
		return Snapshot{Root: root, Manifest: m}, err
	}
	if err := ValidateGit(ref.Source.URI, ref.Source.Ref); err != nil {
		return Snapshot{}, err
	}
	if ref.TTLHours > 0 {
		ttl = ref.TTLHours
	}
	key := storage.Hash([]byte(ref.Source.URI + "#" + ref.Source.Ref))
	dir := filepath.Join(s.Store.Cache, "registries", key)
	pointer := filepath.Join(dir, "current.json")
	var saved struct {
		Revision string    `json:"revision"`
		Fetched  time.Time `json:"fetched"`
	}
	// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
	data, err := os.ReadFile(pointer)
	if err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	previous := func() (Snapshot, error) {
		if saved.Revision == "" {
			return Snapshot{}, fmt.Errorf("registry unavailable: %s", ref.ID)
		}
		root := filepath.Join(dir, saved.Revision)
		m, readErr := readManifest(root)
		sha := saved.Revision
		return Snapshot{Root: root, Revision: &sha, Manifest: m, Stale: true}, readErr
	}
	if saved.Revision != "" && !refresh && time.Since(saved.Fetched) < time.Duration(ttl)*time.Hour {
		snap, readErr := previous()
		snap.Stale = false
		return snap, readErr
	}
	l, err := acquire(ctx, dir+".lock")
	if err != nil {
		return Snapshot{}, err
	}
	defer l.close()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return Snapshot{}, err
	}
	stage, err := os.MkdirTemp(dir, "fetch-")
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	revision, err := s.export(ctx, ref.Source, stage)
	if err == nil {
		err = validateSource(stage)
	}
	if err != nil {
		snap, oldErr := previous()
		if oldErr == nil {
			return snap, nil
		}
		return Snapshot{}, fmt.Errorf("registry %s refresh failed: %w", ref.ID, err)
	}
	destination := filepath.Join(dir, revision)
	if _, statErr := os.Stat(destination); errors.Is(statErr, os.ErrNotExist) {
		if err = os.Rename(stage, destination); err != nil {
			return Snapshot{}, err
		}
	}
	saved.Revision = revision
	saved.Fetched = time.Now()
	if err = storage.JSON(pointer, saved); err != nil {
		return Snapshot{}, err
	}
	m, err := readManifest(destination)
	return Snapshot{Root: destination, Revision: &revision, Manifest: m}, err
}

func readManifest(root string) (domain.Manifest, error) {
	var manifest domain.Manifest
	data, err := storage.Read(root, "nosey.registry.json", 1<<20)
	if err != nil {
		return manifest, err
	}
	if err = contracts.Validate("registry.schema.json", data); err != nil {
		return manifest, err
	}
	err = json.Unmarshal(data, &manifest)
	return manifest, err
}

func validateSource(root string) error {
	m, err := readManifest(root)
	if err != nil {
		return err
	}
	for kind, name := range m.Files {
		data, readErr := storage.Read(root, name, 16<<20)
		if readErr != nil {
			return readErr
		}
		schema := kind + ".schema.json"
		if kind == "technologies" {
			var header struct {
				Version int `json:"schemaVersion"`
			}
			if err = json.Unmarshal(data, &header); err != nil {
				return err
			}
			if header.Version == 2 {
				schema = "technologies.v2.schema.json"
			} else if header.Version != 1 {
				return fmt.Errorf("unsupported technology version %d", header.Version)
			}
		}
		if err = contracts.Validate(schema, data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// export materializes the source's requested revision into dest. Symlinks are
// kept verbatim: one that leaves the tree cannot fail the whole snapshot, and
// storage.Read refuses to follow it when validateSource reads a declared file.
func (s Service) export(ctx context.Context, source domain.RegistrySource, dest string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotBudget)
	defer cancel()
	client := s.Git
	if client == nil {
		client = gitclient.New(gitclient.Options{Policy: gitclient.Hardened(), Credentials: gitclient.SharedCredentials()})
	}
	repo, err := client.Temporary(ctx, source.URI, gitclient.TemporaryOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	commit, err := repo.FetchRef(ctx, source.Ref)
	if err != nil {
		return "", err
	}
	if err = repo.MaterializeVerbatimLinks(ctx, commit, dest); err != nil {
		return "", err
	}
	return commit, nil
}

// Inspect reports cache freshness without fetching or writing application data.
func (s Service) Inspect(ref domain.RegistryRef, ttl int) (string, *string) {
	if ref.Source.Kind == "local" {
		return "local", nil
	}
	if ref.TTLHours > 0 {
		ttl = ref.TTLHours
	}
	key := storage.Hash([]byte(ref.Source.URI + "#" + ref.Source.Ref))
	directory := filepath.Join(s.Store.Cache, "registries", key)
	data, err := storage.Read(directory, "current.json", 1<<20)
	if err != nil {
		return "missing", nil
	}
	var saved struct {
		Revision string    `json:"revision"`
		Fetched  time.Time `json:"fetched"`
	}
	if json.Unmarshal(data, &saved) != nil || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(saved.Revision) {
		return "missing", nil
	}
	if _, err = readManifest(filepath.Join(directory, saved.Revision)); err != nil {
		return "missing", nil
	}
	if time.Since(saved.Fetched) < time.Duration(ttl)*time.Hour {
		return "fresh", &saved.Revision
	}
	return "stale", &saved.Revision
}
