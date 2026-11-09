package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// UpdateCheck is what a native update check established about installed project skills.
type UpdateCheck struct {
	// Names lists skills whose source content differs from the installed tree.
	Names []string
	// Failures holds the reason for each source or skill that could not be checked.
	Failures []string
	// Failed counts skills that could not be checked.
	Failed int
	// UpToDate counts skills confirmed identical to their source.
	UpToDate int
	// AllCurrent is true only when every installed skill was checked and found current.
	AllCurrent bool
	// Legacy counts lock entries without a skillPath, which cannot be compared with a source.
	Legacy int
	// NothingToCompare is true when no skill could be compared with its source.
	NothingToCompare bool
}

// Summary states what the check established, without claiming more than upstream compared.
func (c UpdateCheck) Summary() string {
	switch {
	case len(c.Names) > 0:
		return fmt.Sprintf("%s can be updated: %s", skillCount(len(c.Names)), strings.Join(c.Names, ", "))
	case c.AllCurrent:
		return "All installed skills are up to date."
	case c.UpToDate > 0:
		return skillCount(c.UpToDate) + " confirmed up to date."
	case c.NothingToCompare:
		return "No installed skills can be compared with their sources."
	}
	return "The update check reported no updates."
}

// Warning describes skills the check could not cover; it is empty for a complete check.
func (c UpdateCheck) Warning() string {
	parts := []string{}
	if detail := c.failureDetail(); detail != "" {
		parts = append(parts, "Some skills could not be checked: "+detail+".")
	}
	if c.Legacy > 0 {
		parts = append(parts, skillCount(c.Legacy)+" cannot be checked automatically; reinstall to enable update checks.")
	}
	return strings.Join(parts, " ")
}

func (c UpdateCheck) failureDetail() string {
	switch {
	case len(c.Failures) == 1:
		return c.Failures[0]
	case len(c.Failures) > 1:
		return fmt.Sprintf("%s (and %d more)", c.Failures[0], len(c.Failures)-1)
	case c.Failed > 0:
		return skillCount(c.Failed) + " failed"
	}
	return ""
}

func skillCount(count int) string {
	if count == 1 {
		return "1 skill"
	}
	return fmt.Sprintf("%d skills", count)
}

// updateGroup is every lock entry fetched from the same source and ref.
type updateGroup struct {
	source skillkit.Source
	label  string
	names  []string
}

// CheckUpdates compares each installed project skill with its source. Lock entries are grouped
// by source and ref and each group is fetched once; a skill differs when the FolderHash of its
// fetched copy differs from the FolderHash of the installed tree, so local edits count too.
// A fetch or read failure is reported in Failures, never as "no updates".
func (s Service) CheckUpdates(ctx context.Context) (UpdateCheck, error) {
	check := UpdateCheck{Names: []string{}}
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	if errors.Is(err, skillkit.ErrLockMissing) {
		check.NothingToCompare = true
		return check, nil
	}
	if err != nil {
		return UpdateCheck{}, err
	}
	table, err := s.agentTable()
	if err != nil {
		return UpdateCheck{}, err
	}
	listed, err := skillkit.ListInstalled(skillkit.Target{Workspace: s.Store.Workspace, Agents: table})
	if err != nil {
		return UpdateCheck{}, fmt.Errorf("reading installed skills: %w", err)
	}
	installed := map[string]string{}
	for _, item := range listed.Skills {
		installed[item.Name] = filepath.Join(s.Store.Workspace, filepath.FromSlash(item.Path))
	}
	groups := map[string]*updateGroup{}
	for _, name := range lock.Names() {
		entry := lock.Skills[name]
		if entry.SkillPath == "" {
			check.Legacy++
			continue
		}
		source := skillkit.Source{URI: entry.Source, Ref: entry.Ref}
		label := entry.Source
		if entry.SourceType == "local" {
			source = skillkit.Source{LocalPath: entry.Source}
		} else if entry.SourceURL != "" {
			source.URI = entry.SourceURL
		}
		key := strings.Join([]string{entry.SourceType, source.URI, source.LocalPath, source.Ref}, "\x00")
		if groups[key] == nil {
			groups[key] = &updateGroup{source: source, label: label}
			if entry.Ref != "" {
				groups[key].label += "@" + entry.Ref
			}
		}
		groups[key].names = append(groups[key].names, name)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if err = ctx.Err(); err != nil {
			return UpdateCheck{}, err
		}
		s.checkGroup(ctx, groups[key], lock, installed, &check)
	}
	if err = ctx.Err(); err != nil {
		return UpdateCheck{}, err
	}
	slices.Sort(check.Names)
	check.Names = slices.Compact(check.Names)
	slices.Sort(check.Failures)
	check.NothingToCompare = check.UpToDate == 0 && len(check.Names) == 0 && check.Failed == 0
	check.AllCurrent = check.UpToDate > 0 && len(check.Names) == 0 && check.Failed == 0 && check.Legacy == 0
	return check, nil
}

func (s Service) checkGroup(ctx context.Context, g *updateGroup, lock skillkit.ProjectLock, installed map[string]string, check *UpdateCheck) {
	fail := func(count int, reason string) {
		check.Failed += count
		check.Failures = append(check.Failures, storage.Safe(reason))
	}
	checkout, err := s.fetcher().Fetch(ctx, g.source)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		fail(len(g.names), fmt.Sprintf("%s: %v", g.label, err))
		return
	}
	defer func() { _ = checkout.Close() }()
	for _, name := range g.names {
		same, err := s.sameAsSource(ctx, checkout.Dir, lock.Skills[name], installed[name])
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			fail(1, fmt.Sprintf("%s (%s): %v", name, g.label, err))
		case same:
			check.UpToDate++
		default:
			check.Names = append(check.Names, storage.Safe(name))
		}
	}
}

// sameAsSource hashes the skill at the entry's skillPath in the fetched tree the way an install
// would copy it, and the installed tree as it is on disk.
func (s Service) sameAsSource(ctx context.Context, checkout string, entry skillkit.LockEntry, installed string) (bool, error) {
	if installed == "" {
		return false, errors.New("recorded in skills-lock.json but not installed")
	}
	skillPath := path.Clean(filepath.ToSlash(entry.SkillPath))
	dir := path.Dir(skillPath)
	if path.Base(skillPath) != "SKILL.md" || !filepath.IsLocal(filepath.FromSlash(dir)) {
		return false, fmt.Errorf("invalid skillPath %q in skills-lock.json", entry.SkillPath)
	}
	root, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return false, err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(dir)))
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("%s no longer exists in the source", entry.SkillPath)
	}
	if err != nil {
		return false, err
	}
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return false, fmt.Errorf("skillPath %q leaves the source", entry.SkillPath)
	}
	if info, err := os.Stat(filepath.Join(full, "SKILL.md")); err != nil || info.IsDir() {
		return false, fmt.Errorf("%s no longer exists in the source", entry.SkillPath)
	}
	fetched, err := skillkit.CopyHash(ctx, full)
	if err != nil {
		return false, err
	}
	current, err := skillkit.FolderHash(installed)
	if err != nil {
		return false, err
	}
	return fetched == current, nil
}

// MarkOutdated flags installed skills that the upstream check reported as having an update.
func (i Inventory) MarkOutdated(names []string) Inventory {
	skills := slices.Clone(i.Skills)
	for n := range skills {
		if slices.Contains(names, skills[n].Name) && skills[n].Status == "installed" {
			skills[n].Status = "outdated"
		}
	}
	i.Skills = skills
	return i
}
