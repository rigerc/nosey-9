package ui

import (
	"fmt"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
)

func (m noseyModel) activeFilter() int {
	if m.tab == 1 {
		return 2
	}
	return m.section
}
func (m noseyModel) rows() []noseyRow {
	if m.rowCache != nil {
		return m.rowCache
	}
	rows := []noseyRow{}
	switch m.tab {
	case 0:
		techView := catalog.Project(m.session.Catalog, m.scan, m.filters[0], domain.Sort{Technologies: m.sort[0], Skills: m.sort[1]})
		skillView := catalog.Project(m.session.Catalog, m.scan, m.filters[1], domain.Sort{Technologies: m.sort[0], Skills: m.sort[1]})
		rows = append(rows, noseyRow{"heading", "technologies", fmt.Sprintf("Detected technologies  %d/%d", len(techView.Technologies), m.scan.TechnologyTotal)})
		for _, t := range techView.Technologies {
			rows = append(rows, noseyRow{"technology", t.ID, m.session.Catalog.Technologies[t.ID].Name})
		}
		rows = append(rows, noseyRow{"heading", "skills", fmt.Sprintf("Recommended skills  %d/%d", len(skillView.Skills), m.scan.SkillTotal)})
		rows = append(rows, resultRows(scanResultGroups(m.session.Catalog, skillView.Skills), m.collapsed, m.selected)...)
		switch {
		case m.scan.TechnologyTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No technologies found. Explore the catalog with Tab."})
		case m.scan.SkillTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No applicable recommendations. Explore the catalog with Tab."})
		case len(skillView.Skills) == 0:
			rows = append(rows, noseyRow{"notice", "", "All recommendations are hidden. Press x to clear filters."})
		}
	case 1:
		if strings.TrimSpace(m.filters[2].Query) == "" {
			return m.registryOverview()
		}
		result := catalog.Search(m.session.Catalog, m.filters[2], m.sort[2])
		rows = append(rows, resultRows(searchResultGroups(m.session.Catalog, result), m.collapsed, m.selected)...)
		if len(rows) == 0 {
			rows = append(rows, noseyRow{"notice", "", "No matching skills. Press x to clear filters."})
		}
	case 2:
		for _, s := range m.inventory.Skills {
			status := s.Status
			if s.Unmanaged {
				status += " · unmanaged"
			}
			rows = append(rows, noseyRow{"inventory", s.Name, s.Name + "  ·  " + status})
		}
		if len(rows) == 0 {
			rows = append(rows, noseyRow{"notice", "", "No project inventory loaded. Press r to read installed skills."})
		}
	case 3:
		rows = append(rows, noseyRow{"setting", "display", "Display mode: " + m.session.Resolved.Settings.UI.Mode + " (next launch)"})
		rows = append(rows, noseyRow{"setting", "agents", "Target agents: " + strings.Join(m.session.Resolved.Settings.Installation.Agents, ", ")}, noseyRow{"setting", "method", "Install method: " + m.session.Resolved.Settings.Installation.Method}, noseyRow{"setting", "ttl", fmt.Sprintf("Refresh interval: %d hours", m.session.Resolved.Settings.Updates.TTL)}, noseyRow{"setting", "mcp", fmt.Sprintf("MCP inspection: %t", m.session.Resolved.Settings.Detection.MCP)})
		rows = append(rows,
			noseyRow{"setting", "mode", "Default install action: " + m.session.Resolved.Settings.Installation.Mode},
			noseyRow{"setting", "telemetry", "Upstream telemetry: " + m.session.Resolved.Settings.Installation.Telemetry},
			noseyRow{"setting", "verbose", fmt.Sprintf("Verbose installation: %t", m.session.Resolved.Settings.Installation.Verbose)},
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
func skillLabel(s domain.SkillView, selected bool) string {
	box := "[ ]"
	if selected {
		box = "[x]"
	}
	label := box + " " + s.Name + "  ·  " + s.Status
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
