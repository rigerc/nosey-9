package gitclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const maxSubmoduleDepth = 8

// cleanAction is Git's add-time line-ending conversion for a path.
type cleanAction int

const (
	cleanNone cleanAction = iota
	// cleanText normalizes every CRLF to LF.
	cleanText
	// cleanAuto is cleanText for content that is text and not already CRLF in the index.
	cleanAuto
)

// cleanActionFor follows convert.c's convert_attrs. autocrlf is core.autocrlf and
// applies only when no attribute decides the path.
func (a *attributeSet) cleanActionFor(name, autocrlf string) cleanAction {
	values := a.evaluate(name)
	attr, ok := values["text"]
	if !ok || attr.IsUnspecified() {
		attr, ok = values["crlf"]
	}
	action, defined := cleanNone, false
	if ok && !attr.IsUnspecified() {
		switch {
		case attr.IsUnset():
			return cleanNone
		case attr.IsValueSet() && attr.Value() == "auto":
			action, defined = cleanAuto, true
		default:
			action, defined = cleanText, true
		}
	}
	if eol, hasEOL := values["eol"]; hasEOL && eol.IsValueSet() && (eol.Value() == "lf" || eol.Value() == "crlf") && !defined {
		return cleanText
	}
	if defined {
		return action
	}
	if autocrlf == "true" || autocrlf == "input" {
		return cleanAuto
	}
	return cleanNone
}

type contentStats struct {
	loneCR, crlf, loneLF, nul, printable, nonPrintable int
}

func gatherStats(data []byte) contentStats {
	var stats contentStats
	for i := 0; i < len(data); i++ {
		switch c := data[i]; {
		case c == '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				stats.crlf++
				i++
			} else {
				stats.loneCR++
			}
		case c == '\n':
			stats.loneLF++
		case c == 127:
			stats.nonPrintable++
		case c < 32:
			switch c {
			case '\b', '\t', 033, '\f':
				stats.printable++
			case 0:
				stats.nul++
				stats.nonPrintable++
			default:
				stats.nonPrintable++
			}
		default:
			stats.printable++
		}
	}
	if len(data) > 0 && data[len(data)-1] == 032 {
		stats.nonPrintable--
	}
	return stats
}

func (s contentStats) binary() bool {
	return s.loneCR > 0 || s.nul > 0 || s.printable>>7 < s.nonPrintable
}

