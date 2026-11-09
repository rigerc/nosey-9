// Package installation builds upstream Skills CLI commands and reads project skill state.
package installation

import (
	"context"
	"regexp"
	"slices"

	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
)

// Version pins the tested upstream parser and JSON inventory format.
const Version = "1.7.6"

// PolicyError marks a blocked operation that must never invoke the installer.
type PolicyError struct{ Reason string }

func (e PolicyError) Error() string { return e.Reason }

// PrerequisiteError marks a missing or incompatible installer prerequisite.
type PrerequisiteError struct{ Reason string }

func (e PrerequisiteError) Error() string { return e.Reason }

var (
	safeSkillName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	fullSHARef    = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
)

// Service builds project installs and reads their state through the upstream CLI.
type Service struct {
	Store    settings.Store
	Settings settings.Settings
	Runner   process.Executor
	// Resolver overrides CLI discovery; nil uses an installed or npx-provided CLI.
	Resolver func(context.Context) (CLI, error)
	// Attach overrides how interactive installs reach the terminal; nil uses process.RunAttached.
	Attach func(ctx context.Context, dir string, env []string, name string, args ...string) error
}

// Options selects catalog skills to install.
type Options struct {
	IDs             []string
	Agents          []string
	Method          string
	IgnoreConflicts bool
	// Yes makes the installer run without prompts; it requires explicit agents.
	Yes bool
}

func (s Service) env() []string {
	if s.Settings.Installation.Telemetry == "disabled" {
		return []string{"DISABLE_TELEMETRY=1", "DO_NOT_TRACK=1"}
	}
	return nil
}

func unique(v []string) []string { v = slices.Clone(v); slices.Sort(v); return slices.Compact(v) }

func (s Service) run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	runner := s.Runner
	if runner == nil {
		runner = process.Runner{}
	}
	return runner.Run(ctx, dir, env, name, args...)
}
