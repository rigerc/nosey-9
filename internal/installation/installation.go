// Package installation plans confirmed project installs and reconciles pinned upstream output.
package installation

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
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
	resolvedSHA   = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

// staleLockAge bounds how long a crashed run may block later project operations.
const staleLockAge = time.Hour

// Service coordinates source resolution, approval and project installation.
type Service struct {
	Store    settings.Store
	Settings settings.Settings
	Runner   process.Executor
}

// Item records one selected source and its stable logical origin.
type Item struct {
	SkillID       *string  `json:"skillId"`
	SkillName     string   `json:"skillName"`
	Source        string   `json:"source"`
	LogicalSource string   `json:"logicalSource"`
	RegistryID    *string  `json:"sourceRegistryId"`
	Ref           *string  `json:"ref"`
	Revision      *string  `json:"resolvedRevision"`
	RepositoryID  *string  `json:"repositoryId"`
	Provenance    string   `json:"provenance"`
	Agents        []string `json:"agents"`
	Method        string   `json:"method"`
	Status        string   `json:"status"`
}

// Command contains validated argv and aligned per-skill identities.
type Command struct {
	Argv          []string  `json:"argv"`
	Source        string    `json:"source"`
	LogicalSource string    `json:"logicalSource"`
	Ref           *string   `json:"ref"`
	Revision      *string   `json:"resolvedRevision"`
	SkillIDs      []*string `json:"skillIds"`
	SkillNames    []string  `json:"skillNames"`
	Agents        []string  `json:"agents"`
	Method        string    `json:"method"`
}

// Override explains a source/provenance change requiring review.
type Override struct {
	SkillID *string `json:"skillId"`
	Field   string  `json:"field"`
	From    *string `json:"fromRegistryId"`
	To      string  `json:"toRegistryId"`
	Reason  string  `json:"reason"`
}

// Conflict records a selected or installed relationship blocker.
type Conflict struct {
	SkillID *string `json:"skillId"`
	Other   string  `json:"conflictingSkillId"`
	Kind    string  `json:"kind"`
	Ignored bool    `json:"ignored"`
	Reason  string  `json:"reason"`
}

// Fingerprint binds an installation source to immutable content.
type Fingerprint struct {
	LogicalSource string  `json:"logicalSource"`
	RegistryID    *string `json:"sourceRegistryId"`
	Ref           *string `json:"requestedRef"`
	Revision      *string `json:"resolvedRevision"`
	Hash          string  `json:"contentSha256"`
}

// RegistryFingerprint binds review to a registry snapshot/content generation.
type RegistryFingerprint struct {
	ID       string  `json:"registryId"`
	Revision *string `json:"resolvedRevision"`
	Hash     string  `json:"contentSha256"`
}

// Approval carries review requirements and the canonical plan digest.
type Approval struct {
	Required         bool                  `json:"required"`
	Digest           *string               `json:"digest"`
	Canonicalization string                `json:"canonicalization"`
	Version          string                `json:"installerVersion"`
	Sources          []Fingerprint         `json:"sourceFingerprints"`
	Registries       []RegistryFingerprint `json:"registryFingerprints"`
}

// Plan contains the immutable reviewed project installation commands.
type Plan struct {
	Version       int                 `json:"outputVersion"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics"`
	Blocked       bool                `json:"blocked"`
	Workspace     string              `json:"workspace"`
	Agents        []string            `json:"agents"`
	Method        string              `json:"method"`
	Commands      []Command           `json:"commands"`
	Items         []Item              `json:"items"`
	Overrides     []Override          `json:"overrides"`
	Conflicts     []Conflict          `json:"conflicts"`
	Approval      Approval            `json:"approval"`
	trees         map[string]string
	registryRoots map[string]string
}

// Options selects catalog or direct-source installation targets.
type Options struct {
	IDs             []string
	Agents          []string
	Method          string
	Source          string
	SkillName       string
	IgnoreConflicts bool
}

func (s Service) env() []string {
	if s.Settings.Installation.Telemetry == "disabled" {
		return []string{"DISABLE_TELEMETRY=1", "DO_NOT_TRACK=1"}
	}
	return nil
}

func unique(v []string) []string { v = slices.Clone(v); slices.Sort(v); return slices.Compact(v) }

// CommandText escapes argv for display/copy; execution always uses tokens directly.
func CommandText(argv []string) string {
	parts := []string{}
	for _, arg := range argv {
		parts = append(parts, "\""+strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(arg)+"\"")
	}
	return strings.Join(parts, " ")
}

func (s Service) run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	runner := s.Runner
	if runner == nil {
		runner = process.Runner{}
	}
	return runner.Run(ctx, dir, env, name, args...)
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
