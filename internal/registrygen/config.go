// Package registrygen canonicalizes registry data and produces the published
// manifest and lock file from a single in-repo data set.
package registrygen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	// ConfigName is the authoring configuration file at a registry root.
	ConfigName = "nosey.registry.config.json"
	// LockName is the generated lock file at a registry root.
	LockName = "nosey.registry.lock.json"
	// Generator is the lock format version, not a build version.
	Generator = "native-v1/1"
)

var (
	idPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	templateIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

var dataRoles = []string{"technologies", "skills", "mappings", "repositories"}
var requiredRoles = map[string]bool{"technologies": true, "skills": true, "mappings": true}

// Registry is the published identity of a registry.
type Registry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Upstream declares where refresh downloads one data file from.
type Upstream struct {
	URL  string `json:"url"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path"`
}

// Source records the one-time origin of seeded data. It is informational.
type Source struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
}

// Config is the versioned registry generation configuration.
type Config struct {
	SchemaVersion    int                               `json:"schemaVersion"`
	Registry         Registry                          `json:"registry"`
	Manifest         string                            `json:"manifest"`
	Files            map[string]string                 `json:"files"`
	Upstreams        map[string]Upstream               `json:"upstreams,omitempty"`
	Templates        []domain.Template                 `json:"templates,omitempty"`
	RepositoryScores map[string]domain.RepositoryScore `json:"repositoryScores,omitempty"`
	Source           *Source                           `json:"source,omitempty"`
}

// Load reads and validates the configuration at root.
func Load(root string) (Config, error) {
	var cfg Config
	data, err := storage.Read(root, ConfigName, 1<<20)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", ConfigName, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, inputError("%s: %w", ConfigName, err)
	}
	if err = cfg.Validate(); err != nil {
		return cfg, inputError("%s: %w", ConfigName, err)
	}
	return cfg, nil
}

// Validate checks path, identity and upstream rules before any file is read.
func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported config schemaVersion %d", c.SchemaVersion)
	}
	if !idPattern.MatchString(c.Registry.ID) {
		return fmt.Errorf("invalid registry id %q", c.Registry.ID)
	}
	if strings.TrimSpace(c.Registry.Name) == "" {
		return fmt.Errorf("registry name is required")
	}
	if !localPath(c.Manifest) {
		return fmt.Errorf("unsafe manifest path %q", c.Manifest)
	}
	if c.Manifest == ConfigName || c.Manifest == LockName {
		return fmt.Errorf("manifest path %q collides with config or lock", c.Manifest)
	}
	if len(c.Files) == 0 {
		return fmt.Errorf("registry declares no data files")
	}
	seen := map[string]bool{}
	for role, path := range c.Files {
		if !slices.Contains(dataRoles, role) {
			return fmt.Errorf("unknown data role %q", role)
		}
		if !localPath(path) {
			return fmt.Errorf("unsafe %s path %q", role, path)
		}
		if seen[path] {
			return fmt.Errorf("duplicate data path %q", path)
		}
		seen[path] = true
		if path == c.Manifest || path == ConfigName || path == LockName {
			return fmt.Errorf("data path %q collides with generated or config file", path)
		}
	}
	for _, role := range dataRoles {
		if requiredRoles[role] && c.Files[role] == "" {
			return fmt.Errorf("missing required %s file", role)
		}
	}
	for role, upstream := range c.Upstreams {
		if c.Files[role] == "" {
			return fmt.Errorf("upstream %s does not match a declared file", role)
		}
		if err := gitpolicy.Validate(upstream.URL, upstream.Ref); err != nil {
			return fmt.Errorf("upstream %s: %w", role, err)
		}
		if !localPath(upstream.Path) || strings.Contains(upstream.Path, ":") {
			return fmt.Errorf("unsafe upstream path %q", upstream.Path)
		}
	}
	for _, template := range c.Templates {
		if !templateIDPattern.MatchString(template.ID) {
			return fmt.Errorf("invalid template id %q", template.ID)
		}
		if template.Kind != "registry" {
			return fmt.Errorf("template %s has unsupported kind %q", template.ID, template.Kind)
		}
		if !localPath(template.Descriptor) {
			return fmt.Errorf("unsafe template descriptor %q", template.Descriptor)
		}
	}
	return nil
}

func localPath(path string) bool {
	return path != "" && filepath.IsLocal(path) && !strings.Contains(path, "\\") && !strings.HasPrefix(path, "-")
}
