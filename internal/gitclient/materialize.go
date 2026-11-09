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
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	maxSymlinkTarget  = 4096
	maxAttributesFile = 1 << 20
	maxSymlinkDepth   = 40
	attributesName    = ".gitattributes"
)

// Limits bound tree operations. Zero fields use the defaults.
type Limits struct {
	MaxFiles      int64
	MaxFileBytes  int64
	MaxTotalBytes int64
}

func (l Limits) withDefaults() Limits {
	if l.MaxFiles <= 0 {
		l.MaxFiles = 200_000
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = 1 << 30
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = 4 << 30
	}
	return l
}

// Materialize writes commit's tree into dest, which must be absent or empty. It
// honours context, enforces Limits, confines writes to dest, rejects unsafe
// paths and escaping symlinks, skips submodules and applies .gitattributes eol
// conversion so files match a Git checkout. On failure dest is cleaned up.
func (r *Repo) Materialize(ctx context.Context, commit, dest string) error {
	return r.materialize(ctx, commit, dest, materializeMode{})
}

// MaterializeVerbatimLinks is Materialize without symlink target validation:
// links are written as committed, even ones leaving the tree, as a Git checkout
// would write them. Nothing is written through a link, but the caller must not
// follow links in dest without confining them itself.
func (r *Repo) MaterializeVerbatimLinks(ctx context.Context, commit, dest string) error {
	return r.materialize(ctx, commit, dest, materializeMode{verbatimLinks: true})
}

// materializeMode selects materialize's variations.
type materializeMode struct {
	// keepGit tolerates and preserves an existing .git directory in dest.
	keepGit bool
	// verbatimLinks skips validating symlink targets.
	verbatimLinks bool
}

type treeEntry struct {
	path   string
	hash   plumbing.Hash
	mode   filemode.FileMode
	target string
}

func (r *Repo) materialize(ctx context.Context, commit, dest string, mode materializeMode) (retErr error) {
	const op = "materialize"
	tree, err := r.commitTree(ctx, op, commit)
	if err != nil {
		return err
	}
	preexisting, err := prepareDestination(dest, mode.keepGit)
	if err != nil {
		return newError(KindFailed, op, commit, err)
	}
	defer func() {
		if retErr != nil {
			removeMaterialized(dest, preexisting, mode.keepGit)
		}
	}()
	entries, attributes, err := r.collect(ctx, op, tree)
	if err != nil {
		return err
	}
	if !mode.verbatimLinks {
		if err = validateSymlinks(entries); err != nil {
			return newError(KindInvalidSource, op, commit, err)
		}
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return newError(KindFailed, op, commit, err)
	}
	defer func() { _ = root.Close() }()
	var total int64
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return newError(KindCanceled, op, commit, err)
		}
		if dir := path.Dir(entry.path); dir != "." {
			if err = root.MkdirAll(dir, 0o755); err != nil {
				return newError(KindFailed, op, entry.path, err)
			}
		}
		if entry.mode == filemode.Symlink {
			if err = root.Symlink(entry.target, entry.path); err != nil {
				return newError(KindFailed, op, entry.path, err)
			}
			continue
		}
		written, writeErr := r.writeFile(ctx, op, root, entry, attributes, total)
		if writeErr != nil {
			return writeErr
		}
		total += written
	}
	return nil
}

func prepareDestination(dest string, keepGit bool) (bool, error) {
	entries, err := os.ReadDir(dest)
	if errors.Is(err, os.ErrNotExist) {
		return false, os.MkdirAll(dest, 0o750)
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !keepGit || entry.Name() != ".git" {
			return false, fmt.Errorf("destination %s is not empty", dest)
		}
	}
	return true, nil
}

func removeMaterialized(dest string, preexisting, keepGit bool) {
	if !preexisting {
		_ = os.RemoveAll(dest)
		return
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !keepGit || entry.Name() != ".git" {
			_ = os.RemoveAll(filepath.Join(dest, entry.Name()))
		}
	}
}

// collect validates every entry, reads symlink targets and gathers attribute
// rules before anything is written.
func (r *Repo) collect(ctx context.Context, op string, tree *object.Tree) ([]treeEntry, *attributeSet, error) {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	entries := []treeEntry{}
	attributes := newAttributeSet()
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, newError(KindCanceled, op, "", err)
		}
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return entries, attributes, nil
		}
		if err != nil {
			return nil, nil, classify(ctx, op, "", err)
		}
		if err = validateEntryPath(name); err != nil {
			return nil, nil, newError(KindInvalidSource, op, name, err)
		}
		if !isFileMode(entry.Mode) {
			continue
		}
		if int64(len(entries)) >= r.client.limits.MaxFiles {
			return nil, nil, newError(KindResourceLimit, op, name, fmt.Errorf("more than %d files", r.client.limits.MaxFiles))
		}
		item := treeEntry{path: name, hash: entry.Hash, mode: entry.Mode}
		if entry.Mode == filemode.Symlink {
			data, readErr := r.readBlob(ctx, op, name, entry.Hash, maxSymlinkTarget)
			if readErr != nil {
				return nil, nil, readErr
			}
			item.target = string(data)
		} else if path.Base(name) == attributesName {
			if data, readErr := r.readBlob(ctx, op, name, entry.Hash, maxAttributesFile); readErr == nil {
				attributes.add(path.Dir(name), data)
			} else if KindOf(readErr) != KindResourceLimit {
				return nil, nil, readErr
			}
		}
		entries = append(entries, item)
	}
}

