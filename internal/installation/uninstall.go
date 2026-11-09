package installation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// UninstallOptions selects tracked skills to remove.
type UninstallOptions struct {
	// Targets are tracked skill names or catalog IDs.
	Targets []string
	// IgnoreRequirements removes skills even when an installed skill still requires them.
	IgnoreRequirements bool
}

// RemoveSkill is one skill an uninstall removes.
type RemoveSkill struct {
	// Name is the tracked skill name in skills-lock.json.
	Name string
	// ID is the catalog ID; empty for a tracked skill that is not in the current catalog.
	ID string
}

// UninstallPlan lists the skills to remove in removal order: a skill that requires another
// selected skill is removed before it.
type UninstallPlan struct {
	Skills []RemoveSkill
	// Overridden lists requirements broken on purpose with IgnoreRequirements.
	Overridden []string
	// recheck repeats the planning checks against the project's current state.
	recheck func(context.Context) (UninstallPlan, error)
}

// Lines renders the plan for the confirmation prompt.
func (p UninstallPlan) Lines() []string {
	var lines []string
	for _, s := range p.Skills {
		line := "remove " + storage.Safe(s.Name)
		if s.ID != "" {
			line += " (" + storage.Safe(s.ID) + ")"
		}
		lines = append(lines, line)
	}
	for _, o := range p.Overridden {
		lines = append(lines, "warning: "+storage.Safe(o))
	}
	return lines
}

// PlanUninstall resolves tracked names and catalog IDs, evaluates the whole selection and
// blocks removals that would leave an installed skill without a requirement. It does not take
// the operation lock.
func (s Service) PlanUninstall(_ context.Context, c domain.Catalog, recs catalog.Recommendations, opts UninstallOptions) (UninstallPlan, error) {
	var plan UninstallPlan
	if len(opts.Targets) == 0 {
		return plan, PolicyError{Reason: "name at least one skill to uninstall"}
	}
	lock, items, _, err := s.snapshot(c)
	if err != nil {
		return plan, err
	}
	// Identity of every tracked skill, whether or not it is currently on disk.
	idOf := map[string]string{}
	byID := map[string]string{}
	for _, name := range lock.Names() {
		if id, _, _ := s.catalogIdentity(name, lock.Skills[name], c); id != nil {
			idOf[name] = *id
			byID[*id] = name
		}
	}
	chosen := map[string]bool{}
	for _, target := range opts.Targets {
		switch name := target; {
		case hasLockEntry(lock, name):
			chosen[name] = true
		case byID[target] != "":
			chosen[byID[target]] = true
		default:
			if _, known := c.Skills[target]; known {
				return plan, PolicyError{Reason: target + " is not tracked in skills-lock.json, so Nosey did not install it"}
			}
			return plan, PolicyError{Reason: target + " is not a tracked skill or catalog ID; only skills recorded in skills-lock.json can be uninstalled"}
		}
	}
	selectedIDs := map[string]string{}
	for name := range chosen {
		if id := idOf[name]; id != "" {
			selectedIDs[id] = name
		}
	}
	var blocked []string
	for _, it := range items {
		if chosen[it.Name] || !it.Tracked || it.ID == "" {
			continue
		}
		for _, req := range requirements(c, recs, it.ID) {
			if name, ok := selectedIDs[req]; ok {
				blocked = append(blocked, fmt.Sprintf("%s requires %s", it.Name, name))
			}
		}
	}
	slices.Sort(blocked)
	blocked = slices.Compact(blocked)
	if len(blocked) > 0 {
		if !opts.IgnoreRequirements {
			return plan, PolicyError{Reason: strings.Join(blocked, "; ") + "; remove them too or pass --ignore-requirements"}
		}
		plan.Overridden = blocked
	}
	// Dependents first: walk requirements among the selection, then reverse the postorder.
	names := keys(chosen)
	visited := map[string]bool{}
	var post []string
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		if id := idOf[name]; id != "" {
			for _, req := range requirements(c, recs, id) {
				if dep, ok := selectedIDs[req]; ok {
					walk(dep)
				}
			}
		}
		post = append(post, name)
	}
	// Visit in reverse name order so unrelated skills come out alphabetically after the reversal.
	for i := len(names) - 1; i >= 0; i-- {
		walk(names[i])
	}
	slices.Reverse(post)
	for _, name := range post {
		plan.Skills = append(plan.Skills, RemoveSkill{Name: name, ID: idOf[name]})
	}
	plan.recheck = func(ctx context.Context) (UninstallPlan, error) { return s.PlanUninstall(ctx, c, recs, opts) }
	return plan, nil
}

