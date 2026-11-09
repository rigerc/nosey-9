package installation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

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
func (s Service) catalogIdentity(name string, lock skillkit.LockEntry, c domain.Catalog) (*string, *string, *string) {
	matches := []string{}
	logicalByID := map[string]string{}
	for id, skill := range c.Skills {
		if skill.Source.SkillName != name {
			continue
		}
		logical := skill.Source.URI
		if skill.Source.Kind == "git" {
			if !sourceMatches(s.Store.Workspace, logical, lock.Source, lock.SourceURL) {
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
func (s Service) localSourceMatches(skill domain.Skill, lock skillkit.LockEntry, c domain.Catalog) bool {
	if lock.SourceType != "local" {
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
func installedStatus(path string, entry skillkit.LockEntry, tracked bool, linkedAgents int, skill *domain.Skill) string {
	switch {
	case path == "":
		return "missing"
	case !tracked || linkedAgents == 0:
		return "unknown"
	case pinChanged(entry, skill):
		return "outdated"
	}
	return "installed"
}

// pinChanged reports that the catalog pins a full SHA different from the one the lock recorded.
func pinChanged(entry skillkit.LockEntry, skill *domain.Skill) bool {
	return skill != nil && skill.Source.Kind == "git" && fullSHARef.MatchString(skill.Source.Ref) && !strings.EqualFold(skill.Source.Ref, entry.Ref)
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

	// tracked holds the names recorded in skills-lock.json; it is not part of the wire format.
	tracked map[string]bool
}

// Tracked reports whether the installed skill is recorded in skills-lock.json.
func (i Inventory) Tracked(name string) bool { return i.tracked[name] }

// Inventory reads installed project skills from the agent skill directories and skills-lock.json.
func (s Service) Inventory(ctx context.Context, c domain.Catalog) Inventory {
	result := Inventory{Version: 1, Diagnostics: []domain.Diagnostic{}, Workspace: s.Store.Workspace, Skills: []InventoryItem{}}
	unknown := func(message string) {
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: message})
	}
	if err := ctx.Err(); err != nil {
		unknown("Project skill inventory unavailable: " + err.Error())
		return result
	}
	table, err := s.agentTable()
	var listed skillkit.Inventory
	if err == nil {
		listed, err = skillkit.ListInstalled(skillkit.Target{Workspace: s.Store.Workspace, Agents: table})
	}
	if err != nil {
		unknown("Project skill inventory unavailable: " + err.Error())
		return result
	}
	locked, lockErr := skillkit.ReadLock(s.Store.Workspace)
	result.Complete = (lockErr == nil || len(listed.Skills) == 0) && len(listed.Warnings) == 0
	if lockErr != nil && len(listed.Skills) > 0 {
		unknown("Project lock attribution unavailable: " + lockErr.Error() + "; fix or delete skills-lock.json")
	}
	if len(listed.Warnings) > 0 {
		message := "Project skill inventory is incomplete: " + listed.Warnings[0].Message
		if more := len(listed.Warnings) - 1; more > 0 {
			message += fmt.Sprintf(" (and %d more)", more)
		}
		unknown(message)
	}
	for _, item := range listed.Skills {
		path := filepath.Join(s.Store.Workspace, filepath.FromSlash(item.Path))
		record := InventoryItem{Name: item.Name, Agents: append([]string{}, item.Agents...), Scope: "project", Status: "unknown", Unmanaged: true, Path: &path}
		if lockErr == nil {
			entry, tracked := locked.Skills[item.Name]
			if tracked {
				if result.tracked == nil {
					result.tracked = map[string]bool{}
				}
				result.tracked[item.Name] = true
			}
			record.SkillID, record.LogicalSource, record.Ref = s.catalogIdentity(item.Name, entry, c)
			record.Unmanaged = record.SkillID == nil
			var skill *domain.Skill
			if record.SkillID != nil {
				match := c.Skills[*record.SkillID]
				skill = &match
			}
			record.Status = installedStatus(path, entry, tracked, len(record.Agents), skill)
		}
		result.Skills = append(result.Skills, record)
	}
	return result
}
