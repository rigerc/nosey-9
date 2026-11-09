// Package registry resolves data-only sources and compiles provenance-aware overlays.
package registry

import (
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/githubclient"
	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/settings"
)

// Service fetches sources and compiles effective registry entities.
type Service struct {
	Store settings.Store
	// Git retrieves registry snapshots; nil uses a Hardened client.
	Git *gitclient.Client
	// UserGit initializes and publishes the repository of Init; nil uses a
	// UserOrigin client.
	UserGit *gitclient.Client
	// GitHub configures the API client of Init. Only tests set BaseURL.
	GitHub githubclient.Options
}

// ValidateGit rejects unsupported URI/ref forms before argv construction.
func ValidateGit(uri, ref string) error { return gitpolicy.Validate(uri, ref) }
