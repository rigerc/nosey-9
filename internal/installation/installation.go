// Package installation plans catalog installs, runs them through the native skillkit engine
// and reads project skill state.
package installation

import (
	"context"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

// PolicyError marks a blocked operation that must never change the project.
type PolicyError struct{ Reason string }

func (e PolicyError) Error() string { return e.Reason }

// PrerequisiteError marks a missing or incompatible prerequisite, such as an unsupported agent.
type PrerequisiteError struct{ Reason string }

func (e PrerequisiteError) Error() string { return e.Reason }

var (
	safeSkillName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	fullSHARef    = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
)

// SourceFetcher fetches one source into a temporary checkout. skillkit.Fetcher implements it.
type SourceFetcher interface {
	Fetch(ctx context.Context, src skillkit.Source) (*skillkit.Checkout, error)
}

// Service plans project installs, runs them and reads their state.
type Service struct {
	Store    settings.Store
	Settings settings.Settings
	// Credentials feeds the doctor diagnostics; nil uses gitclient.SharedCredentials.
	Credentials *gitclient.Credentials
	// Fetcher fetches git sources; nil uses a skillkit.Fetcher with its own timeouts.
	Fetcher SourceFetcher
	// LockTimeout bounds waiting for another Nosey operation in the same workspace; zero uses ten seconds.
	LockTimeout time.Duration
	// Home overrides the user's home directory for agent detection; empty uses os.UserHomeDir.
	Home string
}

// Options selects catalog skills to install.
type Options struct {
	IDs             []string
	Agents          []string
	Method          string
	IgnoreConflicts bool
	// Yes marks a non-interactive install; it requires explicit agents.
	Yes bool
}

func unique(v []string) []string { v = slices.Clone(v); slices.Sort(v); return slices.Compact(v) }

func (s Service) fetcher() SourceFetcher {
	if s.Fetcher != nil {
		return s.Fetcher
	}
	return skillkit.Fetcher{}
}

func (s Service) home() string {
	if s.Home != "" {
		return s.Home
	}
	home, _ := os.UserHomeDir()
	return home
}
