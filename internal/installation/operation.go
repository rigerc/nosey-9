package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// SourceResult is what running one SourcePlan did. It is complete even when the run failed.
type SourceResult struct {
	Source, Ref string
	// Commit is the resolved commit for git sources.
	Commit string
	// Skills has one entry per planned skill, in plan order.
	Skills []skillkit.InstallResult
	// Warnings are source-level problems, such as skipped unparsable skills.
	Warnings []skillkit.Warning
	// Started is false when an earlier source failed first.
	Started bool
	// LockWritten reports that skills-lock.json now records the installed skills.
	LockWritten bool
	// Err is the run's failure, including a failed lock write.
	Err error
	// Updates is set for an update run; it holds the reason and newness of each planned skill.
	Updates map[string]UpdateNote
}

// UpdateNote explains one planned update: why it is needed and whether the skill is new.
type UpdateNote struct {
	Reasons []string
	// New marks a newly required skill that is installed rather than refreshed.
	New bool
}

// Installed lists the skills that reached at least one agent.
func (r SourceResult) Installed() []string {
	var names []string
	for _, s := range r.Skills {
		if s.Installed() {
			names = append(names, s.Skill)
		}
	}
	return names
}

// Notes lists what the user must see even when the source succeeded: skipped agents,
// symlink fallbacks and warnings.
func (r SourceResult) Notes() []string {
	var notes []string
	for _, w := range r.Warnings {
		notes = append(notes, "warning: "+storage.Safe(w.Message))
	}
	for _, s := range r.Skills {
		for _, a := range s.Agents {
			switch {
			case a.Status == skillkit.StatusSkipped:
				notes = append(notes, fmt.Sprintf("skipped %s for %s: %s", a.Agent, storage.Safe(s.Skill), skipReason(a.Reason)))
			case a.SymlinkFallback:
				notes = append(notes, fmt.Sprintf("%s: symlink for %s failed; copied instead", a.Agent, storage.Safe(s.Skill)))
			}
		}
		for _, w := range s.Warnings {
			notes = append(notes, fmt.Sprintf("warning: %s: %s", storage.Safe(s.Skill), storage.Safe(w.Message)))
		}
	}
	return dedupe(notes)
}

