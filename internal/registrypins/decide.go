package registrypins

import (
	"fmt"
	"maps"
	"strings"
	"time"
)

type action int

const (
	actionSkip action = iota
	actionKeep
	actionBump
	actionMissing
)

type decision struct {
	action action
	reason string
	ref    string
}

// decide turns one observation into an action. Anything uncertain is a skip,
// so a transient failure can never move a pin or count as a miss.
func decide(pin Pin, probe Probe) decision {
	if probe.Repo == RepoGone {
		return decision{action: actionMissing, reason: reasonRepoGone}
	}
	if probe.Repo != RepoOK {
		return decision{action: actionSkip}
	}
	// A quarantined skill is always inspected; others are current while HEAD is the pin.
	if probe.Head == pin.Commit && !pin.Quarantined {
		return decision{action: actionKeep}
	}
	switch {
	case !probe.Inspected || probe.HeadHits > 1:
		return decision{action: actionSkip}
	case probe.HeadHits == 0:
		return decision{action: actionMissing, reason: reasonSkillAbsent}
	case probe.Head == pin.Commit:
		return decision{action: actionKeep}
	case probe.PinGone:
		return decision{action: actionBump, ref: probe.Head}
	case probe.SameTree == nil:
		return decision{action: actionSkip}
	case *probe.SameTree:
		return decision{action: actionKeep}
	default:
		return decision{action: actionBump, ref: probe.Head}
	}
}

func daysBetween(from, to string) int {
	start, startErr := time.Parse(dateLayout, from)
	end, endErr := time.Parse(dateLayout, to)
	if startErr != nil || endErr != nil {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}

type quarantineCandidate struct {
	id, reason, commit string
}

// applier accumulates the outcome of applying decisions to a registry.
type applier struct {
	today      string
	enforce    bool
	edits      map[string]edit
	strikes    map[string]Strike
	summary    Summary
	candidates []quarantineCandidate
}

// apply computes the skill edits and next probe history without writing.
func apply(skills []skillRecord, history map[string]Strike, decisions map[string]decision, today string, enforce bool) (map[string]edit, map[string]Strike, Summary) {
	state := &applier{today: today, enforce: enforce, edits: map[string]edit{}, strikes: maps.Clone(history)}
	live := map[string]bool{}
	for _, skill := range skills {
		live[skill.id] = true
		if skill.pin == nil {
			continue
		}
		state.summary.Pinned++
		if result, ok := decisions[skill.id]; ok {
			state.summary.Probed++
			state.applyOne(skill, result)
		}
	}
	state.settleQuarantine()
	maps.DeleteFunc(state.strikes, func(id string, _ Strike) bool { return !live[id] })
	return state.edits, state.strikes, state.summary
}

func (a *applier) applyOne(skill skillRecord, result decision) {
	switch result.action {
	case actionSkip:
		if result.reason == reasonUnreachable {
			a.summary.Unreachable = append(a.summary.Unreachable, skill.id)
		} else {
			a.summary.Skipped = append(a.summary.Skipped, skill.id)
		}
	case actionKeep, actionBump:
		a.resolve(skill, result)
	case actionMissing:
		a.miss(skill, result.reason)
	}
}

func (a *applier) resolve(skill skillRecord, result decision) {
	delete(a.strikes, skill.id)
	change := edit{ref: result.ref}
	if result.action == actionBump {
		a.summary.Bumped = append(a.summary.Bumped, skill.id)
	}
	if skill.quarantine != nil {
		change.clearQuarantine = true
		a.summary.Restored = append(a.summary.Restored, skill.id)
	}
	if change != (edit{}) {
		a.edits[skill.id] = change
	}
}

func (a *applier) miss(skill skillRecord, reason string) {
	if skill.quarantine != nil {
		if a.enforce {
			refreshed := *skill.quarantine
			refreshed.LastChecked = a.today
			a.edits[skill.id] = edit{quarantine: &refreshed}
		}
		return
	}
	strike := recordMiss(a.strikes[skill.id], reason, a.today)
	a.strikes[skill.id] = strike
	a.summary.Strikes = append(a.summary.Strikes, skill.id)
	if strike.Misses >= requiredMisses {
		a.candidates = append(a.candidates, quarantineCandidate{skill.id, strike.Reason, skill.pin.Commit})
	}
}

// settleQuarantine writes quarantine only when enforced and within the safety
// rail, so an upstream outage cannot empty a registry.
func (a *applier) settleQuarantine() {
	a.summary.RailTripped = len(a.candidates) > quarantineLimit(a.summary.Pinned)
	for _, candidate := range a.candidates {
		if a.summary.RailTripped || !a.enforce {
			a.summary.WouldQuarantine = append(a.summary.WouldQuarantine, candidate.id)
			continue
		}
		a.summary.Quarantined = append(a.summary.Quarantined, candidate.id)
		a.edits[candidate.id] = edit{quarantine: &Quarantine{Reason: candidate.reason, Since: a.today, LastSeenRef: candidate.commit, LastChecked: a.today}}
	}
}

func recordMiss(previous Strike, reason, today string) Strike {
	if previous.Misses == 0 || previous.Reason != reason {
		return Strike{Misses: 1, FirstMissAt: today, LastMissAt: today, Reason: reason}
	}
	if daysBetween(previous.LastMissAt, today) >= minStrikeGapDays {
		previous.Misses++
		previous.LastMissAt = today
	}
	return previous
}

func quarantineLimit(pinned int) int {
	return min(maxQuarantine, max(1, pinned*maxQuarantinePct/100))
}

// Markdown renders the summary for a terminal or a pull request body.
func (s Summary) Markdown() string {
	var text strings.Builder
	mode := "**report-only** (set repository variable `QUARANTINE=enforce` to write it)"
	if s.Enforced {
		mode = "**enforced**"
	}
	fmt.Fprintf(&text, "## Skill pins\n\nProbed %d of %d pinned skills. Quarantine is %s.\n\n", s.Probed, s.Pinned, mode)
	if s.TimedOut {
		text.WriteString("> The time limit was reached; remaining repositories wait for the next run.\n\n")
	}
	if s.RailTripped {
		fmt.Fprintf(&text, "> **Safety rail tripped:** %d skills would be newly quarantined, more than this registry's limit of %d. No quarantine was written. Check for an upstream outage before retrying.\n\n", len(s.WouldQuarantine), quarantineLimit(s.Pinned))
	}
	ready, would := "Would quarantine", s.WouldQuarantine
	if s.Enforced {
		ready = "Ready to quarantine"
	}
	if s.RailTripped {
		would = nil
	}
	sections := []struct {
		title string
		ids   []string
	}{
		{"Re-pinned", s.Bumped},
		{"Restored from quarantine", s.Restored},
		{"Quarantined", s.Quarantined},
		{ready, would},
		{"Misses recorded", s.Strikes},
		{"Unreachable (never read from this runner; private or removed)", s.Unreachable},
		{"Skipped (transient or ambiguous)", s.Skipped},
	}
	for _, section := range sections {
		if len(section.ids) == 0 {
			continue
		}
		fmt.Fprintf(&text, "### %s (%d)\n", section.title, len(section.ids))
		for _, id := range section.ids {
			text.WriteString("- `" + id + "`\n")
		}
		text.WriteString("\n")
	}
	if len(s.Bumped)+len(s.Restored)+len(s.Quarantined)+len(s.Strikes) == 0 {
		text.WriteString("No changes.\n")
	}
	return text.String()
}
