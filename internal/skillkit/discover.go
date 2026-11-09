package skillkit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true, "__pycache__": true,
}

// DiscoverOptions controls skill discovery.
type DiscoverOptions struct {
	// Subpath narrows discovery to a slash-separated directory below the root.
	Subpath string
	// IncludeInternal keeps skills whose metadata marks them internal.
	IncludeInternal bool
	// FullDepth keeps searching below directories that already hold a SKILL.md.
	FullDepth bool
	// Agents supplies the skill directories to search. The zero table searches the built-in agents' directories.
	Agents AgentTable
}

// ParseError records a SKILL.md that could not be parsed.
type ParseError struct {
	Path string
	Err  error
}

func (e ParseError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e ParseError) Unwrap() error { return e.Err }

// DuplicateError records two skills with the same name at different paths.
type DuplicateError struct {
	Name         string
	First, Other string
}

func (e DuplicateError) Error() string {
	return fmt.Sprintf("duplicate skill name %q at %s and %s", e.Name, e.First, e.Other)
}

// DiscoverError reports problems found while discovering. Discover still returns
// the skills it could read, keeping the first of any duplicates, so a caller may
// continue if it only needs skills that are unaffected.
type DiscoverError struct {
	Parse      []ParseError
	Duplicates []DuplicateError
}

func (e *DiscoverError) Error() string {
	var parts []string
	for _, p := range e.Parse {
		parts = append(parts, "cannot parse "+p.Error())
	}
	for _, d := range e.Duplicates {
		parts = append(parts, d.Error())
	}
	return strings.Join(parts, "; ")
}

// Unwrap exposes every contained error to errors.Is and errors.As.
func (e *DiscoverError) Unwrap() []error {
	out := make([]error, 0, len(e.Parse)+len(e.Duplicates))
	for _, p := range e.Parse {
		out = append(out, p)
	}
	for _, d := range e.Duplicates {
		out = append(out, d)
	}
	return out
}

// PathTraversalError is returned when a subpath leaves the discovery root.
type PathTraversalError struct{ Subpath string }

func (e *PathTraversalError) Error() string {
	return "invalid subpath: " + e.Subpath + " resolves outside the repository directory"
}

// Discover finds skills below root. Reads go through os.Root, so symlinks that
// leave root are never followed. The search order is deterministic. Parse errors
// and duplicate names are collected in a *DiscoverError returned together with
// the skills that were readable.
func Discover(root string, opts DiscoverOptions) ([]Skill, error) {
	searchPath := "."
	if opts.Subpath != "" {
		if !filepath.IsLocal(opts.Subpath) || strings.Contains(opts.Subpath, "\\") {
			return nil, &PathTraversalError{Subpath: opts.Subpath}
		}
		searchPath = path.Clean(filepath.ToSlash(opts.Subpath))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	fsys := r.FS()

	table := opts.Agents
	if table.agents == nil {
		table, _ = NewAgentTable("", nil)
	}
	groupings := pluginGroupings(fsys, searchPath)

	var (
		skills []Skill
		derr   DiscoverError
		seen   = map[string]string{}
		done   = map[string]bool{}
	)
	add := func(skillDir string) {
		if done[skillDir] {
			return
		}
		done[skillDir] = true
		data, err := fs.ReadFile(fsys, path.Join(skillDir, "SKILL.md"))
		var s Skill
		if err == nil {
			s, err = ParseSkill(data)
		}
		if err != nil {
			derr.Parse = append(derr.Parse, ParseError{Path: path.Join(skillDir, "SKILL.md"), Err: err})
			return
		}
		s.Path = skillDir
		s.Dir = filepath.Join(abs, filepath.FromSlash(skillDir))
		s.PluginName = groupings[path.Clean(skillDir)]
		if first, dup := seen[s.Name]; dup {
			derr.Duplicates = append(derr.Duplicates, DuplicateError{Name: s.Name, First: first, Other: skillDir})
			return
		}
		seen[s.Name] = skillDir
		if s.Metadata.Internal && !opts.IncludeInternal {
			return
		}
		skills = append(skills, s)
	}

	if hasSkillMD(fsys, searchPath) {
		add(searchPath)
		if !opts.FullDepth {
			return skills, derr.orNil()
		}
	}
	dirs := searchDirs(searchPath, table)
	dirs = append(dirs, pluginSkillPaths(fsys, searchPath)...)
	for _, dir := range dirs {
		entries, err := fs.ReadDir(fsys, dir) // sorted by name
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if skillDir := path.Join(dir, entry.Name()); hasSkillMD(fsys, skillDir) {
				add(skillDir)
			}
		}
	}
	if len(skills) == 0 || opts.FullDepth {
		for _, dir := range findSkillDirs(fsys, searchPath, 0, 5) {
			add(dir)
		}
	}
	return skills, derr.orNil()
}