// dedupe drops repeated lines, such as the same skipped link reported once per copy.
func dedupe(lines []string) []string {
	seen := map[string]bool{}
	out := lines[:0:0]
	for _, line := range lines {
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

func skipReason(reason string) string {
	switch reason {
	case skillkit.ReasonMissingAgentDir:
		return "its project directory does not exist (symlink installs skip such agents; create the directory or use --copy)"
	case skillkit.ReasonSourceOverlaps:
		return "the skill source overlaps the destination"
	}
	return reason
}

// Lines renders the result per skill for terminal output, followed by Notes.
func (r SourceResult) Lines() []string {
	if r.Updates != nil {
		return r.updateLines()
	}
	var lines []string
	for _, s := range r.Skills {
		name := storage.Safe(s.Skill)
		switch s.Status {
		case skillkit.StatusInstalled:
			var ids []string
			for _, a := range s.Agents {
				if a.Status == skillkit.StatusInstalled {
					ids = append(ids, a.Agent)
				}
			}
			line := fmt.Sprintf("installed %s for %s", name, strings.Join(ids, ", "))
			for _, a := range s.Agents {
				if a.Status == skillkit.StatusFailed {
					line += fmt.Sprintf("; failed for %s: %s", a.Agent, errText(a.Err))
				}
			}
			lines = append(lines, line)
		case skillkit.StatusFailed:
			lines = append(lines, fmt.Sprintf("failed %s: %s", name, errText(s.Err)))
		case skillkit.StatusSkipped:
			lines = append(lines, fmt.Sprintf("not installed %s: every agent was skipped", name))
		default:
			lines = append(lines, fmt.Sprintf("not started %s", name))
		}
	}
	return append(lines, r.Notes()...)
}

func errText(err error) string {
	if err == nil {
		return "unknown error"
	}
	return storage.Safe(err.Error())
}

// Operation is one lifecycle operation holding the workspace operation lock. Create it with
// Service.Begin and always Close it. The lock is a kernel advisory lock, so it is also
// released when the process dies.
type Operation struct {
	s     Service
	table skillkit.AgentTable
	lock  *skillkit.OperationLock
	mu    sync.Mutex
}

// Begin runs the project preflight, takes the operation lock and re-checks the
// project under it. A busy lock is a PolicyError saying another Nosey operation is running.
func (s Service) Begin(ctx context.Context) (*Operation, error) {
	if err := s.Preflight(); err != nil {
		return nil, err
	}
	table, err := s.agentTable()
	if err != nil {
		return nil, err
	}
	lock, err := skillkit.AcquireOperationLock(ctx, s.Store.Data, s.Store.Workspace, skillkit.LockOptions{Timeout: s.LockTimeout})
	if err != nil {
		var busy *skillkit.BusyError
		if errors.As(err, &busy) {
			return nil, PolicyError{Reason: busy.Error()}
		}
		return nil, err
	}
	if err = s.Preflight(); err != nil {
		_ = lock.Release()
		return nil, err
	}
	return &Operation{s: s, table: table, lock: lock}, nil
}

// Close releases the operation lock. It is safe to call more than once and on a nil Operation.
func (o *Operation) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	lock := o.lock
	o.lock = nil
	return lock.Release()
}

func (o *Operation) target(plan SourcePlan) skillkit.Target {
	mode := skillkit.ModeSymlink
	if plan.Method == "copy" {
		mode = skillkit.ModeCopy
	}
	return skillkit.Target{Workspace: o.s.Store.Workspace, Mode: mode, Agents: o.table}
}

func notStarted(plan SourcePlan) []skillkit.InstallResult {
	results := make([]skillkit.InstallResult, 0, len(plan.Skills))
	for _, skill := range plan.Skills {
		r := skillkit.InstallResult{Skill: skill.Name, Status: skillkit.StatusNotStarted}
		for _, id := range plan.Agents {
			r.Agents = append(r.Agents, skillkit.AgentOutcome{Agent: id, Status: skillkit.StatusNotStarted})
		}
		results = append(results, r)
	}
	return results
}

// NotStarted describes a source that never ran because an earlier one failed.
func NotStarted(plan SourcePlan) SourceResult {
	return SourceResult{Source: plan.Source, Ref: plan.Ref, Skills: notStarted(plan)}
}

// RunSource fetches the plan's source once, installs its skills and records every skill that
// reached an agent in skills-lock.json, whether or not the run failed or was cancelled. The
// result is always populated. A failed lock write is part of the returned error.
func (o *Operation) RunSource(ctx context.Context, plan SourcePlan) (SourceResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	res := SourceResult{Source: plan.Source, Ref: plan.Ref, Started: true, Skills: notStarted(plan)}
	if o.lock == nil {
		res.Err = errors.New("installation operation is closed")
		return res, res.Err
	}
	var (
		parsed skillkit.ParsedSource
		found  = map[string]skillkit.Skill{}
	)
	res.Err = o.install(ctx, plan, &res, &parsed, found)
	if installed := res.Installed(); len(installed) > 0 {
		if err := o.persist(plan, res, parsed, found); err != nil {
			res.Err = errors.Join(res.Err, fmt.Errorf("installed %s but %s could not be updated: %w", strings.Join(installed, ", "), skillkit.LockFile, err))
		} else {
			res.LockWritten = true
		}
	}
	return res, res.Err
}

func (o *Operation) install(ctx context.Context, plan SourcePlan, res *SourceResult, parsed *skillkit.ParsedSource, found map[string]skillkit.Skill) error {
	if len(plan.Skills) == 0 || len(plan.Agents) == 0 {
		return PolicyError{Reason: "source plan needs skills and agents"}
	}
	if plan.Method != "copy" && plan.Method != "symlink" {
		return PolicyError{Reason: "invalid installation method"}
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
	names := plan.names()
	for _, skill := range skills {
		found[skill.Name] = skill
	}
	target := o.target(plan)
	report, err := skillkit.InstallSkills(ctx, names, skills, plan.Agents, target)
	if len(report.Skills) > 0 {
		res.Skills = mergeResults(plan, report.Skills)
	}
	var missing *skillkit.MissingSkillError
	if errors.As(err, &missing) && problems != nil && len(problems.Parse) > 0 {
		err = fmt.Errorf("%w (%d SKILL.md files in the source could not be parsed: %s)", err, len(problems.Parse), problems.Error())
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		err = o.reconcile(target, res)
	}
	// A skill that reached no agent because every agent was skipped is not a success.
	var unplaced []string
	for _, r := range res.Skills {
		if r.Status == skillkit.StatusSkipped {
			unplaced = append(unplaced, storage.Safe(r.Skill))
		}
	}
	if len(unplaced) > 0 && ctx.Err() == nil {
		err = errors.Join(err, fmt.Errorf("%s was not installed for any agent because every selected agent was skipped", strings.Join(unplaced, ", ")))
	}
	return err
}

// fetchSkills fetches the plan's source once and discovers its skills. The caller closes the
// returned checkout, which must outlive any use of the skills' directories. Unparsable
// SKILL.md files that do not touch the planned names are reported as warnings.
func (o *Operation) fetchSkills(ctx context.Context, plan SourcePlan, res *SourceResult, parsed *skillkit.ParsedSource) (*skillkit.Checkout, []skillkit.Skill, *skillkit.DiscoverError, error) {
	src := skillkit.Source{URI: plan.Source, Ref: plan.Ref}
	if plan.Local() {
		src = skillkit.Source{LocalPath: plan.Source}
	}
	checkout, err := o.s.fetcher().Fetch(ctx, src)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("fetching %s: %w", storage.Safe(plan.Source), err)
	}
	*parsed, res.Commit = checkout.Parsed, checkout.Commit
	skills, err := skillkit.Discover(checkout.Dir, skillkit.DiscoverOptions{IncludeInternal: true, FullDepth: true, Agents: o.table})
	var problems *skillkit.DiscoverError
	if err != nil {
		if !errors.As(err, &problems) {
			return checkout, nil, nil, fmt.Errorf("discovering skills in %s: %w", storage.Safe(plan.Source), err)
		}
		warnings, rerr := problems.Resolve(checkout.Dir, plan.names())
		if rerr != nil {
			return checkout, nil, nil, fmt.Errorf("discovering skills in %s: %w", storage.Safe(plan.Source), rerr)
		}
		res.Warnings = append(res.Warnings, warnings...)
	}
	return checkout, skills, problems, nil
}

// mergeResults keeps plan order and fills in skills the engine did not report.
func mergeResults(plan SourcePlan, reported []skillkit.InstallResult) []skillkit.InstallResult {
	out := notStarted(plan)
	used := make([]bool, len(reported))
	for i := range out {
		for j, r := range reported {
			if !used[j] && strings.EqualFold(r.Skill, out[i].Skill) {
				out[i], used[j] = r, true
				break
			}
		}
	}
	for j, r := range reported {
		if !used[j] {
			out = append(out, r)
		}
	}
	return out
}

// reconcile checks every skill reported as installed against the project's real inventory, so a
// skill that is not on disk is never recorded in the lock.
func (o *Operation) reconcile(target skillkit.Target, res *SourceResult) error {
	inv, err := skillkit.ListInstalled(target)
	if err != nil {
		res.Warnings = append(res.Warnings, skillkit.Warning{Code: skillkit.WarnUnreadable, Message: "could not verify the installed skills: " + err.Error()})
		return nil
	}
	onDisk := map[string]bool{}
	for _, item := range inv.Skills {
		onDisk[item.Name] = true
	}
	var errs []error
	for i, s := range res.Skills {
		if s.Installed() && !onDisk[s.Skill] {
			res.Skills[i].Status = skillkit.StatusFailed
			res.Skills[i].Err = fmt.Errorf("%s was installed but is not in the project inventory", s.Skill)
			errs = append(errs, res.Skills[i].Err)
		}
	}
	return errors.Join(errs...)
}

func (o *Operation) persist(plan SourcePlan, res SourceResult, parsed skillkit.ParsedSource, found map[string]skillkit.Skill) error {
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
		lock.Skills[r.Skill] = withInstallMode(lock.Skills[r.Skill], plan.Method)
	}
	return lock.Write(workspace)
}

