package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	sourceRemote   = "remote"
	sourceCheckout = "checkout"
	fetchTimeout   = 20 * time.Second
	staleViewAge   = 24 * time.Hour
)

// Source says which copy of the registry the studio is showing.
type Source struct {
	Mode   string `json:"mode"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	// Behind and Ahead compare the local checkout with the shown commit.
	Behind  int    `json:"behind"`
	Ahead   int    `json:"ahead"`
	Dirty   bool   `json:"dirty"`
	Warning string `json:"warning"`
}

func rootKey(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:8])
}

// Sync points the studio at the latest remote default branch, or at the local
// checkout when that is requested or unavailable. The checkout itself is only
// left untouched, including its objects: it is often a submodule pinned by another
// repository, so the fetch lands in a private repository under the studio cache.
func (s *Server) Sync(ctx context.Context) Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sync(ctx)
}

func (s *Server) sync(ctx context.Context) Source {
	if s.opts.Local {
		s.useCheckout("")
		return s.source
	}
	git := s.gitState(ctx)
	if git.unsupported {
		s.useCheckout("This checkout uses SHA-256 or reftable Git storage, which the studio cannot read, so it is showing the local files.")
		return s.source
	}
	if !git.Repository {
		s.useCheckout("This registry is not a Git checkout, so the studio is editing its files directly.")
		return s.source
	}
	if !git.Publishable {
		s.useCheckout("This checkout has no origin remote, so the studio is showing the local files.")
		return s.source
	}
	parent := s.cacheDir()
	if err := os.MkdirAll(parent, 0o700); err != nil {
		s.useCheckout("Could not prepare the studio cache: " + err.Error())
		return s.source
	}
	removeStaleViews(parent)
	user, err := s.gitClient(parent).OpenUser(s.root)
	if err != nil {
		s.useCheckout("Could not read this checkout: " + storage.Safe(err.Error()))
		return s.source
	}
	base := defaultBranch(user)
	ref := "origin/" + base
	fetch, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	snapshot, commit, err := user.FetchOriginSnapshot(fetch, "refs/heads/"+base)
	if err != nil {
		s.useCheckout("Could not fetch " + ref + ", so this may be out of date: " + storage.Safe(err.Error()))
		return s.source
	}
	defer func() { _ = snapshot.Close() }()
	view, err := s.exportCommit(ctx, parent, user, snapshot, commit)
	if err != nil {
		s.useCheckout("Could not load " + ref + ": " + err.Error())
		return s.source
	}
	cfg, err := registrygen.Load(view)
	if err != nil {
		_ = os.RemoveAll(view)
		s.useCheckout(ref + " is not a valid registry: " + storage.Safe(err.Error()))
		return s.source
	}
	s.releaseView()
	s.view, s.exported = view, view
	s.setConfig(cfg)
	s.source = Source{Mode: sourceRemote, Ref: ref, Commit: commit}
	s.compareCheckout(ctx, user, snapshot, commit)
	return s.source
}

func (s *Server) cacheDir() string {
	return filepath.Join(s.opts.Store.Cache, "studio", rootKey(s.root))
}

// useCheckout shows the local files, keeping any explanation visible in the UI.
func (s *Server) useCheckout(warning string) {
	s.releaseView()
	s.view = s.root
	if cfg, err := registrygen.Load(s.root); err == nil {
		s.setConfig(cfg)
	}
	s.source = Source{Mode: sourceCheckout, Warning: warning, Commit: s.gitState(context.Background()).Commit}
}

func (s *Server) setConfig(cfg registrygen.Config) {
	s.cfg = cfg
	s.automated = cfg.Registry.ID == officialID
}

// exportCommit writes a commit's tree to a private directory. It reads the
// snapshot, which holds the fetched objects and sees the checkout's through its
// alternates. A partial clone can lack blobs or trees that the snapshot's incremental
// fetch relied on, so a missing object is fetched from origin into a temporary
// repository instead of failing.
func (s *Server) exportCommit(ctx context.Context, parent string, user, snapshot *gitclient.Repo, commit string) (string, error) {
	view, err := os.MkdirTemp(parent, "view-")
	if err != nil {
		return "", err
	}
	err = snapshot.MaterializeVerbatimLinks(ctx, commit, view)
	if kind := gitclient.KindOf(err); kind == gitclient.KindMissingBlob || kind == gitclient.KindMissingRef {
		fetch, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		err = user.ExportOrigin(fetch, commit, view)
	}
	if err != nil {
		_ = os.RemoveAll(view)
		return "", requestError{storage.Safe(err.Error())}
	}
	return view, nil
}

// removeStaleViews clears exports left behind by sessions that did not exit
// cleanly, without disturbing another session still running on this registry.
func removeStaleViews(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr == nil && time.Since(info.ModTime()) > staleViewAge {
			_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
		}
	}
}

func (s *Server) releaseView() {
	if s.exported != "" {
		_ = os.RemoveAll(s.exported)
		s.exported = ""
	}
}

// Close removes the exported snapshot.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseView()
}

// compareCheckout records how the local checkout relates to the shown commit,
// so local work that the remote view hides is called out. Failures leave the
// counts at zero, as they did with the CLI, and the checkout is only read. A dirty
// check that cannot finish warns instead of reporting clean, because a repository
// format the adapter cannot decode would otherwise hide local changes.
func (s *Server) compareCheckout(ctx context.Context, user, snapshot *gitclient.Repo, commit string) {
	if head, err := user.Head(); err == nil && head != "" {
		if ahead, behind, compareErr := snapshot.AheadBehind(ctx, head, commit); compareErr == nil {
			s.source.Ahead, s.source.Behind = ahead, behind
		}
	}
	dirty, err := user.Dirty(ctx)
	if err != nil {
		s.source.Warning = "Could not check this checkout for local changes, so it may not be clean: " + storage.Safe(err.Error())
		return
	}
	s.source.Dirty = dirty
}

func (s *Server) handleRefresh(r *http.Request) (response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ok(s.sync(r.Context()))
}
