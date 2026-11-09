package installation

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// Reasons an update is needed.
const (
	ReasonPinChanged     = "catalog pin changed"
	ReasonContentChanged = "upstream content changed"
)

// UpdateOptions selects what Service.PlanUpdate updates.
type UpdateOptions struct {
	// IDs are catalog IDs or tracked skill names. Empty selects every outdated catalog-managed skill.
	IDs             []string
	IgnoreConflicts bool
	// Check is a finished update check to reuse; nil runs CheckUpdates, which needs network access.
	Check *UpdateCheck
}

// SkippedSkill is an installed skill an update left alone.
type SkippedSkill struct{ Name, Reason string }

// UpdatePlan is the confirmed shape of an update: sources in dependency order, what was skipped
// and warnings. It performs no I/O to build the sources; RunUpdate decides the install
// locations from the filesystem when it runs.
type UpdatePlan struct {
	Plans   []SourcePlan
	Skipped []SkippedSkill
	Notes   []string
}

// Empty reports that nothing will be updated.
func (p UpdatePlan) Empty() bool { return len(p.Plans) == 0 }

// Count is the number of skills the plan touches.
func (p UpdatePlan) Count() int {
	n := 0
	for _, plan := range p.Plans {
		n += len(plan.Skills)
	}
	return n
}

// Lines renders the plan for the confirmation prompt. Values are sanitized for the terminal.
func (p UpdatePlan) Lines() []string {
	var lines []string
	for _, plan := range p.Plans {
		ref := plan.Ref
		if ref == "" {
			ref = "default branch"
		}
		lines = append(lines, fmt.Sprintf("Source: %s (ref: %s)", storage.Safe(plan.Source), storage.Safe(ref)))
		for _, skill := range plan.Skills {
			verb := "update"
			if skill.Required {
				verb = "install (required)"
			}
			lines = append(lines, fmt.Sprintf("  %s %s: %s", verb, storage.Safe(skill.Name), storage.Safe(strings.Join(skill.Reasons, "; "))))
			for _, target := range skill.Targets {
				lines = append(lines, "    at "+storage.Safe(target))
			}
		}
	}
	return append(lines, p.SkippedLines()...)
}

// SkippedLines lists skills the update left alone, then warnings.
func (p UpdatePlan) SkippedLines() []string {
	var lines []string
	for _, s := range p.Skipped {
		lines = append(lines, fmt.Sprintf("skipped %s: %s", storage.Safe(s.Name), storage.Safe(s.Reason)))
	}
	for _, n := range p.Notes {
		lines = append(lines, "warning: "+storage.Safe(n))
	}
	return lines
}

// UpdateCheckError reports that the update check could not cover every skill, so nothing was updated.
type UpdateCheckError struct{ Failures []string }

func (e UpdateCheckError) Error() string {
	return "update check failed; nothing was updated: " + strings.Join(e.Failures, "; ")
}

// installedSkill joins one skill found on disk with its lock entry and catalog identity.
type installedSkill struct {
	skillkit.InstalledSkill
	Entry   skillkit.LockEntry
	Tracked bool
	// ID is the catalog skill ID; empty when the skill is not in the current catalog.
	ID string
}

// snapshot reads the filesystem inventory and the lock and maps both to the catalog.
func (s Service) snapshot(c domain.Catalog) (skillkit.ProjectLock, []installedSkill, []string, error) {
	table, err := s.agentTable()
	if err != nil {
		return skillkit.ProjectLock{}, nil, nil, err
	}
	listed, err := skillkit.ListInstalled(skillkit.Target{Workspace: s.Store.Workspace, Agents: table})
	if err != nil {
		return skillkit.ProjectLock{}, nil, nil, fmt.Errorf("reading installed skills: %w", err)
	}
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	switch {
	case errors.Is(err, skillkit.ErrLockMissing):
		lock = skillkit.NewProjectLock()
	case err != nil:
		return skillkit.ProjectLock{}, nil, nil, PolicyError{Reason: err.Error() + "; fix or delete it first"}
	}
	var notes []string
	for _, w := range listed.Warnings {
		notes = append(notes, w.Message)
	}
	items := make([]installedSkill, 0, len(listed.Skills))
	for _, item := range listed.Skills {
		entry, tracked := lock.Skills[item.Name]
		it := installedSkill{InstalledSkill: item, Entry: entry, Tracked: tracked}
		if tracked {
			if id, _, _ := s.catalogIdentity(item.Name, entry, c); id != nil {
				it.ID = *id
			}
		}
		items = append(items, it)
	}
	return lock, items, notes, nil
}

