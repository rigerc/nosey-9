// Package catalog supplies shared recommendation, query, filter and grouping semantics.
package catalog

import (
	"cmp"
	"github.com/rigerc/nosey-9/internal/detection"
	"github.com/rigerc/nosey-9/internal/domain"
	"slices"
	"strings"
)

// Recommendations holds confirmed views and passing relationship unions.
type Recommendations struct {
	Skills      []domain.SkillView
	Unevaluated []domain.UnevaluatedRecommendation
	Requires    map[string][]string
	Conflicts   map[string][]string
}

// Recommend evaluates unfiltered mappings using three-state technology facts.
func Recommend(c domain.Catalog, d detection.Result) Recommendations {
	r := Recommendations{Skills: []domain.SkillView{}, Unevaluated: []domain.UnevaluatedRecommendation{}, Requires: map[string][]string{}, Conflicts: map[string][]string{}}
	views := map[string]domain.SkillView{}
	ids := []string{}
	for id := range c.Mappings {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		m := c.Mappings[id]
		for _, rec := range m.Recommendations {
			if rec.Disabled {
				continue
			}
			state, unknown := condition(m.TechnologyID, rec.When, d.States)
			if state == "absent" {
				continue
			}
			if state == "unknown" {
				r.Unevaluated = append(r.Unevaluated, domain.UnevaluatedRecommendation{SkillID: rec.SkillID, MappingID: id, State: "unevaluated", Unknown: unknown, Reason: "Required technology state is unknown"})
				continue
			}
			s, ok := c.Skills[rec.SkillID]
			if !ok {
				continue
			}
			view, exists := views[s.ID]
			if !exists {
				view = View(c, s)
				view.State = "confirmed"
				view.Priority = "optional"
			}
			priority := rec.Priority
			if priority == "" {
				priority = "optional"
			}
			if priorityRank(priority) > priorityRank(view.Priority) {
				view.Priority = priority
			}
			view.Technologies = append(view.Technologies, m.TechnologyID)
			reason := rec.Reason
			if reason == "" {
				reason = "Detected " + m.TechnologyID + "; mapping " + id + " applies."
			}
			view.Reasons = append(view.Reasons, reason)
			view.Technologies = unique(view.Technologies)
			view.Reasons = unique(view.Reasons)
			views[s.ID] = view
			r.Requires[s.ID] = unique(append(append(r.Requires[s.ID], s.Requires...), rec.Requires...))
			r.Conflicts[s.ID] = unique(append(append(r.Conflicts[s.ID], s.ConflictsWith...), rec.ConflictsWith...))
		}
	}
	for _, v := range views {
		r.Skills = append(r.Skills, v)
	}
	SortSkills(r.Skills, "priority-desc", "")
	return r
}
func condition(anchor string, when *domain.Condition, states map[string]string) (string, []string) {
	unknown := []string{}
	state := states[anchor]
	if state == "" {
		state = "unknown"
	}
	if state == "unknown" {
		unknown = append(unknown, anchor)
	}
	if state == "absent" {
		return state, unknown
	}
	if when == nil {
		return state, unknown
	}
	for _, group := range []struct {
		ids  []string
		mode string
	}{{when.All, "all"}, {when.Any, "any"}, {when.None, "none"}} {
		if len(group.ids) == 0 {
			continue
		}
		hits, missing := 0, 0
		blocked := []string{}
		for _, id := range group.ids {
			switch states[id] {
			case "detected":
				hits++
			case "absent":
				missing++
			default:
				blocked = append(blocked, id)
			}
		}
		failed := group.mode == "all" && missing > 0 || group.mode == "any" && hits == 0 && len(blocked) == 0 || group.mode == "none" && hits > 0
		if failed {
			return "absent", []string{}
		}
		uncertain := group.mode == "all" && len(blocked) > 0 || group.mode == "any" && hits == 0 && len(blocked) > 0 || group.mode == "none" && len(blocked) > 0
		if uncertain {
			state = "unknown"
			unknown = append(unknown, blocked...)
		}
	}
	return state, unique(unknown)
}

// View projects authored metadata without inventing ratings or installed state.
func View(c domain.Catalog, s domain.Skill) domain.SkillView {
	v := domain.SkillView{ID: s.ID, Name: s.Name, Status: "unknown", State: "confirmed", Priority: "optional", Technologies: []string{}, Reasons: []string{}, RepositoryID: s.RepositoryID, RegistryID: s.RegistryID, Quality: s.Quality, Score: s.Score, Assessment: s.Assessment}
	if status, ok := c.InstallStatus[s.ID]; ok {
		v.Status = status
	}
	if score, ok := c.Scores[s.RepositoryID]; ok {
		value := score.Average
		v.RepositoryAverage = &value
	}
	return v
}
func match(facet []string, value string) bool {
	return len(facet) == 0 || slices.Contains(facet, value) || slices.Contains(facet, "any")
}
func overlap(facet, values []string) bool {
	if len(facet) == 0 {
		return true
	}
	for _, v := range values {
		if slices.Contains(facet, v) {
			return true
		}
	}
	return false
}