func (e *DiscoverError) orNil() error {
	if len(e.Parse) == 0 && len(e.Duplicates) == 0 {
		return nil
	}
	return e
}

// searchDirs lists priority directories: the search path, conventional
// locations, then every agent skills directory in sorted order.
func searchDirs(searchPath string, table AgentTable) []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	add(searchPath)
	for _, d := range []string{"skills", "skills/.curated", "skills/.experimental", "skills/.system", ".github/skills"} {
		add(path.Join(searchPath, d))
	}
	for _, d := range table.SkillsDirs() {
		add(path.Join(searchPath, d))
	}
	return dirs
}

func hasSkillMD(fsys fs.FS, dir string) bool {
	info, err := fs.Stat(fsys, path.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func findSkillDirs(fsys fs.FS, dir string, depth, maxDepth int) []string {
	if depth > maxDepth {
		return nil
	}
	var result []string
	if hasSkillMD(fsys, dir) {
		result = append(result, dir)
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() || skipDirs[entry.Name()] {
			continue
		}
		result = append(result, findSkillDirs(fsys, path.Join(dir, entry.Name()), depth+1, maxDepth)...)
	}
	return result
}

// Resolve decides which discovery problems matter for the requested skill names.
// Skills that cannot be parsed are reported as warnings: whether one of them was
// wanted shows up as a missing skill. A requested name defined twice is acceptable
// when both definitions have identical content (the first is used, with a warning)
// and an error when the content differs. Duplicates of names nobody asked for are
// ignored. root is the directory Discover ran in.
func (e *DiscoverError) Resolve(root string, names []string) ([]Warning, error) {
	var warns []Warning
	for _, p := range e.Parse {
		warns = append(warns, Warning{Code: WarnParseError, Path: p.Path, Message: "skipped " + p.Error()})
	}
	var errs []error
	for _, d := range e.Duplicates {
		if !requestedName(d.Name, names) {
			continue
		}
		first, ferr := FolderHash(filepath.Join(root, filepath.FromSlash(d.First)))
		other, oerr := FolderHash(filepath.Join(root, filepath.FromSlash(d.Other)))
		switch {
		case ferr != nil || oerr != nil:
			errs = append(errs, fmt.Errorf("cannot compare duplicate skill %q at %s and %s: %w", d.Name, d.First, d.Other, errors.Join(ferr, oerr)))
		case first != other:
			errs = append(errs, fmt.Errorf("skill %q is defined at %s and %s with different content", d.Name, d.First, d.Other))
		default:
			warns = append(warns, Warning{Code: WarnDuplicateSkill, Path: d.Other, Message: fmt.Sprintf("skill %q is defined at %s and %s with identical content; using %s", d.Name, d.First, d.Other, d.First)})
		}
	}
	return warns, errors.Join(errs...)
}

func requestedName(name string, names []string) bool {
	for _, want := range names {
		if strings.EqualFold(name, want) || SanitizeName(name) == SanitizeName(want) {
			return true
		}
	}
	return false
}

// WarnDuplicateSkill marks a requested skill name defined twice with identical content.
const WarnDuplicateSkill = "duplicate-skill"
