package skillkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// Warning is a non-fatal problem found while installing a skill.
type Warning struct {
	// Code is symlink-broken, symlink-escapes, symlink-cycle or special-file.
	Code string
	// Path is the slash-separated path inside the skill directory.
	Path    string
	Message string
}

func (w Warning) String() string { return w.Message }

// Codes of Warning.
const (
	WarnSymlinkBroken  = "symlink-broken"
	WarnSymlinkEscapes = "symlink-escapes"
	WarnSymlinkCycle   = "symlink-cycle"
	WarnSpecialFile    = "special-file"
)

// excluded ports isExcluded from skills@1.7.6.
func excluded(name string, isDir bool) bool {
	if name == "metadata.json" {
		return true
	}
	return isDir && (name == ".git" || name == "__pycache__" || name == "__pypackages__")
}

// copier copies one skill directory into the workspace with the rules of
// skills@1.7.6 copyDirectory: broken symlinks and symlinks that resolve outside
// the skill are skipped with a warning; links to files and directories inside
// the skill are followed; directory cycles are cut.
type copier struct {
	ctx      context.Context
	dest     *destRoot
	root     string // real path of the skill directory
	warnings []Warning
}

func (c *copier) warn(code, rel, msg string) {
	c.warnings = append(c.warnings, Warning{Code: code, Path: rel, Message: msg})
}

// copySkill fills dstRel (workspace-relative, already unique) with src's contents.
func (d *destRoot) copySkill(ctx context.Context, src, dstRel string) ([]Warning, error) {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return nil, err
	}
	c := &copier{ctx: ctx, dest: d, root: real}
	err = c.copyDir(real, ".", dstRel, map[string]bool{})
	return c.warnings, err
}

func (c *copier) copyDir(realSrc, rel, dstRel string, ancestors map[string]bool) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if ancestors[realSrc] {
		c.warn(WarnSymlinkCycle, rel, fmt.Sprintf("skipped symlink that creates a directory cycle: %s", rel))
		return nil
	}
	next := make(map[string]bool, len(ancestors)+1)
	for k := range ancestors {
		next[k] = true
	}
	next[realSrc] = true
	if err := c.dest.root.MkdirAll(filepath.FromSlash(dstRel), 0o755); err != nil {
		return wrap(dstRel, err)
	}
	entries, err := os.ReadDir(realSrc) // sorted by name
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if excluded(name, e.IsDir()) {
			continue
		}
		srcPath := filepath.Join(realSrc, name)
		childRel := path.Join(rel, name)
		if rel == "." {
			childRel = name
		}
		dst := path.Join(dstRel, name)
		isDir := e.IsDir()
		target := srcPath
		if e.Type()&fs.ModeSymlink != 0 {
			resolved, rerr := filepath.EvalSymlinks(srcPath)
			if rerr != nil {
				c.warn(WarnSymlinkBroken, childRel, "skipped broken symlink: "+childRel)
				continue
			}
			if !within(c.root, resolved) {
				c.warn(WarnSymlinkEscapes, childRel, "skipped symlink that points outside the skill directory: "+childRel)
				continue
			}
			info, serr := os.Stat(resolved)
			if serr != nil {
				c.warn(WarnSymlinkBroken, childRel, "skipped broken symlink: "+childRel)
				continue
			}
			target, isDir = resolved, info.IsDir()
			if isDir && excluded(name, true) {
				continue
			}
		}
		if isDir {
			if err = c.copyDir(target, childRel, dst, next); err != nil {
				return err
			}
			continue
		}
		info, serr := os.Stat(target)
		if serr != nil {
			c.warn(WarnSymlinkBroken, childRel, "skipped unreadable file: "+childRel)
			continue
		}
		if !info.Mode().IsRegular() {
			c.warn(WarnSpecialFile, childRel, "skipped non-regular file: "+childRel)
			continue
		}
		if err = c.copyFile(target, dst, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func (c *copier) copyFile(src, dstRel string, perm fs.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := c.dest.root.OpenFile(filepath.FromSlash(dstRel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wrap(dstRel, err)
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = c.dest.root.Chmod(filepath.FromSlash(dstRel), perm); err != nil && !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return nil
}