// normalizeLF applies action. indexHasCR reports whether the path's current index
// blob holds a CR, which Git's text=auto treats as a reason not to convert.
func normalizeLF(data []byte, action cleanAction, indexHasCR func() bool) []byte {
	if action == cleanNone || !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	if action == cleanAuto && (gatherStats(data).binary() || indexHasCR()) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

// worktreeAttributes reads the .gitattributes files that apply to paths from the
// working tree, plus $GIT_DIR/info/attributes. core.attributesFile is not read.
func (r *Repo) worktreeAttributes(paths git.Status) *attributeSet {
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	return r.attributesFor(names)
}

func (r *Repo) attributesFor(names []string) *attributeSet {
	attributes := newAttributeSet()
	seen := map[string]struct{}{}
	for _, name := range names {
		for dir := path.Dir(name); ; dir = path.Dir(dir) {
			if _, done := seen[dir]; done {
				break
			}
			seen[dir] = struct{}{}
			if data, err := readBoundedFile(filepath.Join(r.dir, filepath.FromSlash(dir), attributesName)); err == nil {
				attributes.add(dir, data)
			}
			if dir == "." {
				break
			}
		}
	}
	if data := r.readGitDirFile("info/attributes"); data != nil {
		attributes.addRules(data, nil, infoAttributesDepth)
	}
	return attributes
}

func (r *Repo) indexEntry(name string) (plumbing.Hash, bool) {
	idx, err := r.repo.Storer.Index()
	if err != nil {
		return plumbing.ZeroHash, false
	}
	entry, err := idx.Entry(name)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	return entry.Hash, true
}

func (r *Repo) blobHasCR(hash plumbing.Hash) bool {
	if hash.IsZero() {
		return false
	}
	data, err := r.readBlob(context.Background(), "read blob", "", hash, r.client.limits.MaxFileBytes)
	return err == nil && bytes.IndexByte(data, '\r') >= 0
}

// stageNormalized replaces the blob go-git just staged for name when Git would
// have normalized its line endings. The working file is left as it is.
func (r *Repo) stageNormalized(ctx context.Context, op, name string, previous plumbing.Hash, attributes *attributeSet) error {
	action := attributes.cleanActionFor(name, "")
	if action == cleanNone {
		return nil
	}
	file := filepath.Join(r.dir, filepath.FromSlash(name))
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if info.Size() > r.client.limits.MaxFileBytes {
		return newError(KindResourceLimit, op, name, fmt.Errorf("%d bytes exceeds the %d byte limit", info.Size(), r.client.limits.MaxFileBytes))
	}
	data, err := os.ReadFile(file) // #nosec G304 -- a path inside this repository's working tree.
	if err != nil {
		return classify(ctx, op, name, err)
	}
	cleaned := normalizeLF(data, action, func() bool { return r.blobHasCR(previous) })
	if bytes.Equal(cleaned, data) {
		return nil
	}
	blob := r.repo.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	blob.SetSize(int64(len(cleaned)))
	writer, err := blob.Writer()
	if err != nil {
		return classify(ctx, op, name, err)
	}
	if _, err = writer.Write(cleaned); err != nil {
		_ = writer.Close()
		return classify(ctx, op, name, err)
	}
	if err = writer.Close(); err != nil {
		return classify(ctx, op, name, err)
	}
	hash, err := r.repo.Storer.SetEncodedObject(blob)
	if err != nil {
		return classify(ctx, op, name, err)
	}
	idx, err := r.repo.Storer.Index()
	if err != nil {
		return classify(ctx, op, name, err)
	}
	entry, err := idx.Entry(name)
	if err != nil {
		return classify(ctx, op, name, err)
	}
	entry.Hash = hash
	return classify(ctx, op, name, r.repo.Storer.SetIndex(idx))
}

// Dirty reports whether git status --porcelain would print anything for a user
// repository: staged changes, modified or deleted tracked files, or untracked
// files that no ignore rule matches. Files whose only difference is a stat change
// or CRLF line endings that Git's clean filter removes are not dirty.
func (r *Repo) Dirty(ctx context.Context) (bool, error) {
	return r.dirty(ctx, 0)
}

func (r *Repo) dirty(ctx context.Context, depth int) (bool, error) {
	const op = "read status"
	idx, err := r.repo.Storer.Index()
	if err != nil {
		return false, classify(ctx, op, "", err)
	}
	if changed, stagedErr := r.stagedChanges(ctx, op, idx); stagedErr != nil || changed {
		return changed, stagedErr
	}
	check := worktreeCheck{repo: r, op: op, depth: depth, indexTime: r.indexModTime()}
	check.autocrlf = r.configValue("core", "autocrlf")
	check.trustMode = r.configValue("core", "filemode") != "false"
	names := make([]string, 0, len(idx.Entries))
	tracked := make(map[string]struct{}, len(idx.Entries))
	for _, entry := range idx.Entries {
		names = append(names, entry.Name)
		tracked[entry.Name] = struct{}{}
	}
	check.attributes = r.attributesFor(names)
	for _, entry := range idx.Entries {
		if err = ctx.Err(); err != nil {
			return false, newError(KindCanceled, op, entry.Name, err)
		}
		if entry.SkipWorktree {
			continue
		}
		if changed, entryErr := check.modified(ctx, entry); entryErr != nil || changed {
			return changed, entryErr
		}
	}
	if strings.EqualFold(r.configValue("status", "showuntrackedfiles"), "no") {
		return false, nil
	}
	return r.hasUntracked(ctx, op, tracked)
}

// stagedChanges compares the index with HEAD's tree.
func (r *Repo) stagedChanges(ctx context.Context, op string, idx *index.Index) (bool, error) {
	head := map[string]object.TreeEntry{}
	headCommit, err := r.Head()
	if err != nil {
		return false, err
	}
	if headCommit != "" {
		tree, treeErr := r.commitTree(ctx, op, headCommit)
		if treeErr != nil {
			return false, treeErr
		}
		walker := object.NewTreeWalker(tree, true, nil)
		defer walker.Close()
		for {
			name, entry, nextErr := walker.Next()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return false, classify(ctx, op, headCommit, nextErr)
			}
			if entry.Mode != filemode.Dir {
				head[name] = entry
			}
		}
	}
	for _, entry := range idx.Entries {
		committed, known := head[entry.Name]
		if entry.Stage != 0 || !known || committed.Hash != entry.Hash || !sameMode(committed.Mode, entry.Mode) {
			return true, nil
		}
		delete(head, entry.Name)
	}
	return len(head) > 0, nil
}

func sameMode(a, b filemode.FileMode) bool {
	regular := func(mode filemode.FileMode) filemode.FileMode {
		if mode == filemode.Deprecated {
			return filemode.Regular
		}
		return mode
	}
	return regular(a) == regular(b)
}