// withInstallMode records `installMode: copy` for copy installs, as skills@1.7.6 does, so a later
// `npx skills update` keeps the mode, and drops a stale value left by an earlier copy install.
func withInstallMode(entry skillkit.LockEntry, method string) skillkit.LockEntry {
	extra := make(map[string]json.RawMessage, len(entry.Extra)+1)
	for k, v := range entry.Extra {
		extra[k] = v
	}
	if method == "copy" {
		extra["installMode"] = json.RawMessage(`"copy"`)
	} else {
		delete(extra, "installMode")
	}
	if len(extra) == 0 {
		extra = nil
	}
	entry.Extra = extra
	return entry
}

// Progress reports the CLI install loop: Result is nil before a source starts.
type Progress struct {
	Index, Total int
	Plan         SourcePlan
	Result       *SourceResult
}

// Install runs every plan in order under one operation lock and stops at the first failed
// source. The results cover every plan; sources after the failure are not started.
func (s Service) Install(ctx context.Context, plans []SourcePlan, progress func(Progress)) ([]SourceResult, error) {
	return s.runPlans(ctx, "install", plans, progress, (*Operation).RunSource)
}

func (s Service) runPlans(ctx context.Context, verb string, plans []SourcePlan, progress func(Progress), run func(*Operation, context.Context, SourcePlan) (SourceResult, error)) ([]SourceResult, error) {
	op, err := s.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.Close() }()
	results := make([]SourceResult, 0, len(plans))
	for i, plan := range plans {
		if progress != nil {
			progress(Progress{Index: i, Total: len(plans), Plan: plan})
		}
		res, err := run(op, ctx, plan)
		results = append(results, res)
		if progress != nil {
			progress(Progress{Index: i, Total: len(plans), Plan: plan, Result: &res})
		}
		if err != nil {
			for _, later := range plans[i+1:] {
				results = append(results, NotStarted(later))
			}
			return results, fmt.Errorf("%s failed for %s: %w (%d later sources not started)", verb, storage.Safe(plan.Source), err, len(plans)-i-1)
		}
	}
	return results, nil
}

// Preflight refuses project roots and lock states the installer must not touch.
func (s Service) Preflight() error {
	if home, err := os.UserHomeDir(); err == nil && samePath(home, s.Store.Workspace) {
		return PolicyError{Reason: "refusing to install into the home directory; run from a project directory"}
	}
	if _, err := skillkit.ReadLock(s.Store.Workspace); err != nil && !errors.Is(err, skillkit.ErrLockMissing) {
		return PolicyError{Reason: err.Error() + "; fix or delete it before installing"}
	}
	return nil
}

func samePath(a, b string) bool {
	left, leftErr := filepath.EvalSymlinks(a)
	right, rightErr := filepath.EvalSymlinks(b)
	return leftErr == nil && rightErr == nil && left == right
}
