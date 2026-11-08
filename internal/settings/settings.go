// Package settings resolves schema-validated user/project JSON settings and trust.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

// OfficialURL identifies the external default registry; its payloads are never embedded.
const OfficialURL = "https://github.com/rigerc/nosey-registry"

// Adapter pins a user-approved executable detector implementation.
type Adapter struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Hash    string `json:"sha256"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// Settings contains resolved product settings, separate from UI theme defaults.
type Settings struct {
	Version    int                  `json:"schemaVersion"`
	Registries []domain.RegistryRef `json:"registries"`
	Updates    struct {
		TTL int `json:"ttlHours"`
	} `json:"updates"`
	Installation struct {
		Agents    []string `json:"agents"`
		Method    string   `json:"method"`
		Mode      string   `json:"defaultMode"`
		Verbose   bool     `json:"verbose"`
		Version   string   `json:"cliVersion"`
		Telemetry string   `json:"upstreamTelemetry"`
	} `json:"installation"`
	Detection struct {
		Exclude  []string  `json:"exclude"`
		MaxBytes int64     `json:"maxFileBytes"`
		MCP      bool      `json:"enableMcpPackage"`
		Adapters []Adapter `json:"adapters"`
	} `json:"detection"`
}

// Store owns the exact workspace and user configuration/cache/data paths.
type Store struct{ Workspace, UserPath, Cache, Data string }

// Resolved includes effective settings and unapproved workspace proposals.
type Resolved struct {
	WorkspaceIDs map[string]bool
	Settings     Settings
	Pending      []domain.RegistryRef
	Fingerprint  string
	Diagnostics  []domain.Diagnostic
}
type document map[string]json.RawMessage

// New canonicalizes the exact root and resolves user-owned storage paths.
func New(workspace, userPath string) (Store, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return Store{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Store{}, err
	}
	if storage.Safe(root) != root || strings.ContainsAny(root, "  ") {
		return Store{}, fmt.Errorf("workspace path contains unsafe controls")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Store{}, fmt.Errorf("workspace must be a directory: %s", root)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return Store{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Store{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	if userPath == "" {
		userPath = filepath.Join(config, "nosey", "config.json")
	}
	return Store{root, userPath, filepath.Join(cache, "nosey"), filepath.Join(data, "nosey")}, nil
}

// Defaults provides the schema-valid initial product configuration.
func Defaults() Settings {
	var s Settings
	s.Version = 1
	s.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "git", URI: OfficialURL}, Enabled: true, Priority: 0}}
	s.Updates.TTL = 24
	s.Installation.Agents = []string{}
	s.Installation.Method = "symlink"
	s.Installation.Mode = "execute"
	s.Installation.Version = "1.7.6"
	s.Installation.Telemetry = "inherit"
	s.Detection.Exclude = []string{"**/node_modules/**", "**/vendor/**", "**/.git/**", "**/dist/**", "**/build/**"}
	s.Detection.MaxBytes = 1 << 20
	s.Detection.Adapters = []Adapter{}
	return s
}
func load(path string) (document, error) {
	// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
	data, err := os.ReadFile(path)
	return decodeSettings(data, err, path)
}
func loadProject(workspace string) (document, error) {
	data, err := storage.Read(workspace, ".nosey/config.json", 1<<20)
	return decodeSettings(data, err, ".nosey/config.json")
}
func decodeSettings(data []byte, err error, path string) (document, error) {
	if errors.Is(err, os.ErrNotExist) {
		return document{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err = contracts.Validate("settings.schema.json", data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var doc document
	err = json.Unmarshal(data, &doc)
	return doc, err
}

// Resolve layers user/project settings while gating workspace registries on trust.
func (s Store) Resolve() (Resolved, error) {
	result := Resolved{WorkspaceIDs: map[string]bool{}, Pending: []domain.RegistryRef{}, Diagnostics: []domain.Diagnostic{}}
	baseData, _ := json.Marshal(Defaults())
	var base document
	if err := json.Unmarshal(baseData, &base); err != nil {
		return result, err
	}
	user, err := load(s.UserPath)
	if err != nil {
		return result, err
	}
	project, err := loadProject(s.Workspace)
	if err != nil {
		return result, err
	}
	if raw, ok := project["detection"]; ok {
		var d document
		if err = json.Unmarshal(raw, &d); err != nil {
			return result, err
		}
		if _, ok = d["adapters"]; ok {
			return result, fmt.Errorf("project configuration cannot configure detector adapters")
		}
	}
	for _, layer := range []struct {
		doc     document
		project bool
	}{{user, false}, {project, true}} {
		if raw, ok := layer.doc["registries"]; ok {
			refs, parseErr := normalizeRefs(raw, layer.project, s.Workspace)
			if parseErr != nil {
				return result, parseErr
			}
			var supplied []document
			if err = json.Unmarshal(raw, &supplied); err != nil {
				return result, err
			}
			var inherited []domain.RegistryRef
			if err = json.Unmarshal(base["registries"], &inherited); err != nil {
				return result, err
			}
			for i := range refs {
				index := slices.IndexFunc(inherited, func(r domain.RegistryRef) bool { return r.ID == refs[i].ID })
				if index < 0 {
					continue
				}
				if _, ok := supplied[i]["enabled"]; !ok {
					refs[i].Enabled = inherited[index].Enabled
				}
				if _, ok := supplied[i]["priority"]; !ok {
					refs[i].Priority = inherited[index].Priority
				}
				if _, ok := supplied[i]["ttlHours"]; !ok {
					refs[i].TTLHours = inherited[index].TTLHours
				}
			}
			if layer.project {
				for _, ref := range refs {
					result.WorkspaceIDs[ref.ID] = true
				}
				fpData, _ := json.Marshal(struct {
					Root string               `json:"workspace"`
					Refs []domain.RegistryRef `json:"registries"`
				}{s.Workspace, refs})
				result.Fingerprint = storage.Hash(fpData)
				approved, trustErr := s.Approved(result.Fingerprint)
				if trustErr != nil {
					return result, trustErr
				}
				if !approved {
					result.Pending = refs
					delete(layer.doc, "registries")
					result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_UNTRUSTED_PROJECT_REGISTRY", Severity: "warning", Message: "Workspace registries await explicit trust approval", Subject: result.Fingerprint})
				} else {
					data, _ := json.Marshal(refs)
					layer.doc["registries"] = data
				}
			} else {
				data, _ := json.Marshal(refs)
				layer.doc["registries"] = data
			}
		}
		if err = merge(base, layer.doc); err != nil {
			return result, err
		}
	}
	bytes, err := json.Marshal(base)
	if err != nil {
		return result, err
	}
	if err = contracts.Validate("settings.schema.json", bytes); err != nil {
		return result, err
	}
	err = json.Unmarshal(bytes, &result.Settings)
	if err != nil {
		return result, err
	}
	for _, a := range result.Settings.Detection.Adapters {
		if !a.Enabled {
			continue
		}
		relative, relErr := filepath.Rel(filepath.Join(s.Data, "adapters"), a.Path)
		if relErr != nil || !filepath.IsAbs(a.Path) || !filepath.IsLocal(relative) {
			return result, fmt.Errorf("trusted adapters must be installed inside %s", filepath.Join(s.Data, "adapters"))
		}
	}
	return result, err
}
func normalizeRefs(data []byte, project bool, root string) ([]domain.RegistryRef, error) {
	var raw []document
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	refs := []domain.RegistryRef{}
	seen := map[string]bool{}
	for _, v := range raw {
		ref := domain.RegistryRef{Enabled: true, Priority: 20}
		if project {
			ref.Priority = 30
		}
		b, _ := json.Marshal(v)
		if err := json.Unmarshal(b, &ref); err != nil {
			return nil, err
		}
		if seen[ref.ID] {
			return nil, fmt.Errorf("duplicate registry ID %s", ref.ID)
		}
		seen[ref.ID] = true
		if ref.Source.Kind == "local" {
			p := ref.Source.Path
			if strings.HasPrefix(p, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return nil, err
				}
				p = filepath.Join(home, p[2:])
			}
			if !filepath.IsAbs(p) {
				if !project {
					return nil, fmt.Errorf("user registry paths must be absolute or ~/ prefixed")
				}
				p = filepath.Join(root, p)
			}
			if storage.Safe(p) != p || strings.ContainsAny(p, "  ") {
				return nil, fmt.Errorf("registry path contains unsafe controls")
			}
			ref.Source.Path = filepath.Clean(p)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
func merge(base, layer document) error {
	for key, value := range layer {
		if key == "registries" {
			var old, incoming []domain.RegistryRef
			if err := json.Unmarshal(base[key], &old); err != nil {
				return err
			}
			if err := json.Unmarshal(value, &incoming); err != nil {
				return err
			}
			for _, ref := range incoming {
				idx := slices.IndexFunc(old, func(r domain.RegistryRef) bool { return r.ID == ref.ID })
				if idx < 0 {
					old = append(old, ref)
				} else {
					old[idx] = ref
				}
			}
			data, _ := json.Marshal(old)
			base[key] = data
			continue
		}
		if key == "installation" || key == "detection" || key == "updates" {
			var a, b document
			if err := json.Unmarshal(base[key], &a); err != nil {
				return err
			}
			if err := json.Unmarshal(value, &b); err != nil {
				return err
			}
			for k, v := range b {
				a[k] = v
			}
			data, _ := json.Marshal(a)
			base[key] = data
		} else {
			base[key] = value
		}
	}
	return nil
}

// Approved checks the current workspace fingerprint in the user-owned trust store.
func (s Store) Approved(fingerprint string) (bool, error) {
	var trust map[string]string
	// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.UserPath), "trust.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = json.Unmarshal(data, &trust); err != nil {
		return false, err
	}
	return trust[s.Workspace] == fingerprint, nil
}

// Trust explicitly grants or revokes approval for this workspace fingerprint.
func (s Store) Trust(fingerprint string, revoke bool) error {
	if !revoke {
		resolved, err := s.Resolve()
		if err != nil {
			return err
		}
		if fingerprint == "" || fingerprint != resolved.Fingerprint {
			return fmt.Errorf("workspace registry fingerprint changed; review the current proposals")
		}
	}
	path := filepath.Join(filepath.Dir(s.UserPath), "trust.json")
	trust := map[string]string{}
	// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &trust); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if revoke {
		delete(trust, s.Workspace)
	} else {
		if fingerprint == "" {
			return fmt.Errorf("no workspace registries to approve")
		}
		trust[s.Workspace] = fingerprint
	}
	return storage.JSON(path, trust)
}

// Set validates and atomically updates one chosen settings layer.
func (s Store) Set(key string, value []byte, project bool) error {
	path := s.UserPath
	if project {
		path = filepath.Join(s.Workspace, ".nosey", "config.json")
	}
	var doc document
	var err error
	if project {
		doc, err = loadProject(s.Workspace)
	} else {
		doc, err = load(path)
	}
	if err != nil {
		return err
	}
	doc["schemaVersion"] = json.RawMessage("1")
	if !json.Valid(value) {
		return fmt.Errorf("value must be JSON")
	}
	parts := strings.Split(key, ".")
	if len(parts) > 2 || parts[0] == "schemaVersion" {
		return fmt.Errorf("invalid settings key")
	}
	if len(parts) == 1 {
		doc[key] = value
	} else {
		nested := document{}
		if raw, ok := doc[parts[0]]; ok {
			if err = json.Unmarshal(raw, &nested); err != nil {
				return err
			}
		}
		nested[parts[1]] = value
		data, _ := json.Marshal(nested)
		doc[parts[0]] = data
	}
	if project && key == "detection.adapters" {
		return fmt.Errorf("project cannot activate adapters")
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err = contracts.Validate("settings.schema.json", data); err != nil {
		return err
	}
	if project {
		return storage.WriteAt(s.Workspace, ".nosey/config.json", append(data, '\n'))
	}
	return storage.Write(path, append(data, '\n'))
}

// Registries writes only the explicitly supplied user registry entries.
func (s Store) Registries(refs []domain.RegistryRef) error {
	data, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	return s.Set("registries", data, false)
}

// UserRegistries returns only explicitly user-owned references, never project proposals.
func (s Store) UserRegistries() ([]domain.RegistryRef, error) {
	doc, err := load(s.UserPath)
	if err != nil {
		return nil, err
	}
	raw, ok := doc["registries"]
	if !ok {
		return []domain.RegistryRef{}, nil
	}
	return normalizeRefs(raw, false, s.Workspace)
}