// MatchSkill applies presentation facets without changing recommendations.
func MatchSkill(c domain.Catalog, v domain.SkillView, f domain.Filters) bool {
	s := c.Skills[v.ID]
	if !match(f.Registries, v.RegistryID) || !match(f.Repositories, v.RepositoryID) || !match(f.Priorities, v.Priority) || !match(f.Statuses, v.Status) || !overlap(f.RecommendationTechnologies, v.Technologies) {
		return false
	}
	quality := "unassessed"
	if v.Quality != nil {
		quality = *v.Quality
	}
	if !match(f.Qualities, quality) {
		return false
	}
	if f.ScoreState == "scored" && v.Score == nil || f.ScoreState == "unscored" && v.Score != nil {
		return false
	}
	if f.MinScore != nil && (v.Score == nil || *v.Score < *f.MinScore) {
		return false
	}
	if f.MinRepositoryAverage != nil && (v.RepositoryAverage == nil || *v.RepositoryAverage < *f.MinRepositoryAverage) {
		return false
	}
	return f.Query == "" || f.QueryTarget == "technologies" || Relevance(c, s, f.Query) > 0
}

// Relevance returns the contract-defined case-insensitive query rank.
func Relevance(c domain.Catalog, s domain.Skill, query string) int {
	q := strings.ToLower(query)
	id, name := strings.ToLower(s.ID), strings.ToLower(s.Name)
	if q == "" {
		return 0
	}
	if id == q || name == q {
		return 7
	}
	if strings.HasPrefix(id, q) || strings.HasPrefix(name, q) {
		return 6
	}
	if strings.Contains(id, q) || strings.Contains(name, q) {
		return 5
	}
	if strings.Contains(strings.ToLower(strings.Join(s.Tags, " ")), q) {
		return 4
	}
	if strings.Contains(strings.ToLower(s.Description), q) {
		return 3
	}
	for _, id := range s.Technologies {
		if strings.Contains(strings.ToLower(id+" "+c.Technologies[id].Name), q) {
			return 2
		}
	}
	if strings.Contains(strings.ToLower(s.RepositoryID+" "+c.Repositories[s.RepositoryID].Name), q) {
		return 1
	}
	return 0
}
func priorityRank(p string) int {
	switch p {
	case "core":
		return 3
	case "recommended":
		return 2
	default:
		return 1
	}
}
func qualityRank(q *string) int {
	if q == nil {
		return -1
	}
	switch *q {
	case "excellent":
		return 4
	case "good":
		return 3
	case "fair":
		return 2
	case "poor":
		return 1
	default:
		return -1
	}
}
func numberCompare(a, b *float64) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return 1
	}
	if b == nil {
		return -1
	}
	return cmp.Compare(*b, *a)
}

