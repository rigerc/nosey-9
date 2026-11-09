package detection

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (e *engine) excluded(path string) bool {
	for _, pattern := range e.settings.Detection.Exclude {
		ok, _ := doublestar.PathMatch(pattern, path)
		dir, _ := doublestar.PathMatch(pattern, path+"/")
		if ok || dir {
			return true
		}
	}
	return false
}

// UnsupportedRepositoryError reports a repository whose storage format Nosey
// cannot read. Scanning fails instead of walking the tree, because a walk would
// silently ignore the repository's .gitignore rules.
type UnsupportedRepositoryError struct {
	Root string
	Err  error
}

func (e *UnsupportedRepositoryError) Error() string {
	return fmt.Sprintf("cannot scan %s: this Git repository uses SHA-256 objects or reftable refs, which Nosey cannot read; "+
		"use a SHA-1 repository with the files ref backend (reftable can be converted with: git refs migrate --ref-format=files)", e.Root)
}

// Unwrap exposes the classified gitclient error.
func (e *UnsupportedRepositoryError) Unwrap() error { return e.Err }

// listRepositoryFiles lists the workspace like git ls-files --cached --others
// --exclude-standard. It needs no network, so no credentials are looked up. Any
// error except unsupported storage sends the caller to the plain walk.
func (e *engine) listRepositoryFiles() ([]string, error) {
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	repo, err := gitclient.New(gitclient.Options{Policy: gitclient.Hardened()}).OpenUser(e.root)
	var listed []string
	if err == nil {
		listed, err = repo.ListWorktreeFiles(ctx, e.root)
	}
	if err != nil {
		if gitclient.KindOf(err) == gitclient.KindUnsupportedFormat {
			return nil, &UnsupportedRepositoryError{Root: e.root, Err: err}
		}
		return nil, err
	}
	names := make([]string, 0, len(listed))
	for _, name := range listed {
		if name != "" && filepath.IsLocal(name) {
			names = append(names, filepath.ToSlash(name))
		}
	}
	return names, nil
}

func (e *engine) walk() error {
	names, err := e.listRepositoryFiles()
	if err != nil {
		var unsupported *UnsupportedRepositoryError
		if errors.As(err, &unsupported) {
			return err
		}
		names = []string{}
		err = filepath.WalkDir(e.root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				e.truncated = true
				return nil
			}
			if e.ctx.Err() != nil {
				return e.ctx.Err()
			}
			rel, relErr := filepath.Rel(e.root, path)
			if relErr != nil {
				return relErr
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if strings.Count(rel, "/") > 64 || e.excluded(rel) || entry.Name() == ".git" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr == nil {
					return filepath.SkipDir
				}
				return nil
			}
			names = append(names, rel)
			if len(names) > 20000 {
				e.truncated = true
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	// Narrow explicit inspection of ignored secret-config files; values are never evidence.
	projects := map[string]bool{".": true}
	for _, name := range names {
		base := filepath.Base(name)
		if slices.Contains([]string{"package.json", "go.mod", "pyproject.toml", "Cargo.toml", "Gemfile", "composer.json", "deno.json", "deno.jsonc", "mix.exs"}, base) {
			projects[filepath.ToSlash(filepath.Dir(name))] = true
		}
	}
	special := []string{".env", ".env.local", ".env.development", ".env.production"}
	if e.settings.Detection.MCP {
		special = append(special, ".mcp.json", ".cursor/mcp.json", ".vscode/mcp.json", ".codex/config.toml")
	}
	for project := range projects {
		e.projects = append(e.projects, project)
	}
	slices.Sort(e.projects)
	for project := range projects {
		entries, readErr := os.ReadDir(filepath.Join(e.root, project))
		if readErr != nil {
			e.truncated = true
		}
		for _, entry := range entries {
			if !entry.IsDir() && (entry.Name() == ".env" || strings.HasPrefix(entry.Name(), ".env.")) {
				names = append(names, filepath.ToSlash(filepath.Join(project, entry.Name())))
			}
		}
		for _, name := range special {
			rel := filepath.ToSlash(filepath.Join(project, name))
			if _, statErr := os.Lstat(filepath.Join(e.root, rel)); statErr == nil {
				names = append(names, rel)
			}
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if e.excluded(name) {
			continue
		}
		info, statErr := os.Lstat(filepath.Join(e.root, name))
		if statErr != nil || info.IsDir() {
			continue
		}
		f := file{path: name}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(filepath.Join(e.root, name))
			if err != nil {
				f.unsafe = true
			} else {
				relative, err := filepath.Rel(e.root, target)
				f.unsafe = err != nil || !filepath.IsLocal(relative)
				if stat, err := os.Stat(target); err == nil && stat.IsDir() {
					f.directory = true
					f.unsafe = true
				}
			}
		}
		f.data, f.err = storage.Read(e.root, name, e.settings.Detection.MaxBytes)
		e.files = append(e.files, f)
		if len(e.files) >= 20000 {
			e.truncated = true
			break
		}
	}
	directories := map[string]bool{}
	for _, f := range e.files {
		for parent := filepath.ToSlash(filepath.Dir(f.path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if !e.excluded(parent) {
				directories[parent] = true
			}
		}
	}
	for path := range directories {
		e.files = append(e.files, file{path: path, directory: true})
	}
	slices.SortFunc(e.files, func(a, b file) int { return strings.Compare(a.path, b.path) })
	return nil
}

func (e *engine) projectFor(path string) string {
	best := "."
	for _, project := range e.projects {
		if project != "." && (strings.HasPrefix(path, project+"/") || path == project) && (best == "." || len(project) > len(best)) {
			best = project
		}
	}
	return best
}
