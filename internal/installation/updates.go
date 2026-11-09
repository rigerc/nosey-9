package installation

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/storage"
)

// The upstream `skills check` command has no machine-readable output, so its report lines are parsed.
var (
	updateLine      = regexp.MustCompile(`^↑ (.+) has an update$`)
	outdatedSummary = regexp.MustCompile(`^\d+ skill\(s\) can be updated\b`)
	failedSummary   = regexp.MustCompile(`^Failed to update (\d+) skill\(s\)$`)
	upToDateLine    = regexp.MustCompile(`^(\d+) project skill\(s\) already up to date$`)
	legacyLine      = regexp.MustCompile(`^(\d+) project skill\(s\) cannot be updated automatically\b`)
)

// UpdateCheck is what the upstream CLI could establish about installed project skills.
type UpdateCheck struct {
	// Names lists skills with newer upstream content.
	Names []string
	// Failures holds upstream's detail for each source it could not check.
	Failures []string
	// Failed counts skills upstream could not check.
	Failed int
	// UpToDate counts skills upstream confirmed as current.
	UpToDate int
	// AllCurrent is true only when upstream confirmed every checkable skill as current.
	AllCurrent bool
	// Legacy counts skills whose lock entries predate upstream's update tracking.
	Legacy int
	// NothingToCompare is true when upstream had no skill it could compare with a source.
	NothingToCompare bool
	// finished records that upstream reached the closing summary of a run that exits non-zero.
	finished bool
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
		return "upstream reported " + skillCount(c.Failed) + " failed"
	}
	return ""
}

func skillCount(count int) string {
	if count == 1 {
		return "1 skill"
	}
	return fmt.Sprintf("%d skills", count)
}

// CheckUpdates asks the pinned upstream CLI which installed project skills have newer upstream content.
// A failed check is an error, never "no updates"; a partly failed one reports the gap through Warning.
func (s Service) CheckUpdates(ctx context.Context) (UpdateCheck, error) {
	cli, err := s.resolveCLI(ctx)
	if err != nil {
		return UpdateCheck{}, err
	}
	if runner, ok := s.Runner.(process.Runner); ok || s.Runner == nil {
		if runner.Timeout == 0 {
			runner.Timeout = 3 * time.Minute
		}
		s.Runner = runner
	}
	name, args := cli.argv("check", "--project")
	out, err := s.run(ctx, s.Store.Workspace, s.env(), name, args...)
	if ctx.Err() != nil {
		return UpdateCheck{}, ctx.Err()
	}
	check := parseUpdates(string(out.Stdout))
	// Upstream also exits 1 when updates exist, so a non-zero exit is only a usable result
	// once it has printed its closing summary and named at least one update. Anything else,
	// such as a killed process with partial output, is a failed check.
	if err != nil && (!check.finished || len(check.Names) == 0) {
		return UpdateCheck{}, updateCheckError(err, check, out)
	}
	return check, nil
}

func updateCheckError(err error, check UpdateCheck, out process.Output) error {
	detail := check.failureDetail()
	if detail == "" {
		detail = meaningfulStderr(string(out.Stderr))
	}
	if detail == "" {
		detail = lastLine(ansi.Strip(string(out.Stdout)))
	}
	detail = storage.Safe(detail)
	if detail == "" {
		return fmt.Errorf("skills check failed: %w", err)
	}
	return fmt.Errorf("skills check failed: %w: %s", err, detail)
}

// meaningfulStderr drops npm's own chatter, which `npx --yes` prints even when the real failure is elsewhere.
func meaningfulStderr(stderr string) string {
	kept := []string{}
	for _, line := range strings.Split(ansi.Strip(stderr), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line != "" && !strings.HasPrefix(lower, "npm warn") && !strings.HasPrefix(lower, "npm notice") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, " ")
}

func parseUpdates(stdout string) UpdateCheck {
	check := UpdateCheck{Names: []string{}}
	for _, raw := range strings.Split(ansi.Strip(stdout), "\n") {
		line := strings.TrimSpace(raw)
		if match := updateLine.FindStringSubmatch(line); match != nil {
			// Names are only displayed and compared with the inventory, so upstream's raw name stands.
			if name := storage.Safe(match[1]); name != "" {
				check.Names = append(check.Names, name)
			}
			continue
		}
		if detail, ok := strings.CutPrefix(line, "✗ "); ok {
			check.Failures = append(check.Failures, storage.Safe(detail))
			continue
		}
		switch {
		case outdatedSummary.MatchString(line):
			check.finished = true
		case failedSummary.MatchString(line):
			check.finished = true
			check.Failed = reportedCount(failedSummary, line)
		case upToDateLine.MatchString(line):
			check.UpToDate = reportedCount(upToDateLine, line)
		case legacyLine.MatchString(line):
			check.Legacy = reportedCount(legacyLine, line)
		case line == "✓ All project skills are up to date":
			check.AllCurrent = true
		case line == "No project skills to update." || line == "No project skills can be updated in place.":
			check.NothingToCompare = true
		}
	}
	slices.Sort(check.Names)
	check.Names = slices.Compact(check.Names)
	return check
}

func reportedCount(pattern *regexp.Regexp, line string) int {
	count, err := strconv.Atoi(pattern.FindStringSubmatch(line)[1])
	if err != nil {
		return 0
	}
	return count
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
