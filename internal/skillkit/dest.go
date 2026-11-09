package skillkit

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PolicyError reports a destination that would leave the workspace, such as a
// symlinked ancestor directory that resolves elsewhere. Nothing is written or
// removed through such a path.
type PolicyError struct {
	// Path is the workspace-relative path that was refused.
	Path   string
	Reason string
}

func (e *PolicyError) Error() string { return fmt.Sprintf("refusing %s: %s", e.Path, e.Reason) }

// destRoot confines every write and removal to a workspace. Mutations use
// os.Root, which the kernel-independent lookup below double checks with an
// explicit symlink walk so the failure is a typed *PolicyError.
type destRoot struct {
	root *os.Root
	real string // symlink-resolved workspace path
}

func openDest(workspace string) (*destRoot, error) {
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}
	if resolved, err = filepath.Abs(resolved); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	return &destRoot{root: r, real: resolved}, nil
}

func (d *destRoot) Close() { _ = d.root.Close() }

func within(root, target string) bool {
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

// checkParent refuses a path whose existing ancestors resolve outside the
// workspace or dangle. The final component is not followed.
func (d *destRoot) checkParent(rel string) error {
	if !filepath.IsLocal(rel) {
		return &PolicyError{Path: rel, Reason: "path is not local to the workspace"}
	}
	parent := filepath.Dir(filepath.FromSlash(rel))
	if parent == "." {
		return nil
	}
	// Find the deepest existing ancestor; everything beyond it is created by us.
	comps := strings.Split(parent, string(filepath.Separator))
	cur := d.real
	for i, c := range comps {
		next := filepath.Join(cur, c)
		info, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			resolved, rerr := filepath.EvalSymlinks(next)
			if rerr != nil {
				return &PolicyError{Path: rel, Reason: "ancestor " + path.Join(comps[:i+1]...) + " is a broken symlink"}
			}
			if !within(d.real, resolved) {
				return &PolicyError{Path: rel, Reason: "ancestor " + filepath.ToSlash(filepath.Join(comps[:i+1]...)) + " is a symlink that leaves the workspace"}
			}
			next = resolved
		} else if !info.IsDir() {
			return &PolicyError{Path: rel, Reason: "ancestor " + filepath.ToSlash(filepath.Join(comps[:i+1]...)) + " is not a directory"}
		}
		cur = next
	}
	return nil
}

// wrap converts os.Root's escape error into a *PolicyError.
func wrap(rel string, err error) error {
	if err != nil && strings.Contains(err.Error(), "escapes from parent") {
		return &PolicyError{Path: rel, Reason: "path escapes the workspace"}
	}
	return err
}

func tempName(prefix string) string { return prefix + rand.Text() }

// replace atomically installs a freshly built entry at rel. build receives a
// sibling temporary path (relative to the workspace) to fill with a directory,
// file or symlink. An existing entry at rel is swapped out and removed only
// after the new one is in place, and restored if the swap fails.
func (d *destRoot) replace(rel string, build func(tmp string) error) (err error) {
	if err = d.checkParent(rel); err != nil {
		return err
	}
	parent := path.Dir(rel)
	if err = d.root.MkdirAll(filepath.FromSlash(parent), 0o755); err != nil {
		return wrap(rel, err)
	}
	// Re-check now that the parent exists: a racing symlink would have been refused by os.Root anyway.
	if err = d.checkParent(rel); err != nil {
		return err
	}
	tmp := path.Join(parent, tempName(".nosey-tmp-"))
	defer func() { _ = d.root.RemoveAll(filepath.FromSlash(tmp)) }()
	if err = build(tmp); err != nil {
		return wrap(rel, err)
	}
	if _, lerr := d.root.Lstat(filepath.FromSlash(rel)); lerr != nil {
		if !errors.Is(lerr, fs.ErrNotExist) {
			return wrap(rel, lerr)
		}
		return wrap(rel, d.root.Rename(filepath.FromSlash(tmp), filepath.FromSlash(rel)))
	}
	backup := path.Join(parent, tempName(".nosey-old-"))
	if err = d.root.Rename(filepath.FromSlash(rel), filepath.FromSlash(backup)); err != nil {
		return wrap(rel, err)
	}
	if err = d.root.Rename(filepath.FromSlash(tmp), filepath.FromSlash(rel)); err != nil {
		_ = d.root.Rename(filepath.FromSlash(backup), filepath.FromSlash(rel))
		return wrap(rel, err)
	}
	_ = d.root.RemoveAll(filepath.FromSlash(backup))
	return nil
}

// realPath resolves rel (which may not exist) against the real workspace: the
// deepest existing ancestor is resolved and the missing tail is appended.
func (d *destRoot) realPath(rel string) string {
	cur := filepath.Join(d.real, filepath.FromSlash(rel))
	var tail []string
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved
		}
		next := filepath.Dir(cur)
		if next == cur {
			return filepath.Join(d.real, filepath.FromSlash(rel))
		}
		tail = append(tail, filepath.Base(cur))
		cur = next
	}
}
