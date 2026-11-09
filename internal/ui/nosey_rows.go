package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (m noseyModel) activeFilter() int {
	if m.tab == 1 {
		return 1
	}
	return 0
}

// technologyNames lists detected technologies for display only; they are never focusable rows.
// View calls it every frame, so it reads the scan directly rather than re-projecting the catalog.
func (m noseyModel) technologyNames() []string {
	names := make([]string, 0, len(m.scan.Technologies))
	for _, t := range m.scan.Technologies {
		names = append(names, storage.Safe(m.session.Catalog.Technologies[t.ID].Name))
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return names
}

// isCollapsed reports a group's state: an explicit toggle wins, otherwise repositories follow the setting.
func (m noseyModel) isCollapsed(key string) bool {
	if closed, ok := m.collapsed[key]; ok {
		return closed
	}
	return m.session.Resolved.Settings.UI.CollapseRepos && strings.HasPrefix(key, "repo:")
}
func (m noseyModel) rows() []noseyRow {
	if m.rowCache != nil {
		return m.rowCache
	}
	rows := []noseyRow{}
	switch m.tab {
	case 0:
		skillView := catalog.Project(m.session.Catalog, m.scan, m.filters[0], domain.Sort{Technologies: "name-asc", Skills: m.sort[0]})
		rows = append(rows, noseyRow{"heading", "skills", fmt.Sprintf("Recommended skills  %d/%d", len(skillView.Skills), m.scan.SkillTotal)})
		rows = append(rows, resultRows(scanResultGroups(m.session.Catalog, skillView.Skills), m.isCollapsed, m.selected)...)
		switch {
		case m.scan.TechnologyTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No technologies found. Explore the catalog with Tab."})
		case m.scan.SkillTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No applicable recommendations. Explore the catalog with Tab."})
		case len(skillView.Skills) == 0:
			rows = append(rows, noseyRow{"notice", "", "All recommendations are hidden. Press x to clear filters."})
		}
	case 1:
		if strings.TrimSpace(m.filters[1].Query) == "" {
			return m.registryOverview()
		}
		result := catalog.Search(m.session.Catalog, m.filters[1], m.sort[1])
		rows = append(rows, resultRows(searchResultGroups(m.session.Catalog, result), m.isCollapsed, m.selected)...)
		if len(rows) == 0 {
			rows = append(rows, noseyRow{"notice", "", "No matching skills. Press x to clear filters."})
		}
	case 2:
		for _, s := range m.inventory.Skills {
			status := statusLabel(s.Status)
			if s.Unmanaged {
				status += " · unmanaged"
			}
			rows = append(rows, noseyRow{"inventory", s.Name, s.Name + "  ·  " + status})
		}
		if len(rows) == 0 {
			rows = append(rows, noseyRow{"notice", "", "No project inventory loaded. Press r to read installed skills."})
		}
	case 3:
		rows = append(rows, noseyRow{"setting", "display", "Display mode: " + m.session.Resolved.Settings.UI.Mode + " (next launch)"}, noseyRow{"setting", "collapse", "Repository groups: " + collapseLabel(m.session.Resolved.Settings.UI.CollapseRepos)})
		agents := strings.Join(m.session.Resolved.Settings.Installation.Agents, ", ")
		if agents == "" {
			agents = "ask at install"
		}
		rows = append(rows, noseyRow{"setting", "agents", "Target agents: " + agents}, noseyRow{"setting", "method", "Install method: " + m.session.Resolved.Settings.Installation.Method}, noseyRow{"setting", "ttl", fmt.Sprintf("Refresh interval: %d hours", m.session.Resolved.Settings.Updates.TTL)}, noseyRow{"setting", "mcp", fmt.Sprintf("MCP inspection: %t", m.session.Resolved.Settings.Detection.MCP)})
		rows = append(rows,
			noseyRow{"setting", "bytes", fmt.Sprintf("Maximum inspected file: %d bytes", m.session.Resolved.Settings.Detection.MaxBytes)},
			noseyRow{"setting", "exclude", "Excluded paths: " + strings.Join(m.session.Resolved.Settings.Detection.Exclude, ", ")})
		for _, r := range m.session.Resolved.Settings.Registries {
			rows = append(rows, noseyRow{"registry", r.ID, fmt.Sprintf("%s  · enabled=%t · priority=%d", r.ID, r.Enabled, r.Priority)})
		}
		for _, r := range m.session.Resolved.Pending {
			rows = append(rows, noseyRow{"pending", r.ID, "Pending trust: " + r.ID})
		}
		rows = append(rows, noseyRow{"setting", "trust", "Approve workspace registries"}, noseyRow{"setting", "revoke", "Revoke workspace approval"}, noseyRow{"setting", "new", "New registry"}, noseyRow{"setting", "add", "Add existing registry"})
	}
	return rows
}
func collapseLabel(on bool) string {
	if on {
		return "collapsed by default"
	}
	return "expanded by default"
}
func statusLabel(status string) string {
	if status == "outdated" {
		return "update available"
	}
	return status
}
func skillLabel(s domain.SkillView, selected bool) string {
	box := "[ ]"
	if selected || s.Status == "installed" {
		box = "[x]"
	}
	label := box + " " + s.Name + "  ·  " + statusLabel(s.Status)
	if s.Score != nil {
		label += fmt.Sprintf("  [score %.0f/100]", *s.Score)
	}
	if s.Quality != nil {
		label += "  [quality " + *s.Quality + "]"
	}
	if s.Score == nil {
		label += "  [score unscored]"
	}
	if s.Quality == nil {
		label += "  [quality unassessed]"
	}
	return label
}
