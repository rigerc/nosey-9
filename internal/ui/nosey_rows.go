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

// noMatch is a quality value no skill carries, so an empty allow-list hides everything.
const noMatch = "\x00"

var qualityOrder = []string{"fair", "good", "excellent"}

// qualitiesFrom lists the quality labels at or above min; unassessed and poor never qualify.
func qualitiesFrom(min string) []string {
	if at := slices.Index(qualityOrder, min); at >= 0 {
		return qualityOrder[at:]
	}
	return nil
}

// effectiveFilters applies the minimum score, repository score and quality settings on top of one tab's filters.
func (m noseyModel) effectiveFilters(tab int) domain.Filters {
	f, ui := m.filters[tab], m.session.Resolved.Settings.UI
	if min := float64(ui.MinScore); min > 0 && (f.MinScore == nil || *f.MinScore < min) {
		f.MinScore = &min
	}
	if min := float64(ui.MinRepoScore); min > 0 && (f.MinRepositoryAverage == nil || *f.MinRepositoryAverage < min) {
		f.MinRepositoryAverage = &min
	}
	if allowed := qualitiesFrom(ui.MinQuality); allowed != nil {
		if len(f.Qualities) > 0 {
			allowed = slices.DeleteFunc(slices.Clone(allowed), func(q string) bool { return !slices.Contains(f.Qualities, q) })
		}
		if len(allowed) == 0 {
			allowed = []string{noMatch}
		}
		f.Qualities = allowed
	}
	return f
}
func (m noseyModel) thresholdsActive() bool {
	ui := m.session.Resolved.Settings.UI
	return ui.MinScore > 0 || ui.MinRepoScore > 0 || qualitiesFrom(ui.MinQuality) != nil
}

// hiddenByThreshold counts results removed only by the minimum score, repository score and quality settings.
// without lazily counts the results the same view would show without them.
func (m noseyModel) hiddenByThreshold(shown int, without func() int) int {
	if !m.thresholdsActive() {
		return 0
	}
	return max(0, without()-shown)
}
func (m noseyModel) thresholdNotice(hidden int) string {
	ui := m.session.Resolved.Settings.UI
	parts := []string{}
	if ui.MinScore > 0 {
		parts = append(parts, fmt.Sprintf("score %d", ui.MinScore))
	}
	if ui.MinRepoScore > 0 {
		parts = append(parts, fmt.Sprintf("repo score %d", ui.MinRepoScore))
	}
	if qualitiesFrom(ui.MinQuality) != nil {
		parts = append(parts, "quality "+ui.MinQuality)
	}
	return fmt.Sprintf("%d hidden below %s", hidden, strings.Join(parts, ", "))
}
func countSkills(groups []resultGroup) int {
	seen := map[string]bool{}
	for _, g := range groups {
		for _, s := range groupSkills(g) {
			seen[s.ID] = true
		}
	}
	return len(seen)
}
func (m noseyModel) rows() []noseyRow {
	if m.rowCache != nil {
		return m.rowCache
	}
	rows := []noseyRow{}
	switch m.tab {
	case 0:
		sort := domain.Sort{Technologies: "name-asc", Skills: m.sort[0]}
		skillView := catalog.Project(m.session.Catalog, m.scan, m.effectiveFilters(0), sort)
		hidden := m.hiddenByThreshold(len(skillView.Skills), func() int {
			return len(catalog.Project(m.session.Catalog, m.scan, m.filters[0], sort).Skills)
		})
		heading := fmt.Sprintf("Recommended skills  %d/%d", len(skillView.Skills), m.scan.SkillTotal)
		if hidden > 0 {
			heading += "  ·  " + m.thresholdNotice(hidden)
		}
		rows = append(rows, noseyRow{"heading", "skills", heading})
		rows = append(rows, resultRows(scanResultGroups(m.session.Catalog, skillView.Skills), m.isCollapsed, m.selected)...)
		switch {
		case m.scan.TechnologyTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No technologies found. Explore the catalog with Tab."})
		case m.scan.SkillTotal == 0:
			rows = append(rows, noseyRow{"notice", "", "No applicable recommendations. Explore the catalog with Tab."})
		case len(skillView.Skills) == 0:
			message := "All recommendations are hidden. Press x to clear filters."
			if hidden > 0 {
				message += " Relax the score/quality settings to show more."
			}
			rows = append(rows, noseyRow{"notice", "", message})
		}
	case 1:
		if strings.TrimSpace(m.filters[1].Query) == "" {
			return m.registryOverview()
		}
		result := catalog.Search(m.session.Catalog, m.effectiveFilters(1), m.sort[1])
		groups := searchResultGroups(m.session.Catalog, result)
		hidden := m.hiddenByThreshold(countSkills(groups), func() int {
			return countSkills(searchResultGroups(m.session.Catalog, catalog.Search(m.session.Catalog, m.filters[1], m.sort[1])))
		})
		if hidden > 0 {
			rows = append(rows, noseyRow{"heading", "hidden", m.thresholdNotice(hidden)})
		}
		rows = append(rows, resultRows(groups, m.isCollapsed, m.selected)...)
		if len(groups) == 0 {
			message := "No matching skills. Press x to clear filters."
			if hidden > 0 {
				message += " Relax the score/quality settings to show more."
			}
			rows = append(rows, noseyRow{"notice", "", message})
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
		rows = append(rows, noseyRow{"setting", "display", "Display mode: " + m.session.Resolved.Settings.UI.Mode + " (next launch)"}, noseyRow{"setting", "collapse", "Repository groups: " + collapseLabel(m.session.Resolved.Settings.UI.CollapseRepos)}, noseyRow{"setting", "minscore", "Minimum score: " + minScoreLabel(m.session.Resolved.Settings.UI.MinScore)}, noseyRow{"setting", "minrepo", "Minimum repository score: " + minScoreLabel(m.session.Resolved.Settings.UI.MinRepoScore)}, noseyRow{"setting", "minquality", "Minimum quality: " + minQualityLabel(m.session.Resolved.Settings.UI.MinQuality)})
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
		rows = append(rows, noseyRow{"setting", "trust", "Approve workspace registries"}, noseyRow{"setting", "revoke", "Revoke workspace approval"}, noseyRow{"setting", "new", "New registry"}, noseyRow{"setting", "add", "Add existing registry"}, noseyRow{"setting", "reset", "Reset settings to defaults"})
	}
	return rows
}
func minQualityLabel(min string) string {
	if qualitiesFrom(min) == nil {
		return "any (show all)"
	}
	return min + " or better (hide lower and unassessed)"
}
func minScoreLabel(min int) string {
	if min <= 0 {
		return "off (show all)"
	}
	return fmt.Sprintf("%d/100 (hide lower and unscored)", min)
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
