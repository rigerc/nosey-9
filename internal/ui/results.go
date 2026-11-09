package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
)

type resultRepository struct {
	id, name string
	average  *float64
	skills   []domain.SkillView
}
type resultGroup struct {
	id, name     string
	repositories []resultRepository
}

// Scan and Explore both feed this result tree, preserving their own filters and ordering.
func scanResultGroups(c domain.Catalog, skills []domain.SkillView) []resultGroup {
	groups := map[string]map[string][]domain.SkillView{}
	for _, skill := range skills {
		techs := skill.Technologies
		if len(techs) == 0 {
			techs = []string{"other"}
		}
		for _, id := range techs {
			if groups[id] == nil {
				groups[id] = map[string][]domain.SkillView{}
			}
			groups[id][skill.RepositoryID] = append(groups[id][skill.RepositoryID], skill)
		}
	}
	ids := []string{}
	for id := range groups {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return strings.Compare(strings.ToLower(c.Technologies[a].Name)+a, strings.ToLower(c.Technologies[b].Name)+b)
	})
	result := []resultGroup{}
	for _, id := range ids {
		name := c.Technologies[id].Name
		if name == "" {
			name = id
		}
		if id == "other" {
			name = "Other / Uncategorized"
		}
		g := resultGroup{id: id, name: name}
		repos := []string{}
		for rid := range groups[id] {
			repos = append(repos, rid)
		}
		slices.Sort(repos)
		for _, rid := range repos {
			name := c.Repositories[rid].Name
			if name == "" {
				name = rid
			}
			if name == "" {
				name = "Unspecified repository"
			}
			repo := resultRepository{id: rid, name: name, skills: groups[id][rid]}
			if rating, ok := c.Scores[rid]; ok {
				v := rating.Average
				repo.average = &v
			}
			g.repositories = append(g.repositories, repo)
		}
		result = append(result, g)
	}
	return result
}
func searchResultGroups(c domain.Catalog, result catalog.SearchResult) []resultGroup {
	groups := []resultGroup{}
	for _, g := range result.Groups {
		group := resultGroup{id: g.ID, name: g.Name}
		for _, r := range g.Repositories {
			repo := resultRepository{id: r.ID, name: r.Name, average: r.Average}
			if repo.name == "" {
				repo.name = "Unspecified repository"
			}
			for _, s := range r.Skills {
				repo.skills = append(repo.skills, catalog.View(c, c.Skills[s.ID]))
			}
			group.repositories = append(group.repositories, repo)
		}
		groups = append(groups, group)
	}
	return groups
}
func resultRows(groups []resultGroup, collapsed func(string) bool, selected map[string]bool) []noseyRow {
	rows := []noseyRow{}
	for _, g := range groups {
		key := "tech:" + g.id
		rows = append(rows, noseyRow{"group", key, disclosure(collapsed(key)) + selectionMark(groupSkills(g), selected) + " " + g.name})
		if collapsed(key) {
			continue
		}
		for _, r := range g.repositories {
			key := "repo:" + g.id + ":" + r.id
			label := "    " + disclosure(collapsed(key)) + selectionMark(r.skills, selected) + " " + r.name
			if r.average != nil {
				label += fmt.Sprintf("  [repo %.0f/100]", *r.average)
			} else {
				label += "  [repo unscored]"
			}
			rows = append(rows, noseyRow{"group", key, label})
			if collapsed(key) {
				continue
			}
			for _, s := range r.skills {
				rows = append(rows, noseyRow{"skill", s.ID, "        " + skillLabel(s, selected[s.ID])})
			}
		}
	}
	return rows
}
func disclosure(closed bool) string {
	if closed {
		return "▸ "
	}
	return "▾ "
}

func groupSkills(group resultGroup) []domain.SkillView {
	skills := []domain.SkillView{}
	for _, repo := range group.repositories {
		skills = append(skills, repo.skills...)
	}
	return skills
}
func selectionMark(skills []domain.SkillView, selected map[string]bool) string {
	all, chosen := map[string]bool{}, map[string]bool{}
	for _, skill := range skills {
		all[skill.ID] = true
		if skill.Status == "installed" || selected[skill.ID] {
			chosen[skill.ID] = true
		}
	}
	if len(all) > 0 && len(chosen) == len(all) {
		return "[x]"
	}
	if len(chosen) > 0 {
		return "[-]"
	}
	return "[ ]"
}
func (m noseyModel) resultGroups() []resultGroup {
	if m.tab == 1 {
		return searchResultGroups(m.session.Catalog, catalog.Search(m.session.Catalog, m.effectiveFilters(1), m.sort[1]))
	}
	view := catalog.Project(m.session.Catalog, m.scan, m.effectiveFilters(0), domain.Sort{Technologies: "name-asc", Skills: m.sort[0]})
	return scanResultGroups(m.session.Catalog, view.Skills)
}
func (m *noseyModel) toggleGroup(key string) {
	ids := map[string]bool{}
	for _, group := range m.resultGroups() {
		for _, repo := range group.repositories {
			if key != "tech:"+group.id && key != "repo:"+group.id+":"+repo.id {
				continue
			}
			for _, skill := range repo.skills {
				if skill.Status != "installed" && m.session.Catalog.InstallStatus[skill.ID] != "installed" {
					ids[skill.ID] = true
				}
			}
		}
	}
	all := len(ids) > 0
	for id := range ids {
		if !m.selected[id] {
			all = false
		}
	}
	for id := range ids {
		if all {
			delete(m.selected, id)
		} else {
			m.selected[id] = true
		}
	}
}
