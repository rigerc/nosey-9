package skillkit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// InvalidNameError reports a skill name that cannot safely name a directory.
type InvalidNameError struct {
	Name   string
	Reason string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid skill name %q: %s", e.Name, e.Reason)
}

// ValidateName checks a skill name read from the lock before it is used to
// build paths. Names with separators, dot segments or control characters are
// refused; the directory used is SanitizeName(name).
func ValidateName(name string) error {
	switch {
	case name == "" || len(name) > 255:
		return &InvalidNameError{name, "must be 1 to 255 bytes"}
	case name == "." || name == "..":
		return &InvalidNameError{name, "is a dot segment"}
	case strings.ContainsAny(name, "/\\"):
		return &InvalidNameError{name, "contains a path separator"}
	case strings.ContainsFunc(name, func(r rune) bool { return r < 32 || r == 127 }):
		return &InvalidNameError{name, "contains control characters"}
	}
	return nil
}

// Removal is one path considered for removal.
type Removal struct {
	// Path is workspace-relative.
	Path string
	// Removed is set when the path existed and was removed.
	Removed bool
	// Symlink is set when the path was a symlink that was unlinked without being followed.
	Symlink bool
	// Absent is set when nothing existed at Path.
	Absent bool
	// Skipped is set when removal was not attempted; Err says why.
	Skipped bool
	Err     error
}

// UninstallResult lists what Uninstall did, agent directories first and the canonical directory last.
type UninstallResult struct {
	Name     string
	Removals []Removal
}

// Removed lists paths that were removed.
func (r UninstallResult) Removed() []string {
	var out []string
	for _, rm := range r.Removals {
		if rm.Removed {
			out = append(out, rm.Path)
		}
	}
	return out
}

// Uninstall removes a skill from the canonical directory and every agent's
// directory in the table. Every path is checked to stay inside the workspace,
// symlinks are unlinked and never followed, agent directories go first and the
// canonical directory last (and is kept if an agent directory could not be
// removed, to avoid leaving dangling links). All errors are returned joined and
// are also recorded per path in the result.
func Uninstall(ctx context.Context, name string, target Target) (UninstallResult, error) {
	res := UninstallResult{Name: name}
	if err := ValidateName(name); err != nil {
		return res, err
	}
	if err := target.validate(); err != nil {
		return res, err
	}
	dest, err := openDest(target.Workspace)
	if err != nil {
		return res, err
	}
	defer dest.Close()

	dirName := SanitizeName(name)
	canonical := canonicalRel(dirName)
	seen := map[string]bool{canonical: true}
	var rels []string
	for _, dir := range target.Agents.SkillsDirs() {
		if rel := path.Join(dir, dirName); !seen[rel] {
			seen[rel] = true
			rels = append(rels, rel)
		}
	}
	var errs []error
	agentFailed := false
	for _, rel := range rels {
		if cerr := ctx.Err(); cerr != nil {
			res.Removals = append(res.Removals, Removal{Path: rel, Skipped: true, Err: cerr})
			errs = append(errs, cerr)
			agentFailed = true
			continue
		}
		rm := dest.remove(rel)
		res.Removals = append(res.Removals, rm)
		if rm.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, rm.Err))
			agentFailed = true
		}
	}
	if agentFailed {
		res.Removals = append(res.Removals, Removal{Path: canonical, Skipped: true, Err: errors.New("kept because an agent directory could not be removed")})
		return res, errors.Join(errs...)
	}
	if cerr := ctx.Err(); cerr != nil {
		res.Removals = append(res.Removals, Removal{Path: canonical, Skipped: true, Err: cerr})
		return res, errors.Join(append(errs, cerr)...)
	}
	rm := dest.remove(canonical)
	res.Removals = append(res.Removals, rm)
	if rm.Err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", canonical, rm.Err))
	}
	return res, errors.Join(errs...)
}

// remove deletes rel without following a symlink at the final component.
func (d *destRoot) remove(rel string) Removal {
	rm := Removal{Path: rel}
	if err := d.checkParent(rel); err != nil {
		rm.Err = err
		return rm
	}
	native := filepath.FromSlash(rel)
	info, err := d.root.Lstat(native)
	if errors.Is(err, fs.ErrNotExist) {
		rm.Absent = true
		return rm
	}
	if err != nil {
		rm.Err = wrap(rel, err)
		return rm
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		rm.Symlink = true
		err = d.root.Remove(native)
	} else {
		err = d.root.RemoveAll(native)
	}
	if err != nil {
		rm.Err = wrap(rel, err)
		return rm
	}
	rm.Removed = true
	return rm
}
