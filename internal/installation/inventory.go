package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

// lockEntry mirrors the fields Nosey reads from a project-scoped skills-lock.json entry.
type lockEntry struct {
	Source    string `json:"source"`
	SourceURL string `json:"sourceUrl"`
	Type      string `json:"sourceType"`
	Ref       string `json:"ref"`
}

type projectLock struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

// LockError describes why skills-lock.json cannot be used; Reason is safe to show to users.
type LockError struct{ Reason string }

func (e LockError) Error() string { return "skills-lock.json " + e.Reason }

var errLockMissing = LockError{"is missing"}

func (s Service) readLock() (projectLock, error) {
	var lock projectLock
	data, err := storage.Read(s.Store.Workspace, "skills-lock.json", 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return lock, errLockMissing
	}
	if err != nil {
		return lock, LockError{"cannot be read: " + err.Error()}
	}
	if err = json.Unmarshal(data, &lock); err != nil {
		reason := "is not valid JSON"
		if strings.Contains(string(data), "<<<<<<<") {
			reason += " (unresolved merge conflict markers)"
		}
		return projectLock{}, LockError{reason}
	}
	if lock.Version != 1 {
		return projectLock{}, LockError{fmt.Sprintf("has unsupported version %d (expected 1)", lock.Version)}
	}
	if lock.Skills == nil {
		return projectLock{}, LockError{`has no "skills" map`}
	}
	return lock, nil
}

func normalizeSource(value, workspace string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return filepath.Clean(filepath.Join(workspace, value))
	}
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimSuffix(value, ".git")
	return strings.ToLower(strings.Split(value, "#")[0])
}

// sourceMatches compares an upstream-reported source (and optional original URL) with the wanted source.
func sourceMatches(workspace, wanted, source, sourceURL string) bool {
	want := normalizeSource(wanted, workspace)
	return normalizeSource(source, workspace) == want || sourceURL != "" && normalizeSource(sourceURL, workspace) == want
}

// catalogIdentity maps an installed skill to its catalog entry using the name and source recorded in the lock.
func (s Service) catalogIdentity(item upstreamItem, lock lockEntry, c domain.Catalog) (*string, *string, *string) {
	matches := []string{}
	logicalByID := map[string]string{}
	for id, skill := range c.Skills {
		if skill.Source.SkillName != item.Name {
			continue
		}
		logical := skill.Source.URI
		if skill.Source.Kind == "git" {
			if !sourceMatches(s.Store.Workspace, logical, item.Source, lock.SourceURL) {
				continue
			}
		} else {
			if !s.localSourceMatches(skill, lock, c) {
				continue
			}
			logical = skill.RegistryID + ":" + filepath.ToSlash(skill.Source.Path)
		}
		matches = append(matches, id)
		logicalByID[id] = logical
	}
	if len(matches) != 1 {
		return nil, nil, nil
	}
	id := matches[0]
	logical := logicalByID[id]
	var ref *string
	if c.Skills[id].Source.Ref != "" {
		value := c.Skills[id].Source.Ref
		ref = &value
	}
	return &id, &logical, ref
}

// localSourceMatches accepts the registry path itself or a copy from the install-source store older versions used.
func (s Service) localSourceMatches(skill domain.Skill, lock lockEntry, c domain.Catalog) bool {
	if lock.Type != "local" {
		return false
	}
	source := lock.Source
	if !filepath.IsAbs(source) {
		source = filepath.Join(s.Store.Workspace, source)
	}
	if filepath.Dir(source) == filepath.Join(s.Store.Data, "install-sources") {
		return true
	}
	root, ok := c.Roots[skill.RegistryID]
	return ok && filepath.Clean(source) == filepath.Join(root, skill.Source.Path)
}

// installedStatus classifies a listed skill from its lock entry and catalog pin.
func installedStatus(item upstreamItem, entry lockEntry, tracked bool, linkedAgents int, skill *domain.Skill) string {
	switch {
	case item.Path == "":
		return "missing"
	case !tracked || linkedAgents == 0:
		return "unknown"
	case skill != nil && skill.Source.Kind == "git" && fullSHARef.MatchString(skill.Source.Ref) && !strings.EqualFold(skill.Source.Ref, entry.Ref):
		return "outdated"
	}
	return "installed"
}

type upstreamItem struct {
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Scope  string   `json:"scope"`
	Agents []string `json:"agents"`
	Source string   `json:"source"`
}

func (s Service) read(ctx context.Context, cli CLI) ([]upstreamItem, error) {
	name, args := cli.argv("list", "--json")
	out, err := s.run(ctx, s.Store.Workspace, s.env(), name, args...)
	if err != nil {
		return nil, err
	}
	var items []upstreamItem
	err = json.Unmarshal(out.Stdout, &items)
	return items, err
}

// InventoryItem is a conservative project-scoped inventory wire record.
type InventoryItem struct {
	SkillID       *string  `json:"skillId"`
	Name          string   `json:"skillName"`
	LogicalSource *string  `json:"logicalSource"`
	Ref           *string  `json:"requestedRef"`
	Agents        []string `json:"agents"`
	Scope         string   `json:"scope"`
	Status        string   `json:"status"`
	Unmanaged     bool     `json:"unmanaged"`
	Path          *string  `json:"path"`
}

// Inventory is the versioned installed-state envelope.
type Inventory struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Workspace   string              `json:"workspace"`
	Complete    bool                `json:"complete"`
	Skills      []InventoryItem     `json:"skills"`
}

// Inventory reads installed project skills from the upstream list and lock file.
func (s Service) Inventory(ctx context.Context, c domain.Catalog) Inventory {
	result := Inventory{Version: 1, Diagnostics: []domain.Diagnostic{}, Workspace: s.Store.Workspace, Skills: []InventoryItem{}}
	var items []upstreamItem
	cli, err := s.resolveCLI(ctx)
	if err == nil {
		items, err = s.read(ctx, cli)
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: "Pinned upstream project inventory unavailable"})
		return result
	}
	locked, lockErr := s.readLock()
	result.Complete = lockErr == nil || len(items) == 0
	if lockErr != nil && len(items) > 0 {
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: "Project lock attribution unavailable: " + lockErr.Error() + "; fix or delete skills-lock.json"})
	}
	for _, item := range items {
		if item.Scope != "project" {
			continue
		}
		record := InventoryItem{Name: item.Name, Agents: []string{}, Scope: "project", Status: "unknown", Unmanaged: true}
		if item.Path != "" {
			record.Path = &item.Path
		}
		for id, name := range agents {
			if slices.Contains(item.Agents, name) {
				record.Agents = append(record.Agents, id)
			}
		}
		slices.Sort(record.Agents)
		if lockErr == nil {
			entry, tracked := locked.Skills[item.Name]
			record.SkillID, record.LogicalSource, record.Ref = s.catalogIdentity(item, entry, c)
			record.Unmanaged = record.SkillID == nil
			var skill *domain.Skill
			if record.SkillID != nil {
				match := c.Skills[*record.SkillID]
				skill = &match
			}
			record.Status = installedStatus(item, entry, tracked, len(record.Agents), skill)
		}
		result.Skills = append(result.Skills, record)
	}
	return result
}
