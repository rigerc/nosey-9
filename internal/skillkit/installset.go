package skillkit

import (
	"context"
	"errors"
	"strings"
)

// Report is the structured outcome of installing a set of skills.
type Report struct {
	// Skills has one entry per requested (or, with no request, available) skill, in order.
	Skills []InstallResult
}

// Installed lists skills that reached at least one agent; these belong in the lock.
func (r Report) Installed() []InstallResult {
	var out []InstallResult
	for _, s := range r.Skills {
		if s.Installed() {
			out = append(out, s)
		}
	}
	return out
}

// Select returns the available skills matching names, in the order of names.
// A name matches a skill's name case-insensitively, or by its sanitized form,
// like upstream's filterSkills. Names that match nothing are returned in a
// *MissingSkillError alongside the skills that did match. Empty names select all.
func Select(available []Skill, names []string) ([]Skill, error) {
	if len(names) == 0 {
		return append([]Skill(nil), available...), nil
	}
	var out []Skill
	var missing []string
	chosen := map[int]bool{}
	for _, want := range names {
		found := false
		for i, s := range available {
			if strings.EqualFold(s.Name, want) {
				found = true
				if !chosen[i] {
					chosen[i] = true
					out = append(out, s)
				}
			}
		}
		if !found {
			for i, s := range available {
				if SanitizeName(s.Name) == SanitizeName(want) {
					found = true
					if !chosen[i] {
						chosen[i] = true
						out = append(out, s)
					}
				}
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return out, &MissingSkillError{Names: missing}
	}
	return out, nil
}

// InstallSkills installs the requested skills (all of available when requested
// is empty) for the agents. A requested skill missing from available is an
// error and nothing is installed. Otherwise every skill is attempted in order,
// the context is checked before each, and skills after cancellation are
// reported as not-started. The Report is complete even when an error is returned.
func InstallSkills(ctx context.Context, requested []string, available []Skill, agentIDs []string, target Target) (Report, error) {
	selected, selErr := Select(available, requested)
	var report Report
	ids, agentErr := resolveAgents(agentIDs, target)
	notStarted := func(s Skill) InstallResult {
		r := InstallResult{Skill: s.Name, Status: StatusNotStarted}
		for _, id := range ids {
			r.Agents = append(r.Agents, AgentOutcome{Agent: id, Status: StatusNotStarted})
		}
		return r
	}
	if selErr != nil || agentErr != nil {
		for _, s := range selected {
			report.Skills = append(report.Skills, notStarted(s))
		}
		var missing *MissingSkillError
		if errors.As(selErr, &missing) {
			for _, name := range missing.Names {
				report.Skills = append(report.Skills, InstallResult{Skill: name, Status: StatusFailed, Err: &MissingSkillError{Names: []string{name}}})
			}
		}
		return report, errors.Join(selErr, agentErr)
	}
	var errs []error
	for i, s := range selected {
		if err := ctx.Err(); err != nil {
			for _, rest := range selected[i:] {
				report.Skills = append(report.Skills, notStarted(rest))
			}
			errs = append(errs, err)
			break
		}
		res, err := Install(ctx, s, ids, target)
		report.Skills = append(report.Skills, res)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return report, errors.Join(errs...)
}