// SortSkills orders views deterministically with missing ratings last.
func SortSkills(views []domain.SkillView, order, _ string) {
	slices.SortStableFunc(views, func(a, b domain.SkillView) int {
		value := 0
		switch order {
		case "priority-desc":
			value = cmp.Compare(priorityRank(b.Priority), priorityRank(a.Priority))
		case "score-desc":
			value = numberCompare(a.Score, b.Score)
		case "repository-average-desc":
			value = numberCompare(a.RepositoryAverage, b.RepositoryAverage)
		case "quality-desc":
			value = cmp.Compare(qualityRank(b.Quality), qualityRank(a.Quality))
		}
		if value != 0 {
			return value
		}
		if value = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); value != 0 {
			return value
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Project filters a scan projection while preserving underlying totals and facts.
func Project(c domain.Catalog, result domain.ScanResult, f domain.Filters, sort domain.Sort) domain.ScanResult {
	result.Filters = f
	result.Sort = sort
	tech := []domain.Detection{}
	skills := []domain.SkillView{}
	for _, d := range result.Technologies {
		t := c.Technologies[d.ID]
		if !match(f.Technologies, d.ID) {
			continue
		}
		if f.Query != "" && f.QueryTarget != "skills" && !strings.Contains(strings.ToLower(t.ID+" "+t.Name), strings.ToLower(f.Query)) {
			continue
		}
		kinds := []string{}
		for _, e := range d.Evidence {
			kinds = append(kinds, e.Kind)
		}
		if !overlap(f.RuleKinds, kinds) {
			continue
		}
		tech = append(tech, d)
	}
	for _, v := range result.Skills {
		if MatchSkill(c, v, f) {
			skills = append(skills, v)
		}
	}
	slices.SortFunc(tech, func(a, b domain.Detection) int {
		if sort.Technologies == "evidence-count-desc" && len(a.Evidence) != len(b.Evidence) {
			return len(b.Evidence) - len(a.Evidence)
		}
		if sort.Technologies == "matched-rules-desc" {
			ar, br := []string{}, []string{}
			for _, e := range a.Evidence {
				ar = append(ar, e.RuleID)
			}
			for _, e := range b.Evidence {
				br = append(br, e.RuleID)
			}
			if len(unique(ar)) != len(unique(br)) {
				return len(unique(br)) - len(unique(ar))
			}
		}
		v := strings.Compare(strings.ToLower(c.Technologies[a.ID].Name), strings.ToLower(c.Technologies[b.ID].Name))
		if sort.Technologies == "name-desc" {
			v = -v
		}
		if v == 0 {
			return strings.Compare(a.ID, b.ID)
		}
		return v
	})
	SortSkills(skills, sort.Skills, f.Query)
	result.Technologies = tech
	result.Skills = skills
	result.VisibleTechnologies = len(tech)
	result.VisibleSkills = len(skills)
	return result
}

// SearchSkill is the closed search-result skill wire record.
type SearchSkill struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Status     string             `json:"status"`
	Selected   bool               `json:"selected"`
	Quality    *string            `json:"quality"`
	Score      *float64           `json:"score"`
	Assessment *domain.Assessment `json:"assessment"`
	RegistryID string             `json:"sourceRegistryId"`
}

// SearchRepository groups visible skills with authored repository ratings.
type SearchRepository struct {
	ID      string        `json:"repositoryId"`
	Name    string        `json:"name"`
	Average *float64      `json:"averageScore"`
	Count   *int          `json:"scoreCount"`
	Skills  []SearchSkill `json:"skills"`
}

// SearchGroup groups repository results under a canonical technology.
type SearchGroup struct {
	ID           string             `json:"technologyId"`
	Name         string             `json:"technologyName"`
	Repositories []SearchRepository `json:"repositories"`
}

// SearchResult is the versioned catalog search envelope.
type SearchResult struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Groups      []SearchGroup       `json:"groups"`
	Total       int                 `json:"totalUniqueSkills"`
}

// Search groups independently browsable catalog matches by technology/repository.
func Search(c domain.Catalog, f domain.Filters, sort string) SearchResult {
	views := []domain.SkillView{}
	for _, s := range c.Skills {
		v := View(c, s)
		v.Technologies = s.Technologies
		if !overlap(f.Technologies, s.Technologies) || !MatchSkill(c, v, f) {
			continue
		}
		views = append(views, v)
	}
	SortSkills(views, sort, f.Query)
	if sort == "relevance" {
		slices.SortStableFunc(views, func(a, b domain.SkillView) int {
			return Relevance(c, c.Skills[b.ID], f.Query) - Relevance(c, c.Skills[a.ID], f.Query)
		})
	}
	result := SearchResult{Version: 2, Diagnostics: c.Diagnostics, Groups: []SearchGroup{}, Total: len(views)}
	groups := map[string]map[string][]SearchSkill{}
	for _, v := range views {
		s := c.Skills[v.ID]
		techs := s.Technologies
		if len(techs) == 0 {
			techs = []string{"other"}
		}
		for _, t := range techs {
			if !match(f.Technologies, t) {
				continue
			}
			if groups[t] == nil {
				groups[t] = map[string][]SearchSkill{}
			}
			groups[t][v.RepositoryID] = append(groups[t][v.RepositoryID], SearchSkill{v.ID, v.Name, v.Status, v.Selected, v.Quality, v.Score, v.Assessment, v.RegistryID})
		}
	}
	ids := []string{}
	for id := range groups {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		name := c.Technologies[id].Name
		if id == "other" {
			name = "Other / Uncategorized"
		}
		g := SearchGroup{ID: id, Name: name, Repositories: []SearchRepository{}}
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
			repo := SearchRepository{ID: rid, Name: name, Skills: groups[id][rid]}
			if score, ok := c.Scores[rid]; ok {
				value := score.Average
				repo.Average = &value
				repo.Count = score.Count
			}
			g.Repositories = append(g.Repositories, repo)
		}
		result.Groups = append(result.Groups, g)
	}
	return result
}
func unique(v []string) []string { v = slices.Clone(v); slices.Sort(v); return slices.Compact(v) }
