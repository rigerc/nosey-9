package catalog

import (
	"github.com/rigerc/nosey-9/internal/detection"
	"github.com/rigerc/nosey-9/internal/domain"
	"slices"
	"testing"
)

func TestUnknownConditionsNeverBecomeAbsence(t *testing.T) {
	for _, tc := range []struct {
		anchor string
		when   *domain.Condition
		states map[string]string
		want   string
	}{{"a", &domain.Condition{None: []string{"b"}}, map[string]string{"a": "detected", "b": "unknown"}, "unknown"}, {"a", &domain.Condition{Any: []string{"b", "c"}}, map[string]string{"a": "detected", "b": "unknown", "c": "detected"}, "detected"}, {"a", &domain.Condition{All: []string{"b", "c"}}, map[string]string{"a": "detected", "b": "unknown", "c": "absent"}, "absent"}} {
		state, _ := condition(tc.anchor, tc.when, tc.states)
		if state != tc.want {
			t.Fatalf("got %s want %s", state, tc.want)
		}
	}
}
func TestPassingMappingUnionAndUnknownExclusion(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{"s": {ID: "s", Name: "Skill"}}, Mappings: map[string]domain.Mapping{"first": {ID: "first", TechnologyID: "a", Recommendations: []domain.Recommendation{{SkillID: "s", Priority: "recommended", Requires: []string{"one"}}}}, "second": {ID: "second", TechnologyID: "b", Recommendations: []domain.Recommendation{{SkillID: "s", Priority: "core", Requires: []string{"two"}}}}, "unknown": {ID: "unknown", TechnologyID: "c", Recommendations: []domain.Recommendation{{SkillID: "s", Requires: []string{"wrong"}}}}}}
	r := Recommend(c, detection.Result{States: map[string]string{"a": "detected", "b": "detected", "c": "unknown"}})
	if len(r.Skills) != 1 || r.Skills[0].Priority != "core" || len(r.Requires["s"]) != 2 || len(r.Unevaluated) != 1 {
		t.Fatal(r)
	}
}
func TestRatingsNullLastAndFilteringDoesNotMutate(t *testing.T) {
	zero := float64(0)
	hundred := float64(100)
	views := []domain.SkillView{{ID: "unknown", Name: "A"}, {ID: "zero", Name: "B", Score: &zero}, {ID: "hundred", Name: "C", Score: &hundred}}
	SortSkills(views, "score-desc", "")
	if views[0].ID != "hundred" || views[1].ID != "zero" || views[2].ID != "unknown" {
		t.Fatal(views)
	}
	c := domain.Catalog{Skills: map[string]domain.Skill{"zero": {ID: "zero", Name: "B"}}}
	f := domain.DefaultFilters()
	f.ScoreState = "scored"
	if !MatchSkill(c, views[1], f) || MatchSkill(c, views[2], f) {
		t.Fatal("null and zero conflated")
	}
}