func shortRef(ref string) string {
	switch {
	case ref == "":
		return "default branch"
	case len(ref) == 40:
		return ref[:7]
	}
	return ref
}

// requirements lists the catalog and mapping requirements of a skill.
func requirements(c domain.Catalog, recs catalog.Recommendations, id string) []string {
	return unique(append(slices.Clone(c.Skills[id].Requires), recs.Requires[id]...))
}

// PlanUpdate selects the catalog-managed skills to update and builds one SourcePlan per source
// from the current catalog. Outdated means the catalog pin differs from the locked ref or the
// native update check found different content; each planned skill keeps those reasons. A failed
// check is an error. Requirements missing from the project are added and installed where the
// skills requiring them live. It does not take the operation lock.
func (s Service) PlanUpdate(ctx context.Context, c domain.Catalog, recs catalog.Recommendations, opts UpdateOptions) (UpdatePlan, error) {
	var plan UpdatePlan
	_, items, notes, err := s.snapshot(c)
	if err != nil {
		return plan, err
	}
	plan.Notes = notes
	byID := map[string]*installedSkill{}
	byName := map[string]*installedSkill{}
	for i := range items {
		byName[items[i].Name] = &items[i]
		if items[i].ID != "" {
			byID[items[i].ID] = &items[i]
		}
	}
	explicit := len(opts.IDs) > 0
	var candidates []*installedSkill
	if explicit {
		seen := map[string]bool{}
		for _, arg := range opts.IDs {
			var it *installedSkill
			switch {
			case byID[arg] != nil:
				it = byID[arg]
			case byName[arg] != nil:
				it = byName[arg]
			default:
				if _, ok := c.Skills[arg]; ok {
					return plan, PolicyError{Reason: arg + " is not installed; use nosey install"}
				}
				if err = unavailableSkill(c, arg); err != nil {
					return plan, err
				}
				return plan, PolicyError{Reason: "unknown skill " + arg}
			}
			if seen[it.Name] {
				continue
			}
			seen[it.Name] = true
			candidates = append(candidates, it)
		}
	} else {
		for i := range items {
			candidates = append(candidates, &items[i])
		}
	}
	var managed []*installedSkill
	for _, it := range candidates {
		switch {
		case it.ID != "":
			managed = append(managed, it)
		case !it.Tracked:
			plan.Skipped = append(plan.Skipped, SkippedSkill{it.Name, "not tracked in skills-lock.json"})
		default:
			plan.Skipped = append(plan.Skipped, SkippedSkill{it.Name, "not in the current catalog"})
		}
	}
	if len(managed) == 0 {
		slices.SortFunc(plan.Skipped, func(a, b SkippedSkill) int { return strings.Compare(a.Name, b.Name) })
		return plan, nil
	}
	check := UpdateCheck{}
	if opts.Check != nil {
		check = *opts.Check
	} else if check, err = s.CheckUpdates(ctx); err != nil {
		return plan, err
	}
	if check.Failed > 0 || len(check.Failures) > 0 {
		failures := check.Failures
		if len(failures) == 0 {
			failures = []string{skillCount(check.Failed) + " could not be checked"}
		}
		return plan, UpdateCheckError{Failures: failures}
	}
	content := map[string]bool{}
	for _, name := range check.Names {
		content[name] = true
	}
	reasons := map[string][]string{}
	var selected []string
	for _, it := range managed {
		skill := c.Skills[it.ID]
		var why []string
		if pinChanged(it.Entry, &skill) {
			why = append(why, fmt.Sprintf("%s (%s → %s)", ReasonPinChanged, shortRef(it.Entry.Ref), shortRef(skill.Source.Ref)))
		}
		if content[storage.Safe(it.Name)] {
			why = append(why, ReasonContentChanged)
		}
		if len(why) == 0 {
			if explicit {
				plan.Skipped = append(plan.Skipped, SkippedSkill{it.Name, "already up to date"})
			}
			continue
		}
		reasons[it.ID] = why
		selected = append(selected, it.ID)
	}
	slices.Sort(selected)
	slices.SortFunc(plan.Skipped, func(a, b SkippedSkill) int { return strings.Compare(a.Name, b.Name) })
	if len(selected) == 0 {
		return plan, nil
	}
	ordered, err := dependencyOrder(c, recs, selected)
	if err != nil {
		return plan, err
	}
	fresh := map[string]bool{}
	for _, id := range ordered {
		if byID[id] == nil {
			fresh[id] = true
		}
	}
	if err = checkUpdateSelection(c, recs, ordered, fresh, byID, items, opts.IgnoreConflicts); err != nil {
		return plan, err
	}
	dependents := map[string][]string{}
	for _, id := range append(slices.Clone(ordered), keys(byID)...) {
		for _, dep := range requirements(c, recs, id) {
			dependents[dep] = append(dependents[dep], id)
		}
	}
	inv := map[string]skillkit.InstalledSkill{}
	for _, it := range items {
		inv[it.Name] = it.InstalledSkill
	}
	bySource := map[string]int{}
	for _, id := range ordered {
		if byID[id] != nil && reasons[id] == nil {
			continue // an installed requirement that is not selected stays as it is
		}
		skill := c.Skills[id]
		source, ref, err := addSource(c, skill)
		if err != nil {
			return plan, err
		}
		add := AddSkill{ID: id, Name: skill.Source.SkillName, Required: fresh[id], Reasons: reasons[id]}
		var locs []skillkit.Location
		if fresh[id] {
			add.RequiredBy = installedRequirers(id, dependents, fresh, byID)
			add.Reasons = []string{"required by " + strings.Join(add.RequiredBy, ", ")}
			locs = unionLocations(add.Name, add.RequiredBy, inv)
		} else {
			locs = byID[id].Locations
		}
		for _, l := range locs {
			add.Targets = append(add.Targets, locationText(l))
		}
		key := source + "#" + ref
		at, ok := bySource[key]
		if !ok {
			at = len(plan.Plans)
			bySource[key] = at
			plan.Plans = append(plan.Plans, SourcePlan{Source: source, Ref: ref})
		}
		plan.Plans[at].Skills = append(plan.Plans[at].Skills, add)
	}
	return plan, nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// installedRequirers returns the names of installed skills that require id, directly or through
// other skills that are about to be installed.
func installedRequirers(id string, dependents map[string][]string, fresh map[string]bool, byID map[string]*installedSkill) []string {
	var names []string
	seen := map[string]bool{id: true}
	var walk func(string)
	walk = func(cur string) {
		for _, d := range dependents[cur] {
			if seen[d] {
				continue
			}
			seen[d] = true
			switch {
			case byID[d] != nil:
				names = append(names, byID[d].Name)
			case fresh[d]:
				walk(d)
			}
		}
	}
	walk(id)
	slices.Sort(names)
	return slices.Compact(names)
}

// unionLocations is the set of skills directories used by any of the named installed skills,
// as locations for a skill called name: canonical first, then by directory. A directory where
// any requirer is a symlink gets a symlink.
func unionLocations(name string, requirers []string, inv map[string]skillkit.InstalledSkill) []skillkit.Location {
	byDir := map[string]*skillkit.Location{}
	dirName := skillkit.SanitizeName(name)
	for _, r := range requirers {
		for _, l := range inv[r].Locations {
			cur := byDir[l.Dir]
			if cur == nil {
				cur = &skillkit.Location{Dir: l.Dir, Path: path.Join(l.Dir, dirName)}
				byDir[l.Dir] = cur
			}
			cur.Symlink = cur.Symlink || l.Symlink
			for _, a := range l.Agents {
				if !slices.Contains(cur.Agents, a) {
					cur.Agents = append(cur.Agents, a)
				}
			}
		}
	}
	dirs := keys(byDir)
	slices.SortStableFunc(dirs, func(a, b string) int {
		switch {
		case a == skillkit.UniversalSkillsDir && b != a:
			return -1
		case b == skillkit.UniversalSkillsDir && a != b:
			return 1
		}
		return strings.Compare(a, b)
	})
	out := make([]skillkit.Location, 0, len(dirs))
	for _, d := range dirs {
		slices.Sort(byDir[d].Agents)
		out = append(out, *byDir[d])
	}
	return out
}

func locationText(l skillkit.Location) string {
	if l.Symlink {
		return l.Path + " (symlink)"
	}
	return l.Path + " (copy)"
}

// checkUpdateSelection blocks name clashes and conflicts that involve a newly installed
// requirement. Conflicts between skills that are already installed existed before the update and
// do not block refreshing them.
func checkUpdateSelection(c domain.Catalog, recs catalog.Recommendations, ordered []string, fresh map[string]bool, installed map[string]*installedSkill, items []installedSkill, ignore bool) error {
	names := map[string]string{}
	for _, id := range ordered {
		name := c.Skills[id].Source.SkillName
		key := strings.ToLower(name)
		if other, ok := names[key]; ok {
			return PolicyError{Reason: fmt.Sprintf("%s and %s both install as %q; select only one", other, id, name)}
		}
		names[key] = id
		if !fresh[id] {
			continue
		}
		for _, it := range items {
			if strings.EqualFold(it.Name, name) && it.ID != id {
				return PolicyError{Reason: fmt.Sprintf("installing %s would replace the installed skill %q", id, it.Name)}
			}
		}
	}
	var problems []string
	for _, id := range ordered {
		for _, conflict := range requirementsConflicts(c, recs, id) {
			if (fresh[id] || fresh[conflict]) && (slices.Contains(ordered, conflict) || installed[conflict] != nil) {
				problems = append(problems, id+" conflicts with "+conflict)
			}
		}
		if !fresh[id] {
			continue
		}
		for other := range installed {
			if slices.Contains(requirementsConflicts(c, recs, other), id) {
				problems = append(problems, other+" conflicts with "+id)
			}
		}
	}
	if len(problems) > 0 && !ignore {
		slices.Sort(problems)
		return PolicyError{Reason: strings.Join(slices.Compact(problems), "; ")}
	}
	return nil
}

func requirementsConflicts(c domain.Catalog, recs catalog.Recommendations, id string) []string {
	return unique(append(slices.Clone(c.Skills[id].ConflictsWith), recs.Conflicts[id]...))
}

// Update runs a confirmed plan under one operation lock and stops at the first failed source.
// The results cover every source; sources after the failure are not started.
func (s Service) Update(ctx context.Context, plan UpdatePlan, progress func(Progress)) ([]SourceResult, error) {
	return s.runPlans(ctx, "update", plan.Plans, progress, (*Operation).RunUpdate)
}

// RunUpdate fetches the plan's source once and refreshes its skills at the locations they have
// on disk right now, preserving each location's symlink or copy mode. A newly required skill
// goes to the union of the locations of the skills requiring it. The lock is written after the
// source whether or not it failed; an existing entry keeps its installMode.
func (o *Operation) RunUpdate(ctx context.Context, plan SourcePlan) (SourceResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	res := SourceResult{Source: plan.Source, Ref: plan.Ref, Started: true, Skills: notStarted(plan), Updates: map[string]UpdateNote{}}
	for _, skill := range plan.Skills {
		res.Updates[skill.Name] = UpdateNote{Reasons: skill.Reasons, New: skill.Required}
	}
	if o.lock == nil {
		res.Err = errors.New("installation operation is closed")
		return res, res.Err
	}
	var (
		parsed skillkit.ParsedSource
		found  = map[string]skillkit.Skill{}
		fresh  = map[string]string{}
	)
	res.Err = o.update(ctx, plan, &res, &parsed, found, fresh)
	if installed := res.Installed(); len(installed) > 0 {
		if err := o.persistUpdate(res, parsed, found, fresh); err != nil {
			res.Err = errors.Join(res.Err, fmt.Errorf("updated %s but %s could not be updated: %w", strings.Join(installed, ", "), skillkit.LockFile, err))
		} else {
			res.LockWritten = true
		}
	}
	return res, res.Err
}

func (o *Operation) update(ctx context.Context, plan SourcePlan, res *SourceResult, parsed *skillkit.ParsedSource, found map[string]skillkit.Skill, fresh map[string]string) error {
	if len(plan.Skills) == 0 {
		return PolicyError{Reason: "update plan needs skills"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	checkout, skills, problems, err := o.fetchSkills(ctx, plan, res, parsed)
	if checkout != nil {
		defer func() { _ = checkout.Close() }()
	}
	if err != nil {
		return err
	}
	for _, skill := range skills {
		found[skill.Name] = skill
	}
	target := o.target(plan)
	listed, err := skillkit.ListInstalled(target)
	if err != nil {
		return fmt.Errorf("reading installed skills: %w", err)
	}
	inv := map[string]skillkit.InstalledSkill{}
	for _, item := range listed.Skills {
		inv[item.Name] = item
	}
	selected, err := skillkit.Select(skills, plan.names())
	if err != nil {
		var missing *skillkit.MissingSkillError
		if errors.As(err, &missing) && problems != nil && len(problems.Parse) > 0 {
			err = fmt.Errorf("%w (%d SKILL.md files in the source could not be parsed: %s)", err, len(problems.Parse), problems.Error())
		}
		return err
	}
	var errs []error
	var reported []skillkit.InstallResult
	for _, skill := range selected {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		var add AddSkill
		if at := slices.IndexFunc(plan.Skills, func(a AddSkill) bool { return strings.EqualFold(a.Name, skill.Name) }); at >= 0 {
			add = plan.Skills[at]
		}
		var locs []skillkit.Location
		if item, ok := inv[skill.Name]; ok {
			locs = item.Locations
		} else if add.Required {
			locs = unionLocations(skill.Name, add.RequiredBy, inv)
			method := "copy"
			for _, l := range locs {
				if l.Symlink {
					method = "symlink"
				}
			}
			fresh[skill.Name] = method
		}
		if len(locs) == 0 {
			r := skillkit.InstallResult{Skill: skill.Name, Status: skillkit.StatusFailed, Err: fmt.Errorf("%s has no installed location to update", skill.Name)}
			reported = append(reported, r)
			errs = append(errs, r.Err)
			continue
		}
		r, ierr := skillkit.InstallAt(ctx, skill, locs, target)
		reported = append(reported, r)
		if ierr != nil {
			errs = append(errs, ierr)
		}
	}
	if len(reported) > 0 {
		res.Skills = mergeResults(plan, reported)
	}
	if len(errs) == 0 && ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	if len(errs) == 0 {
		if rerr := o.reconcile(target, res); rerr != nil {
			errs = append(errs, rerr)
		}
	}
	var unplaced []string
	for _, r := range res.Skills {
		if r.Status == skillkit.StatusSkipped {
			unplaced = append(unplaced, storage.Safe(r.Skill))
		}
	}
	if len(unplaced) > 0 && ctx.Err() == nil {
		errs = append(errs, fmt.Errorf("%s was not updated because every location was skipped", strings.Join(unplaced, ", ")))
	}
	return errors.Join(errs...)
}

// persistUpdate records refreshed skills. Existing entries keep unknown fields, including
// installMode, because the install locations did not change; a newly installed skill records
// copy mode when none of its locations is a symlink.
func (o *Operation) persistUpdate(res SourceResult, parsed skillkit.ParsedSource, found map[string]skillkit.Skill, fresh map[string]string) error {
	workspace := o.s.Store.Workspace
	lock, err := skillkit.ReadLock(workspace)
	switch {
	case errors.Is(err, skillkit.ErrLockMissing):
		lock = skillkit.NewProjectLock()
	case err != nil:
		return err
	}
	for _, r := range res.Skills {
		if !r.Installed() {
			continue
		}
		skill, ok := found[r.Skill]
		if !ok {
			return fmt.Errorf("no discovered skill for %s", r.Skill)
		}
		lock.Set(r.Skill, skillkit.NewLockEntry(parsed, skillkit.SkillMDPath(skill), r.Hash))
		if method, isNew := fresh[r.Skill]; isNew {
			lock.Skills[r.Skill] = withInstallMode(lock.Skills[r.Skill], method)
		}
	}
	return lock.Write(workspace)
}

func (r SourceResult) updateLines() []string {
	var lines []string
	for _, s := range r.Skills {
		name := storage.Safe(s.Skill)
		note := r.Updates[s.Skill]
		why := ""
		if len(note.Reasons) > 0 {
			why = " (" + storage.Safe(strings.Join(note.Reasons, "; ")) + ")"
		}
		switch s.Status {
		case skillkit.StatusInstalled:
			verb := "updated"
			if note.New {
				verb = "installed"
			}
			var at []string
			for _, a := range s.Agents {
				if a.Status == skillkit.StatusInstalled {
					at = append(at, a.Path+" ("+string(a.Mode)+")")
				}
			}
			line := fmt.Sprintf("%s %s%s at %s", verb, name, why, strings.Join(at, ", "))
			for _, a := range s.Agents {
				if a.Status == skillkit.StatusFailed {
					line += fmt.Sprintf("; failed at %s: %s", a.Path, errText(a.Err))
				}
			}
			lines = append(lines, line)
		case skillkit.StatusFailed:
			lines = append(lines, fmt.Sprintf("failed %s%s: %s", name, why, errText(s.Err)))
		case skillkit.StatusSkipped:
			lines = append(lines, fmt.Sprintf("not updated %s: every location was skipped", name))
		default:
			lines = append(lines, fmt.Sprintf("not started %s", name))
		}
	}
	return append(lines, r.Notes()...)
}