// validateEntryPath refuses the paths Git itself refuses to check out.
func validateEntryPath(name string) error {
	if name == "" || !filepath.IsLocal(filepath.FromSlash(name)) {
		return fmt.Errorf("unsafe path %q", name)
	}
	for component := range strings.SplitSeq(name, "/") {
		if component == "" || component == "." || component == ".." || isGitDirName(component) {
			return fmt.Errorf("unsafe path %q", name)
		}
	}
	return nil
}

// isGitDirName also matches the spellings NTFS and HFS+ resolve to .git.
func isGitDirName(component string) bool {
	folded := strings.ToLower(strings.TrimRight(component, ". "))
	if before, _, found := strings.Cut(folded, ":"); found {
		folded = before
	}
	folded = strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200c && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		}
		return r
	}, folded)
	return folded == ".git" || folded == "git~1"
}

// validateSymlinks walks every link through the tree's other links, so a chain
// like a -> d/link/.. cannot climb out of the root.
func validateSymlinks(entries []treeEntry) error {
	links := map[string]string{}
	for _, entry := range entries {
		if entry.mode == filemode.Symlink {
			links[entry.path] = entry.target
		}
	}
	for _, entry := range entries {
		if entry.mode != filemode.Symlink {
			continue
		}
		if err := checkLink(links, entry.path, entry.target); err != nil {
			return err
		}
	}
	return nil
}

func checkLink(links map[string]string, name, target string) error {
	if target == "" || strings.ContainsRune(target, 0) || path.IsAbs(target) || filepath.IsAbs(target) || strings.Contains(target, "\\") {
		return fmt.Errorf("symlink %q has an unsafe target", name)
	}
	stack := []string{}
	if dir := path.Dir(name); dir != "." {
		stack = strings.Split(dir, "/")
	}
	pending := strings.Split(target, "/")
	for depth := 0; len(pending) > 0; {
		component := pending[0]
		pending = pending[1:]
		switch component {
		case "", ".":
			continue
		case "..":
			if len(stack) == 0 {
				return fmt.Errorf("symlink %q escapes the repository", name)
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if isGitDirName(component) {
			return fmt.Errorf("symlink %q targets the Git directory", name)
		}
		candidate := strings.Join(append(slices.Clone(stack), component), "/")
		next, isLink := links[candidate]
		if !isLink {
			stack = append(stack, component)
			continue
		}
		depth++
		if depth > maxSymlinkDepth {
			return fmt.Errorf("symlink %q has too many levels", name)
		}
		if next == "" || path.IsAbs(next) {
			return fmt.Errorf("symlink %q has an unsafe target", name)
		}
		pending = append(strings.Split(next, "/"), pending...)
	}
	return nil
}

func (r *Repo) writeFile(ctx context.Context, op string, root *os.Root, entry treeEntry, attributes *attributeSet, total int64) (int64, error) {
	limits := r.client.limits
	mode := os.FileMode(0o644)
	if entry.mode == filemode.Executable {
		mode = 0o755
	}
	blob, err := r.repo.BlobObject(entry.hash)
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return 0, newError(KindMissingBlob, op, entry.path, err)
		}
		return 0, classify(ctx, op, entry.path, err)
	}
	if blob.Size > limits.MaxFileBytes {
		return 0, newError(KindResourceLimit, op, entry.path, fmt.Errorf("%d bytes exceeds the %d byte limit", blob.Size, limits.MaxFileBytes))
	}
	// #nosec G304 -- os.Root confines this path to the destination.
	file, err := root.OpenFile(entry.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, newError(KindFailed, op, entry.path, err)
	}
	defer func() { _ = file.Close() }()
	action := attributes.eolAction(entry.path)
	remaining := limits.MaxTotalBytes - total
	if action != eolNone {
		data, readErr := r.readBlob(ctx, op, entry.path, entry.hash, limits.MaxFileBytes)
		if readErr != nil {
			return 0, readErr
		}
		data = convertEOL(data, action)
		if int64(len(data)) > remaining {
			return 0, totalLimit(op, entry.path, limits)
		}
		if _, err = file.Write(data); err != nil {
			return 0, newError(KindFailed, op, entry.path, err)
		}
		return int64(len(data)), nil
	}
	if blob.Size > remaining {
		return 0, totalLimit(op, entry.path, limits)
	}
	reader, err := blob.Reader()
	if err != nil {
		return 0, classify(ctx, op, entry.path, err)
	}
	defer func() { _ = reader.Close() }()
	written, err := io.Copy(file, contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return 0, classify(ctx, op, entry.path, err)
	}
	return written, nil
}