// RecheckUninstall repeats the plan's checks under the operation lock, because a confirmed plan
// is built before the lock is taken and another process may have changed the project since.
func (o *Operation) RecheckUninstall(ctx context.Context, plan UninstallPlan) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lock == nil {
		return errors.New("uninstall operation is closed")
	}
	if plan.recheck == nil {
		return nil
	}
	current, err := plan.recheck(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(current.Skills, plan.Skills) || !slices.Equal(current.Overridden, plan.Overridden) {
		return PolicyError{Reason: "the project's skills changed since this uninstall was confirmed; review it and try again"}
	}
	return nil
}

func hasLockEntry(lock skillkit.ProjectLock, name string) bool {
	_, ok := lock.Skills[name]
	return ok
}

// RemoveStatus is the outcome of removing one skill.
type RemoveStatus string

// Removal outcomes.
const (
	RemoveDone       RemoveStatus = "removed"
	RemoveFailed     RemoveStatus = "failed"
	RemoveNotStarted RemoveStatus = "not-started"
)

// RemoveResult is what removing one skill did. It is complete even when removal failed.
type RemoveResult struct {
	Name, ID string
	Status   RemoveStatus
	// Removed lists the workspace-relative paths that were deleted.
	Removed []string
	// LockUpdated reports that the skill's entry was deleted from skills-lock.json.
	LockUpdated bool
	Err         error
}

// Line renders the result for terminal output.
func (r RemoveResult) Line() string {
	name := storage.Safe(r.Name)
	switch r.Status {
	case RemoveDone:
		return fmt.Sprintf("removed %s (%s)", name, strings.Join(r.removedText(), ", "))
	case RemoveFailed:
		line := fmt.Sprintf("failed %s: %s", name, errText(r.Err))
		if len(r.Removed) > 0 {
			line += fmt.Sprintf(" (already removed: %s; lock entry kept)", strings.Join(r.removedText(), ", "))
		}
		return line
	}
	return "not started " + name
}

func (r RemoveResult) removedText() []string {
	if len(r.Removed) == 0 {
		return []string{"no files were present"}
	}
	out := make([]string, len(r.Removed))
	for i, p := range r.Removed {
		out[i] = storage.Safe(p)
	}
	return out
}

// RunRemove removes one tracked skill from the canonical directory and every agent directory,
// then deletes its entry from skills-lock.json. The entry is kept when anything failed. The
// result is always populated.
func (o *Operation) RunRemove(ctx context.Context, skill RemoveSkill) (RemoveResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	res := RemoveResult{Name: skill.Name, ID: skill.ID, Status: RemoveFailed}
	if o.lock == nil {
		res.Err = errors.New("uninstall operation is closed")
		return res, res.Err
	}
	workspace := o.s.Store.Workspace
	lock, err := skillkit.ReadLock(workspace)
	if err != nil {
		res.Err = err
		return res, err
	}
	if !hasLockEntry(lock, skill.Name) {
		res.Err = fmt.Errorf("%s is no longer tracked in %s", skill.Name, skillkit.LockFile)
		return res, res.Err
	}
	removal, err := skillkit.Uninstall(ctx, skill.Name, skillkit.Target{Workspace: workspace, Agents: o.table})
	res.Removed = removal.Removed()
	if err != nil {
		res.Err = err
		return res, err
	}
	lock.Remove(skill.Name)
	if err = lock.Write(workspace); err != nil {
		res.Err = fmt.Errorf("removed %s but %s could not be updated: %w", skill.Name, skillkit.LockFile, err)
		return res, res.Err
	}
	res.LockUpdated, res.Status = true, RemoveDone
	return res, nil
}

// Uninstall removes the planned skills in order under one operation lock and writes the lock
// after each removal. It stops at the first failure; later skills are reported as not started.
// Skills outside the plan are never touched.
func (s Service) Uninstall(ctx context.Context, plan UninstallPlan, progress func(index, total int, result *RemoveResult)) ([]RemoveResult, error) {
	op, err := s.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.Close() }()
	if err = op.RecheckUninstall(ctx, plan); err != nil {
		return nil, err
	}
	results := make([]RemoveResult, 0, len(plan.Skills))
	for i, skill := range plan.Skills {
		if progress != nil {
			progress(i, len(plan.Skills), nil)
		}
		res, err := op.RunRemove(ctx, skill)
		results = append(results, res)
		if progress != nil {
			progress(i, len(plan.Skills), &res)
		}
		if err != nil {
			rest := plan.Skills[i+1:]
			for _, later := range rest {
				results = append(results, RemoveResult{Name: later.Name, ID: later.ID, Status: RemoveNotStarted})
			}
			return results, fmt.Errorf("uninstall failed for %s: %w (%d later skills not started)", storage.Safe(skill.Name), err, len(rest))
		}
	}
	return results, nil
}
