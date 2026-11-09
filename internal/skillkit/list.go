package skillkit

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// InstalledSkill is a skill found in the project's skill directories.
type InstalledSkill struct {
	// Name and Description come from SKILL.md.
	Name        string
	Description string
	// Dir is the directory name under the skills directory.
	Dir string
	// Path is the workspace-relative directory the skill was read from, preferring the canonical one.
	Path string
	// Agents lists agent IDs that read the skill, sorted.
	Agents []string
	// Locations lists every skills directory that holds the skill, canonical directory first.
	// It is the filesystem truth: a location exists even when no agent is attributed to it,
	// such as .agents/skills when no universal agent is detected on this host.
	Locations []Location
}

// Location is one directory entry that holds an installed skill.
type Location struct {
	// Dir is the slash-separated skills directory, such as .claude/skills.
	Dir string
	// Path is the slash-separated, workspace-relative skill directory.
	Path string
	// Symlink is set when Path is a symlink to another copy rather than a copy itself.
	Symlink bool
	// Agents lists the agents attributed to Dir, as in InstalledSkill.Agents.
	Agents []string
}

// Inventory is the installed state of a workspace.
type Inventory struct {
	Skills   []InstalledSkill
	Warnings []Warning
}

// Inventory warning codes.
const (
	WarnParseError  = "parse-error"
	WarnUnreadable  = "unreadable"
	WarnOutsideRoot = "outside-workspace"
)

// ListInstalled scans the skill directories of every agent in the table.
// Agent-specific directories attribute skills to their agents directly. The
// shared .agents/skills directory attributes to the agents that read it and
// are detected on this host, plus custom agents using it, as skills@1.7.6 does.
// Directories whose ancestors leave the workspace are skipped with a warning.
func ListInstalled(target Target) (Inventory, error) {
	if err := target.validate(); err != nil {
		return Inventory{}, err
	}
	resolved, err := filepath.EvalSymlinks(target.Workspace)
	if err != nil {
		return Inventory{}, err
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return Inventory{}, err
	}
	defer func() { _ = root.Close() }()
	d := &destRoot{root: root, real: resolved}

	detected := map[string]bool{}
	for _, id := range target.Agents.Detected(resolved) {
		detected[id] = true
	}
	// Attribution by directory.
	byDir := map[string][]string{}
	for _, a := range target.Agents.All() {
		byDir[a.SkillsDir] = append(byDir[a.SkillsDir], a.ID)
	}
	attributed := func(dir string) []string {
		ids := byDir[dir]
		if dir == UniversalSkillsDir {
			var out []string
			for _, id := range ids {
				if a, _ := target.Agents.Get(id); a.Custom || detected[id] {
					out = append(out, id)
				}
			}
			return out
		}
		// Agents that share a non-universal directory: detected ones, else the first by ID.
		var det []string
		for _, id := range ids {
			if a, _ := target.Agents.Get(id); a.Custom || detected[id] {
				det = append(det, id)
			}
		}
		if len(det) == 0 && len(ids) > 0 {
			det = ids[:1]
		}
		return det
	}

	var inv Inventory
	byName := map[string]*InstalledSkill{}
	var order []string
	dirs := target.Agents.SkillsDirs()
	// Canonical directory first so it provides Path.
	dirs = slices.DeleteFunc(dirs, func(s string) bool { return s == UniversalSkillsDir })
	dirs = append([]string{UniversalSkillsDir}, dirs...)
	for _, dir := range dirs {
		if err = d.checkParent(path.Join(dir, "x")); err != nil {
			var pe *PolicyError
			if errors.As(err, &pe) {
				inv.Warnings = append(inv.Warnings, Warning{Code: WarnOutsideRoot, Path: dir, Message: pe.Error()})
				continue
			}
			return inv, err
		}
		entries, rerr := fs.ReadDir(root.FS(), dir)
		if rerr != nil {
			if !errors.Is(rerr, fs.ErrNotExist) {
				inv.Warnings = append(inv.Warnings, Warning{Code: WarnUnreadable, Path: dir, Message: "cannot read " + dir + ": " + rerr.Error()})
			}
			continue
		}
		ids := attributed(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".nosey-") {
				continue
			}
			rel := path.Join(dir, e.Name())
			info, serr := root.Stat(filepath.FromSlash(rel))
			if serr != nil || !info.IsDir() {
				continue
			}
			data, rerr := fs.ReadFile(root.FS(), path.Join(rel, "SKILL.md"))
			if errors.Is(rerr, fs.ErrNotExist) {
				continue
			}
			var s Skill
			if rerr == nil {
				s, rerr = ParseSkill(data)
			}
			if rerr != nil {
				inv.Warnings = append(inv.Warnings, Warning{Code: WarnParseError, Path: rel, Message: "cannot parse " + path.Join(rel, "SKILL.md") + ": " + rerr.Error()})
				continue
			}
			item := byName[s.Name]
			if item == nil {
				item = &InstalledSkill{Name: s.Name, Description: s.Description, Dir: e.Name(), Path: rel}
				byName[s.Name] = item
				order = append(order, s.Name)
			}
			linfo, lerr := root.Lstat(filepath.FromSlash(rel))
			item.Locations = append(item.Locations, Location{Dir: dir, Path: rel, Symlink: lerr == nil && linfo.Mode()&fs.ModeSymlink != 0, Agents: slices.Clone(ids)})
			for _, id := range ids {
				if !slices.Contains(item.Agents, id) {
					item.Agents = append(item.Agents, id)
				}
			}
		}
	}
	slices.Sort(order)
	for _, name := range order {
		item := byName[name]
		slices.Sort(item.Agents)
		inv.Skills = append(inv.Skills, *item)
	}
	return inv, nil
}