func TestSharedFiltersRatingsQueriesAndGroupDeduplication(t *testing.T) {
	quality := "good"
	score, average := 80.0, 90.0
	c := domain.Catalog{Diagnostics: []domain.Diagnostic{}, Technologies: map[string]domain.Technology{"next": {ID: "next", Name: "Next.js"}, "react": {ID: "react", Name: "React"}}, Skills: map[string]domain.Skill{
		"one": {ID: "one", Name: "Next patterns", Description: "Server components", Tags: []string{"caching"}, Technologies: []string{"next", "react"}, RepositoryID: "repo", RegistryID: "registry", Quality: &quality, Score: &score},
		"two": {ID: "two", Name: "Unassessed", RepositoryID: "repo", RegistryID: "registry"},
	}, Repositories: map[string]domain.Repository{"repo": {ID: "repo", Name: "Official examples"}}, Roots: map[string]string{"registry": "/registry"}, Scores: map[string]domain.RepositoryScore{"repo": {Average: average}}}
	f := domain.DefaultFilters()
	result := Search(c, f, "relevance")
	if result.Total != 2 || len(result.Groups) != 3 {
		t.Fatal("duplicate grouped rows changed unique count", result)
	}
	cases := []struct {
		query string
		want  int
	}{{"ONE", 1}, {"next patterns", 1}, {"next", 1}, {"caching", 1}, {"server components", 1}, {"react", 1}, {"official examples", 2}, {"not present", 0}}
	for _, tc := range cases {
		f.Query = tc.query
		if got := Search(c, f, "relevance").Total; got != tc.want {
			t.Fatalf("query %q got %d want %d", tc.query, got, tc.want)
		}
	}
	f = domain.DefaultFilters()
	f.Qualities = []string{"unassessed"}
	if Search(c, f, "quality-desc").Total != 1 {
		t.Fatal("unassessed facet failed")
	}
	f = domain.DefaultFilters()
	f.MinScore = &score
	f.MinRepositoryAverage = &average
	f.ScoreState = "scored"
	f.Technologies = []string{"react"}
	f.Registries = []string{"registry"}
	f.Repositories = []string{"repo"}
	if err := ValidateFilters(c, f); err != nil {
		t.Fatal(err)
	}
	if Search(c, f, "repository-average-desc").Total != 1 {
		t.Fatal("composed filters failed")
	}
	views := []domain.SkillView{View(c, c.Skills["two"]), View(c, c.Skills["one"])}
	for _, order := range []string{"quality-desc", "score-desc", "repository-average-desc", "name-asc", "priority-desc"} {
		SortSkills(views, order, "")
		if views[0].ID != "one" {
			t.Fatal("rating/null/order contract failed", order, views)
		}
	}
	for _, q := range []string{"excellent", "good", "fair", "poor"} {
		value := q
		if qualityRank(&value) <= 0 {
			t.Fatal("known quality lost rank")
		}
	}
	invalid := []func(*domain.Filters){func(f *domain.Filters) { f.QueryTarget = "bad" }, func(f *domain.Filters) { f.ScoreState = "bad" }, func(f *domain.Filters) { f.Qualities = []string{"bad"} }, func(f *domain.Filters) { f.Technologies = []string{"bad"} }, func(f *domain.Filters) { f.Registries = []string{"bad"} }, func(f *domain.Filters) { f.Repositories = []string{"bad"} }, func(f *domain.Filters) { v := 101.0; f.MinScore = &v }}
	for _, mutate := range invalid {
		f = domain.DefaultFilters()
		mutate(&f)
		if ValidateFilters(c, f) == nil {
			t.Fatal("invalid filter accepted", f)
		}
	}
	f = domain.DefaultFilters()
	f.RuleKinds = []string{"dependency"}
	scan := domain.ScanResult{Technologies: []domain.Detection{{ID: "next", Evidence: []domain.Evidence{{RuleID: "one", Kind: "dependency"}, {RuleID: "two", Kind: "file"}}}, {ID: "react", Evidence: []domain.Evidence{{RuleID: "a", Kind: "file"}}}}, TechnologyTotal: 2, Skills: views, SkillTotal: 2}
	for _, order := range []string{"name-asc", "name-desc", "evidence-count-desc", "matched-rules-desc"} {
		projected := Project(c, scan, f, domain.Sort{Technologies: order, Skills: "name-asc"})
		if projected.VisibleTechnologies != 1 || projected.TechnologyTotal != 2 || projected.Technologies[0].ID != "next" {
			t.Fatal("projection mutated facts", projected)
		}
	}
	if len(scan.Technologies) != 2 {
		t.Fatal("projection changed input")
	}
}

func TestPriorityOrderBreaksTiesByRelevanceSignals(t *testing.T) {
	high, low := 90.0, 40.0
	good, fair := "good", "fair"
	views := []domain.SkillView{
		{ID: "alpha", Name: "Alpha", Priority: "optional", Technologies: []string{"a", "b", "c"}},
		{ID: "core", Name: "Zed core", Priority: "core", Technologies: []string{"a"}},
		{ID: "many", Name: "Many", Priority: "recommended", Technologies: []string{"a", "b"}},
		{ID: "few-high", Name: "Few high", Priority: "recommended", Technologies: []string{"a"}, Score: &high},
		{ID: "few-low", Name: "Few low", Priority: "recommended", Technologies: []string{"a"}, Score: &low, Quality: &fair},
		{ID: "few-none", Name: "Few none", Priority: "recommended", Technologies: []string{"a"}, Quality: &good},
	}
	SortSkills(views, "priority-desc", "")
	got := []string{}
	for _, v := range views {
		got = append(got, v.ID)
	}
	want := []string{"core", "many", "few-high", "few-low", "few-none", "alpha"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	SortSkills(views, "score-desc", "")
	if views[0].ID != "few-high" || views[1].ID != "few-low" || views[2].ID != "core" {
		t.Fatal(views)
	}
}
