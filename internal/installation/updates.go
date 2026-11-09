package installation

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/storage"
)

// updateLine matches the upstream `skills check` report line; that command has no machine-readable output.
var updateLine = regexp.MustCompile(`^↑ (\S+) has an update$`)

// CheckUpdates asks the pinned upstream CLI which installed project skills have newer upstream content.
// A failed check is an error, never "no updates".
func (s Service) CheckUpdates(ctx context.Context) ([]string, error) {
	cli, err := s.resolveCLI(ctx)
	if err != nil {
		return nil, err
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
		return nil, ctx.Err()
	}
	names := parseUpdates(string(out.Stdout))
	// Upstream exits 1 both when updates exist and when a source could not be checked.
	if err != nil && len(names) == 0 {
		detail := strings.TrimSpace(ansi.Strip(string(out.Stderr)))
		if detail == "" {
			detail = lastLine(ansi.Strip(string(out.Stdout)))
		}
		detail = storage.Safe(detail)
		if detail == "" {
			return nil, fmt.Errorf("skills check failed: %w", err)
		}
		return nil, fmt.Errorf("skills check failed: %w: %s", err, detail)
	}
	return names, nil
}

func parseUpdates(stdout string) []string {
	names := []string{}
	for _, line := range strings.Split(ansi.Strip(stdout), "\n") {
		if match := updateLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil && safeSkillName.MatchString(match[1]) {
			names = append(names, match[1])
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
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
