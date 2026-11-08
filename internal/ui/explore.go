package ui

import (
	"fmt"
	"strings"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (m noseyModel) overviewRegistries() []domain.RegistryRef {
	refs := append([]domain.RegistryRef{}, m.session.Resolved.Settings.Registries...)
	seen := map[string]bool{}
	for _, ref := range refs {
		seen[ref.ID] = true
	}
	for _, ref := range m.session.Resolved.Pending {
		if !seen[ref.ID] {
			refs = append(refs, ref)
			seen[ref.ID] = true
		}
	}
	return refs
}
func (m noseyModel) registryOverview() []noseyRow {
	rows := []noseyRow{{kind: "heading", id: "registries", label: "Registries"}}
	skills := map[string]int{}
	repos := map[string]map[string]bool{}
	for _, skill := range m.session.Catalog.Skills {
		skills[skill.RegistryID]++
		if repos[skill.RegistryID] == nil {
			repos[skill.RegistryID] = map[string]bool{}
		}
		if skill.RepositoryID != "" {
			repos[skill.RegistryID][skill.RepositoryID] = true
		}
	}
	pending := map[string]bool{}
	for _, ref := range m.session.Resolved.Pending {
		pending[ref.ID] = true
	}
	for _, ref := range m.overviewRegistries() {
		state := "unavailable"
		if _, ok := m.session.Catalog.Roots[ref.ID]; ok {
			state = "loaded"
		}
		if pending[ref.ID] {
			state = "awaiting approval"
		}
		if !ref.Enabled {
			state = "disabled"
		}
		label := fmt.Sprintf("%s  [%s]  ·  %d skills · %d repositories", ref.ID, state, skills[ref.ID], len(repos[ref.ID]))
		rows = append(rows, noseyRow{kind: "registry-overview", id: ref.ID, label: label})
		location := ref.Source.URI
		if ref.Source.Kind == "local" {
			location = ref.Source.Path
		}
		rows = append(rows, noseyRow{kind: "notice", label: "  " + location})
	}
	if len(rows) == 1 {
		rows = append(rows, noseyRow{kind: "notice", label: "No registries configured. Add one in Settings."})
	}
	return rows
}
func (m noseyModel) registryOverviewDetails(id string) string {
	for _, ref := range m.overviewRegistries() {
		if ref.ID != id {
			continue
		}
		location := ref.Source.URI
		if ref.Source.Kind == "local" {
			location = ref.Source.Path
		}
		lines := []string{id, "Source: " + storage.Safe(location), fmt.Sprintf("Enabled: %t", ref.Enabled), fmt.Sprintf("Priority: %d", ref.Priority)}
		if ref.Source.Ref != "" {
			lines = append(lines, "Requested ref: "+storage.Safe(ref.Source.Ref))
		}
		if revision := m.session.Catalog.Revisions[id]; revision != nil {
			lines = append(lines, "Resolved revision: "+*revision)
		}
		return strings.Join(lines, "\n")
	}
	return id
}