func totalLimit(op, name string, limits Limits) error {
	return newError(KindResourceLimit, op, name, fmt.Errorf("tree exceeds the %d byte limit", limits.MaxTotalBytes))
}

// eolAction is the checkout conversion chosen for a path.
type eolAction int

const (
	eolNone eolAction = iota
	eolCRLF
	eolAutoCRLF
)

// attributeSet evaluates .gitattributes rules across a tree. go-git's Matcher is
// not used because it lets lower-priority rules overwrite higher-priority ones
// when several attributes are requested.
type attributeSet struct {
	macros map[string][]gitattributes.Attribute
	rules  []attributeRule
	sorted bool
}

// infoAttributesDepth ranks $GIT_DIR/info/attributes above every .gitattributes.
const infoAttributesDepth = 1 << 30

type attributeRule struct {
	depth   int
	pattern gitattributes.Pattern
	attrs   []gitattributes.Attribute
}

func newAttributeSet() *attributeSet {
	set := &attributeSet{macros: map[string][]gitattributes.Attribute{}}
	if builtin, err := gitattributes.ParseAttributesLine("[attr]binary -diff -merge -text", nil, true); err == nil {
		set.macros[builtin.Name] = builtin.Attributes
	}
	return set
}

// add parses one .gitattributes file, skipping lines Git would also ignore.
func (a *attributeSet) add(dir string, data []byte) {
	var domain []string
	if dir != "." {
		domain = strings.Split(dir, "/")
	}
	a.addRules(data, domain, len(domain))
}

func (a *attributeSet) addRules(data []byte, domain []string, depth int) {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed, err := gitattributes.ParseAttributesLine(line, domain, len(domain) == 0)
		if err != nil {
			continue
		}
		if parsed.Pattern == nil {
			a.macros[parsed.Name] = parsed.Attributes
			continue
		}
		a.rules = append(a.rules, attributeRule{depth: depth, pattern: parsed.Pattern, attrs: parsed.Attributes})
	}
}

// evaluate applies rules from least to most specific, later lines winning.
func (a *attributeSet) evaluate(name string) map[string]gitattributes.Attribute {
	values := map[string]gitattributes.Attribute{}
	if len(a.rules) == 0 {
		return values
	}
	if !a.sorted {
		slices.SortStableFunc(a.rules, func(x, y attributeRule) int { return x.depth - y.depth })
		a.sorted = true
	}
	parts := strings.Split(name, "/")
	for _, rule := range a.rules {
		if !rule.pattern.Match(parts) {
			continue
		}
		for _, attr := range rule.attrs {
			if attr.IsSet() {
				for _, expanded := range a.macros[attr.Name()] {
					values[expanded.Name()] = expanded
				}
			}
			values[attr.Name()] = attr
		}
	}
	return values
}

// eolAction follows Git's convert.c for a working-tree write.
func (a *attributeSet) eolAction(name string) eolAction {
	return decideEOL(a.evaluate(name))
}

func decideEOL(values map[string]gitattributes.Attribute) eolAction {
	attr, ok := values["text"]
	if !ok || attr.IsUnspecified() {
		attr, ok = values["crlf"]
	}
	binary, auto := false, false
	if ok {
		switch {
		case attr.IsUnset():
			binary = true
		case attr.IsValueSet() && attr.Value() == "auto":
			auto = true
		}
	}
	if binary {
		return eolNone
	}
	eol, ok := values["eol"]
	if !ok || !eol.IsValueSet() || eol.Value() != "crlf" {
		return eolNone
	}
	if auto {
		return eolAutoCRLF
	}
	return eolCRLF
}

// convertEOL matches Git: every bare LF becomes CRLF, and text=auto leaves
// binary content and files already containing CR alone.
func convertEOL(data []byte, action eolAction) []byte {
	var nul, loneCR, crlf, loneLF, printable, nonPrintable int
	for i := 0; i < len(data); i++ {
		switch c := data[i]; {
		case c == '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				crlf++
				i++
			} else {
				loneCR++
			}
		case c == '\n':
			loneLF++
		case c == 127:
			nonPrintable++
		case c < 32:
			switch c {
			case '\b', '\t', 033, '\f':
				printable++
			case 0:
				nul++
				nonPrintable++
			default:
				nonPrintable++
			}
		default:
			printable++
		}
	}
	if len(data) > 0 && data[len(data)-1] == 032 {
		nonPrintable--
	}
	if loneLF == 0 {
		return data
	}
	if action == eolAutoCRLF && (loneCR > 0 || crlf > 0 || nul > 0 || printable>>7 < nonPrintable) {
		return data
	}
	var out bytes.Buffer
	out.Grow(len(data) + loneLF)
	for i, c := range data {
		if c == '\n' && (i == 0 || data[i-1] != '\r') {
			out.WriteByte('\r')
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
