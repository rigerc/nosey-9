package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
// fetched into, never moved: it is often a submodule pinned by another repository.
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
	if !git.Repository {
		s.useCheckout("This registry is not a Git checkout, so the studio is editing its files directly.")
		return s.source
	}
	if !git.Publishable {
		s.useCheckout("This checkout has no origin remote, so the studio is showing the local files.")
		return s.source
	}
	base := s.defaultBranch(ctx)
	ref := "origin/" + base
	fetch, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	if _, err := s.git(fetch, s.root, "fetch", "--no-tags", "origin", base); err != nil {
		s.useCheckout("Could not fetch " + ref + ", so this may be out of date: " + err.Error())
		return s.source
	}
	commit, err := s.git(ctx, s.root, "rev-parse", "FETCH_HEAD")
	if err != nil {
		s.useCheckout("Could not read " + ref + ": " + err.Error())
		return s.source
	}
	view, err := s.exportCommit(ctx, commit)
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
	s.compareCheckout(ctx, commit)
	return s.source
}

// useCheckout shows the local files, keeping any explanation visible in the UI.
func (s *Server) useCheckout(warning string) {
	s.releaseView()
	s.view = s.root
	if cfg, err := registrygen.Load(s.root); err == nil {
		s.setConfig(cfg)
	}
	s.source = Source{Mode: sourceCheckout, Warning: warning}
	background := context.Background()
	if s.gitState(background).Repository {
		s.source.Commit, _ = s.git(background, s.root, "rev-parse", "HEAD")
	}
}

func (s *Server) setConfig(cfg registrygen.Config) {
	s.cfg = cfg
	s.automated = cfg.Registry.ID == officialID
}

// exportCommit writes a commit's tree to a private directory. A temporary index
// keeps the user's index, HEAD and worktree list untouched.
func (s *Server) exportCommit(ctx context.Context, commit string) (string, error) {
	parent := filepath.Join(s.opts.Store.Cache, "studio", rootKey(s.root))
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	removeStaleViews(parent)
	view, err := os.MkdirTemp(parent, "view-")
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp(parent, "index-")
	if err != nil {
		_ = os.RemoveAll(view)
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	env := append(append([]string{}, gitEnv...), "GIT_INDEX_FILE="+filepath.Join(scratch, "index"))
	for _, args := range [][]string{{"read-tree", commit}, {"checkout-index", "--all", "--force", "--prefix=" + view + "/"}} {
		if output, runErr := s.opts.Runner.Run(ctx, s.root, env, "git", args...); runErr != nil {
			_ = os.RemoveAll(view)
			return "", requestError{storage.Safe(strings.TrimSpace(string(output.Stderr)) + " " + runErr.Error())}
		}
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
// so local work that the remote view hides is called out.
func (s *Server) compareCheckout(ctx context.Context, commit string) {
	if counts, err := s.git(ctx, s.root, "rev-list", "--left-right", "--count", "HEAD..."+commit); err == nil {
		if fields := strings.Fields(counts); len(fields) == 2 {
			s.source.Ahead, _ = strconv.Atoi(fields[0])
			s.source.Behind, _ = strconv.Atoi(fields[1])
		}
	}
	if status, err := s.git(ctx, s.root, "status", "--porcelain"); err == nil {
		s.source.Dirty = status != ""
	}
}

func (s *Server) handleRefresh(r *http.Request) (response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ok(s.sync(r.Context()))
}
