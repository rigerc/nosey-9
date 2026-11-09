package gitclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

const (
	maxIgnoreFile = 1 << 20
	maxWalkDepth  = 256
)

// ListWorktreeFiles lists dir's files like git ls-files --cached --others
// --exclude-standard: index entries, even ignored or deleted ones, plus untracked
// files that no ignore rule matches. Paths are slash-separated and relative to
// dir. Untracked nested repositories are omitted. It honours context and MaxFiles
// and reads ignore files with a size bound.
func (r *Repo) ListWorktreeFiles(ctx context.Context, dir string) ([]string, error) {
	const op = "list worktree files"
	if _, err := r.repo.Worktree(); err != nil {
		return nil, classify(ctx, op, dir, err)
	}
	prefix, err := r.relativeToRoot(dir)
	if err != nil {
		return nil, newError(KindInvalidSource, op, dir, err)
	}
	index, err := r.repo.Storer.Index()
	if err != nil {
		return nil, classify(ctx, op, dir, err)
	}
	listing := worktreeListing{repo: r, op: op, found: map[string]struct{}{}, limit: r.client.limits.MaxFiles}
	for _, entry := range index.Entries {
		if underPrefix(entry.Name, prefix) {
			if err = listing.add(ctx, entry.Name); err != nil {
				return nil, err
			}
		}
	}
	patterns, ignored := r.ancestorPatterns(prefix)
	if !ignored {
		if err = listing.walk(ctx, prefix, patterns, 0); err != nil {
			return nil, err
		}
	}
	files := make([]string, 0, len(listing.found))
	for name := range listing.found {
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix+"/")
		}
		files = append(files, name)
	}
	slices.Sort(files)
	return files, nil
}

// gitDirFS returns the repository's Git directory.
func (r *Repo) gitDirFS() (billy.Filesystem, bool) {
	switch storage := r.repo.Storer.(type) {
	case userStorage:
		return storage.Filesystem(), true
	case *filesystem.Storage:
		return storage.Filesystem(), true
	}
	return nil, false
}

func underPrefix(name, prefix string) bool {
	return prefix == "" || strings.HasPrefix(name, prefix+"/")
}

// relativeToRoot returns dir relative to the repository root, slash-separated,
// with "" for the root itself.
func (r *Repo) relativeToRoot(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(r.dir)
	if err != nil {
		return "", err
	}
	target, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%s is outside the repository", dir)
	}
	return filepath.ToSlash(rel), nil
}

// ancestorPatterns gathers the rules that apply on entering prefix and reports
// whether an ignore rule excludes prefix itself.
func (r *Repo) ancestorPatterns(prefix string) ([]gitignore.Pattern, bool) {
	patterns := r.excludePatterns()
	if prefix == "" {
		return patterns, false
	}
	current := ""
	for component := range strings.SplitSeq(prefix, "/") {
		patterns = append(patterns, r.directoryPatterns(current)...)
		current = path.Join(current, component)
		if gitignore.NewMatcher(patterns).Match(strings.Split(current, "/"), true) {
			return patterns, true
		}
	}
	return patterns, false
}

// excludePatterns returns the repository-wide rules from lowest priority: the
// excludes file, then info/exclude.
func (r *Repo) excludePatterns() []gitignore.Pattern {
	patterns := []gitignore.Pattern{}
	if file := r.excludesFile(); file != "" {
		if data, err := readBoundedFile(file); err == nil {
			patterns = parseIgnore(data, nil)
		}
	}
	if gitDir, ok := r.gitDirFS(); ok {
		if file, err := gitDir.Open("info/exclude"); err == nil {
			defer func() { _ = file.Close() }()
			if data, readErr := io.ReadAll(io.LimitReader(file, maxIgnoreFile+1)); readErr == nil && len(data) <= maxIgnoreFile {
				patterns = append(patterns, parseIgnore(data, nil)...)
			}
		}
	}
	return patterns
}