type worktreeCheck struct {
	repo       *Repo
	op         string
	depth      int
	indexTime  int64
	autocrlf   string
	trustMode  bool
	attributes *attributeSet
}

func (c *worktreeCheck) modified(ctx context.Context, entry *index.Entry) (bool, error) {
	file := filepath.Join(c.repo.dir, filepath.FromSlash(entry.Name))
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return entry.Mode != filemode.Submodule, nil
	}
	if err != nil {
		return false, classify(ctx, c.op, entry.Name, err)
	}
	switch entry.Mode {
	case filemode.Submodule:
		return c.submoduleModified(ctx, entry, file, info)
	case filemode.Symlink:
		if info.Mode()&os.ModeSymlink == 0 {
			return true, nil
		}
		link, linkErr := os.Readlink(file)
		if linkErr != nil {
			return false, classify(ctx, c.op, entry.Name, linkErr)
		}
		return plumbing.ComputeHash(plumbing.BlobObject, []byte(link)) != entry.Hash, nil
	}
	if !info.Mode().IsRegular() {
		return true, nil
	}
	if c.trustMode && (info.Mode()&0o111 != 0) != (entry.Mode == filemode.Executable) {
		return true, nil
	}
	if info.Size() == int64(entry.Size) && info.ModTime().Equal(entry.ModifiedAt) && info.ModTime().UnixNano() < c.indexTime {
		return false, nil
	}
	if info.Size() > c.repo.client.limits.MaxFileBytes {
		return false, newError(KindResourceLimit, c.op, entry.Name, fmt.Errorf("%d bytes exceeds the %d byte limit", info.Size(), c.repo.client.limits.MaxFileBytes))
	}
	data, err := os.ReadFile(file) // #nosec G304 -- a tracked path inside the user's own repository.
	if err != nil {
		return false, classify(ctx, c.op, entry.Name, err)
	}
	action := c.attributes.cleanActionFor(entry.Name, c.autocrlf)
	cleaned := normalizeLF(data, action, func() bool { return c.repo.blobHasCR(entry.Hash) })
	return plumbing.ComputeHash(plumbing.BlobObject, cleaned) != entry.Hash, nil
}

// submoduleModified is true when an initialized submodule's HEAD differs from the
// recorded commit or its own work tree is dirty. An unpopulated one is clean.
func (c *worktreeCheck) submoduleModified(ctx context.Context, entry *index.Entry, file string, info os.FileInfo) (bool, error) {
	if !info.IsDir() {
		return true, nil
	}
	if _, err := os.Lstat(filepath.Join(file, git.GitDirName)); err != nil {
		return false, nil
	}
	if c.depth >= maxSubmoduleDepth {
		return false, nil
	}
	nested, err := c.repo.client.OpenUser(file)
	if err != nil {
		return false, nil
	}
	head, err := nested.Head()
	if err != nil {
		return false, nil
	}
	if head != entry.Hash.String() {
		return true, nil
	}
	return nested.dirty(ctx, c.depth+1)
}

func (r *Repo) readGitDirFile(name string) []byte {
	gitDir, ok := r.gitDirFS()
	if !ok {
		return nil
	}
	file, err := gitDir.Open(name)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxAttributesFile+1))
	if err != nil || len(data) > maxAttributesFile {
		return nil
	}
	return data
}

func (r *Repo) indexModTime() int64 {
	gitDir, ok := r.gitDirFS()
	if !ok {
		return 0
	}
	info, err := gitDir.Stat("index")
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// configValue reads section.key from repository config, then the global files.
func (r *Repo) configValue(section, key string) string {
	if local, err := r.repo.Storer.Config(); err == nil && local.Raw.HasSection(section) {
		if value := local.Raw.Section(section).Options.Get(key); value != "" {
			return value
		}
	}
	for _, file := range globalConfigPaths() {
		if cfg := readConfigFile(file); cfg != nil && cfg.Raw.HasSection(section) {
			if value := cfg.Raw.Section(section).Options.Get(key); value != "" {
				return value
			}
		}
	}
	return ""
}

var errFoundUntracked = errors.New("untracked file found")

// hasUntracked stops at the first path git status would list as untracked.
func (r *Repo) hasUntracked(ctx context.Context, op string, tracked map[string]struct{}) (bool, error) {
	listing := worktreeListing{repo: r, op: op, found: map[string]struct{}{}, limit: 1 << 40, tracked: tracked}
	err := listing.walk(ctx, "", r.excludePatterns(), 0)
	if errors.Is(err, errFoundUntracked) {
		return true, nil
	}
	return false, err
}
