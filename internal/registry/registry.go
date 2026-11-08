// Package registry resolves data-only sources and compiles provenance-aware overlays.
package registry

import (
	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
)

// Service fetches sources and compiles effective registry entities.
type Service struct {
	Store  settings.Store
	Runner process.Executor
}

// ValidateGit rejects unsupported URI/ref forms before argv construction.
func ValidateGit(uri, ref string) error { return gitpolicy.Validate(uri, ref) }