// excludesFile resolves core.excludesFile from repository then global config,
// defaulting to Git's XDG location, which go-git's own loader omits.
func (r *Repo) excludesFile() string {
	configured := ""
	if local, err := r.repo.Storer.Config(); err == nil {
		configured = local.Raw.Section("core").Options.Get("excludesfile")
	}
	for _, file := range globalConfigPaths() {
		if configured != "" {
			break
		}
		if cfg := readConfigFile(file); cfg != nil {
			configured = cfg.Raw.Section("core").Options.Get("excludesfile")
		}
	}
	home, homeErr := os.UserHomeDir()
	if rest, ok := strings.CutPrefix(configured, "~/"); ok {
		if homeErr != nil {
			return ""
		}
		return filepath.Join(home, rest)
	}
	if configured != "" {
		if filepath.IsAbs(configured) {
			return configured
		}
		return ""
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "ignore")
	}
	if homeErr != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git", "ignore")
}

func (r *Repo) directoryPatterns(relDir string) []gitignore.Pattern {
	data, err := readBoundedFile(filepath.Join(r.dir, filepath.FromSlash(relDir), ".gitignore"))
	if err != nil {
		return nil
	}
	var domain []string
	if relDir != "" {
		domain = strings.Split(relDir, "/")
	}
	return parseIgnore(data, domain)
}

// readBoundedFile refuses non-regular files, so a FIFO cannot stall the listing.
func readBoundedFile(file string) ([]byte, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxIgnoreFile {
		return nil, errors.New("not a bounded regular file")
	}
	// #nosec G304 -- an ignore file inside the repository or Git's config location.
	return os.ReadFile(file)
}

func parseIgnore(data []byte, domain []string) []gitignore.Pattern {
	patterns := []gitignore.Pattern{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasSuffix(line, `\ `) {
			line = strings.TrimRight(line, " \t")
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, gitignore.ParsePattern(line, domain))
		}
	}
	return patterns
}

type worktreeListing struct {
	repo  *Repo
	op    string
	found map[string]struct{}
	limit int64
	// tracked, when set, switches the walk to stopping with errFoundUntracked at
	// the first file or nested repository outside it. Git lists a nested
	// repository as untracked content, so it counts here even though
	// ListWorktreeFiles omits it.
	tracked map[string]struct{}
}

func (l *worktreeListing) add(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return newError(KindCanceled, l.op, name, err)
	}
	l.found[name] = struct{}{}
	if int64(len(l.found)) > l.limit {
		return newError(KindResourceLimit, l.op, name, fmt.Errorf("more than %d files", l.limit))
	}
	return nil
}

// walk visits relDir. Like Git it does not follow symlinks and passes over
// directories it cannot read.
func (l *worktreeListing) walk(ctx context.Context, relDir string, inherited []gitignore.Pattern, depth int) error {
	if err := ctx.Err(); err != nil {
		return newError(KindCanceled, l.op, relDir, err)
	}
	if depth > maxWalkDepth {
		return newError(KindResourceLimit, l.op, relDir, fmt.Errorf("deeper than %d directories", maxWalkDepth))
	}
	absolute := filepath.Join(l.repo.dir, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil
	}
	patterns := append(slices.Clip(inherited), l.repo.directoryPatterns(relDir)...)
	matcher := gitignore.NewMatcher(patterns)
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		name := path.Join(relDir, entry.Name())
		if matcher.Match(strings.Split(name, "/"), entry.IsDir()) {
			continue
		}
		if l.tracked != nil {
			if _, known := l.tracked[name]; known {
				continue
			}
		}
		if !entry.IsDir() {
			if l.tracked != nil {
				return errFoundUntracked
			}
			if err = l.add(ctx, name); err != nil {
				return err
			}
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(absolute, entry.Name(), ".git")); statErr == nil {
			if l.tracked != nil {
				return errFoundUntracked
			}
			continue
		}
		if err = l.walk(ctx, name, patterns, depth+1); err != nil {
			return err
		}
	}
	return nil
}
