package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/settings"
)

func productModel() noseyModel {
	m := newNoseyModel(context.Background(), "/workspace", "")
	m.loading = false
	m.session = app.Session{Catalog: domain.Catalog{Technologies: map[string]domain.Technology{"a": {ID: "a", Name: "Alpha"}, "b": {ID: "b", Name: "Beta"}}, Skills: map[string]domain.Skill{"s": {ID: "s", Name: "Shared", Technologies: []string{"a", "b"}, RepositoryID: "repo"}}, Repositories: map[string]domain.Repository{"repo": {ID: "repo", Name: "Repository"}}}}
	m.scan = domain.ScanResult{Technologies: []domain.Detection{{ID: "a", Evidence: []domain.Evidence{{RuleID: "file", Kind: "file", ProjectPath: "."}}}}, Skills: []domain.SkillView{{ID: "s", Name: "Shared", Status: "unknown", Priority: "optional", Technologies: []string{"a"}}}, TechnologyTotal: 1, SkillTotal: 1}
	return m
}
func productUpdate(t *testing.T, m noseyModel, key tea.KeyPressMsg) noseyModel {
	t.Helper()
	updated, _ := m.Update(key)
	model, ok := updated.(noseyModel)
	if !ok {
		t.Fatal("unexpected model")
	}
	return model
}
func keypress(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func TestNoseyTabsExitSearchAndPreserveSelection(t *testing.T) {
	m := productModel()
	m.search = true
	m.query.Focus()
	m.selected["s"] = true
	m = productUpdate(t, m, keypress(tea.KeyTab))
	if m.tab != 1 || !m.search || !m.selected["s"] {
		t.Fatal("tab/search/selection contract broken")
	}
	for i := 0; i < 3; i++ {
		m = productUpdate(t, m, keypress(tea.KeyTab))
	}
	if m.tab != 0 {
		t.Fatal("tab cycle does not have four sections")
	}
}
func TestNoseyModalTrapsTabAndEscapeCancels(t *testing.T) {
	m := productModel()
	m.openForm("filters", []string{"Query", "Status"}, []string{"", "unknown"})
	m = productUpdate(t, m, keypress(tea.KeyTab))
	if m.tab != 0 || m.form.index != 1 {
		t.Fatal("modal did not trap focus")
	}
	m = productUpdate(t, m, keypress(tea.KeyEsc))
	if m.modal != "" {
		t.Fatal("Escape did not cancel modal")
	}
}
func TestNoseyExploreDuplicateRowsShareSelectionAndHiddenSelectionPersists(t *testing.T) {
	m := productModel()
	m.tab = 1
	m.filters[1].Query = "Shared"
	rows := m.rows()
	count := 0
	for i, row := range rows {
		if row.kind == "skill" {
			count++
			m.cursor[1] = i
		}
	}
	if count != 2 {
		t.Fatal("expected cross-technology duplicate rows")
	}
	m = productUpdate(t, m, keypress(tea.KeySpace))
	if len(m.selected) != 1 || !m.selected["s"] {
		t.Fatal("duplicate rows got independent selection")
	}
	m.filters[1].Query = "no-match"
	if len(m.selected) != 1 {
		t.Fatal("filter cleared hidden selection")
	}
	m.session.Resolved.Settings = settings.Defaults()
	m.session.Resolved.Settings.Installation.Agents = []string{"codex"}
	skill := m.session.Catalog.Skills["s"]
	skill.Source = domain.Source{Kind: "git", URI: "owner/repo", SkillName: "shared"}
	m.session.Catalog.Skills["s"] = skill
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "install" || m.err != nil || len(m.install.plans) != 1 || m.install.plans[0].Skills[0].ID != "s" {
		t.Fatal("hidden selection did not reach the install page", m.modal, m.err)
	}
}
func TestNoseyScanFiltersTargetRecommendationsOnly(t *testing.T) {
	m := productModel()
	m = productUpdate(t, m, keypress('f'))
	m.form.inputs[0].SetValue("skill-query")
	m.form.inputs[1].SetValue("a")
	updated, _ := m.submitForm()
	m = updated
	if m.err != nil || m.filters[0].Query != "skill-query" || m.filters[1].Query != "" {
		t.Fatal("Scan filter did not target recommendations", m.err)
	}
	if !slices.Equal(m.filters[0].RecommendationTechnologies, []string{"a"}) || len(m.filters[0].Technologies) != 0 {
		t.Fatal("Scan technology filter was not applied to recommendations", m.filters[0])
	}
	m = productUpdate(t, m, keypress('f'))
	if got := m.form.inputs[1].Value(); got != "a" {
		t.Fatalf("reopened form lost the recommendation technology filter: %q", got)
	}
}

func TestLoadingAllowsNavigationQuitAndIgnoresStaleInventory(t *testing.T) {
	m := productModel()
	m.loading = true
	m = productUpdate(t, m, keypress(tea.KeyTab))
	if m.tab != 1 {
		t.Fatal("loading swallowed tab navigation")
	}
	m = productUpdate(t, m, keypress(tea.KeyEsc))
	_, cmd := m.Update(keypress('q'))
	if cmd == nil {
		t.Fatal("loading swallowed quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit did not terminate")
	}
	m.generation = 3
	updated, _ := m.Update(noseyInventory{inventory: installation.Inventory{Complete: true}, generation: 2})
	next, ok := updated.(noseyModel)
	if !ok {
		t.Fatal("unexpected model type")
	}
	if next.inventory.Complete {
		t.Fatal("stale inventory replaced the current generation")
	}
}
func TestSharedResultTreeShowsBadgesAndKeepsEvidenceOutOfRows(t *testing.T) {
	m := productModel()
	score, average := 87.0, 91.0
	quality := "good"
	skill := m.session.Catalog.Skills["s"]
	skill.Score = &score
	skill.Quality = &quality
	m.session.Catalog.Skills["s"] = skill
	m.session.Catalog.Scores = map[string]domain.RepositoryScore{"repo": {Average: average}}
	m.scan.Skills = []domain.SkillView{catalog.View(m.session.Catalog, skill)}
	m.scan.Skills[0].Technologies = []string{"a"}
	m.scan.Technologies[0].Evidence[0].Path = "private-evidence/package.json"
	for _, tab := range []int{0, 1} {
		m.tab = tab
		m.filters[1].Query = "Shared"
		m.rowCache = nil
		rows := m.rows()
		foundRepo, foundSkill := false, false
		for _, row := range rows {
			if strings.Contains(row.label, "private-evidence") {
				t.Fatal("evidence leaked into main list")
			}
			if row.kind == "group" && strings.HasPrefix(row.id, "repo:") {
				foundRepo = strings.Contains(row.label, "[repo 91/100]")
			}
			if row.kind == "skill" {
				foundSkill = strings.Contains(row.label, "[score 87/100]") && strings.Contains(row.label, "[quality good]")
			}
		}
		if !foundRepo || !foundSkill {
			t.Fatal("Scan/Explore lost shared rating presentation", tab, rows)
		}
		slots, _ := resultSlots(rows)
		if len(slots) <= len(rows) {
			t.Fatal("group spacing missing")
		}
	}
}
func TestLargeResultWindowBoundsRenderingAndRetainsFocus(t *testing.T) {
	m := productModel()
	m.tab = 1
	m.filters[1].Query = "skill"
	for i := 0; i < 2000; i++ {
		id := fmt.Sprintf("skill-%04d", i)
		m.session.Catalog.Skills[id] = domain.Skill{ID: id, Name: id, Technologies: []string{"a"}, RepositoryID: "repo"}
	}
	m.rowCache = m.rows()
	m.cursor[1] = len(m.rowCache) - 1
	lines := resultWindow(m.rowCache, m.cursor[1], 0, 12, 64, m.styles)
	if len(lines) > 12 {
		t.Fatal("unbounded render window")
	}
	visible := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(visible, "› ") {
		t.Fatal("focused row disappeared from the window")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 64 {
			t.Fatal("row overflowed terminal width")
		}
	}
}
func BenchmarkNoseyLargeResultView(b *testing.B) {
	m := productModel()
	m.tab = 1
	m.filters[1].Query = "skill"
	for i := 0; i < 2000; i++ {
		id := fmt.Sprintf("skill-%04d", i)
		m.session.Catalog.Skills[id] = domain.Skill{ID: id, Name: id, Technologies: []string{"a"}, RepositoryID: "repo"}
	}
	m.rowCache = m.rows()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func TestSpacedFormsKeepLastFieldVisibleAndNoColorIsRespected(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := productModel()
	labels, values := []string{}, []string{}
	for i := 0; i < 10; i++ {
		labels = append(labels, fmt.Sprintf("Field %d", i))
		values = append(values, "")
	}
	m.openForm("filters", labels, values)
	for i := 0; i < 9; i++ {
		m = productUpdate(t, m, keypress(tea.KeyTab))
	}
	screen := m.View().Content
	if !strings.Contains(ansi.Strip(screen), "Field 9") {
		t.Fatal("focused form field scrolled out of view")
	}
	if strings.Contains(screen, "38;2;") || strings.Contains(screen, "38;5;") {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestGroupSelectionIncludesCollapsedChildrenAndSkipsInstalled(t *testing.T) {
	m := productModel()
	m.tab = 1
	m.filters[1].Query = "Alpha"
	m.session.Catalog.Skills["t"] = domain.Skill{ID: "t", Name: "Second", Technologies: []string{"a"}, RepositoryID: "other"}
	m.session.Catalog.Skills["installed"] = domain.Skill{ID: "installed", Name: "Installed", Technologies: []string{"a"}, RepositoryID: "other"}
	m.session.Catalog.InstallStatus = map[string]string{"installed": "installed"}
	m.collapsed["tech:a"] = true
	for i, row := range m.rows() {
		if row.id == "tech:a" {
			m.cursor[1] = i
			break
		}
	}
	m = productUpdate(t, m, keypress(tea.KeySpace))
	if len(m.selected) != 2 || !m.selected["s"] || !m.selected["t"] || m.selected["installed"] {
		t.Fatal("group did not select eligible collapsed children", m.selected)
	}
	m.collapsed["tech:a"] = false
	m.rowCache = nil
	for i, row := range m.rows() {
		if row.id == "repo:a:other" {
			m.cursor[1] = i
			break
		}
	}
	m = productUpdate(t, m, keypress(tea.KeySpace))
	if !m.selected["s"] || m.selected["t"] {
		t.Fatal("repository selection affected another subgroup", m.selected)
	}
	mixed := false
	for _, row := range m.rows() {
		if row.id == "tech:a" {
			mixed = strings.Contains(row.label, "[-]")
		}
	}
	if !mixed {
		t.Fatal("partially selected technology lacks mixed state")
	}
	m.toggleGroup("tech:a")
	m.toggleGroup("tech:a")
	if len(m.selected) != 0 {
		t.Fatal("group deselection left duplicate selections", m.selected)
	}
}

func TestInstalledSkillCheckboxInScanAndSearch(t *testing.T) {
	for tab, name := range []string{"scan", "search"} {
		t.Run(name, func(t *testing.T) {
			m := productModel()
			m.tab = tab
			m.filters[1].Query = "Shared"
			m.selected["s"] = true
			skill := m.session.Catalog.Skills["s"]
			skill.Source.SkillName = "Shared"
			m.session.Catalog.Skills["s"] = skill
			id := "s"
			updated, _ := m.Update(noseyInventory{generation: m.generation, inventory: installation.Inventory{
				Complete: true,
				Skills:   []installation.InventoryItem{{SkillID: &id, Name: "Shared", Status: "installed"}},
			}})
			var ok bool
			m, ok = updated.(noseyModel)
			if !ok {
				t.Fatal("unexpected model after inventory refresh")
			}
			found := false
			for i, row := range m.rows() {
				if row.kind == "skill" || row.kind == "group" {
					if !strings.Contains(row.label, "[x]") {
						t.Fatalf("installed result is unchecked: %s", row.label)
					}
				}
				if row.kind == "skill" {
					found = true
					m.cursor[tab] = i
				}
			}
			if !found {
				t.Fatal("installed skill missing from results")
			}
			for _, key := range []rune{tea.KeySpace, 'a'} {
				m = productUpdate(t, m, keypress(key))
				if len(m.selected) != 0 {
					t.Fatal("installed skill queued for installation", m.selected)
				}
				for _, row := range m.rows() {
					if row.kind == "skill" && !strings.Contains(row.label, "[x]") {
						t.Fatalf("installed skill became unchecked: %s", row.label)
					}
				}
			}
		})
	}
}

func TestResultCheckboxStates(t *testing.T) {
	for _, tt := range []struct {
		status   string
		selected bool
		mark     string
	}{
		{"installed", false, "[x]"},
		{"installed", true, "[x]"},
		{"missing", false, "[ ]"},
		{"outdated", false, "[ ]"},
		{"unknown", false, "[ ]"},
		{"missing", true, "[x]"},
	} {
		t.Run(fmt.Sprintf("%s/selected=%t", tt.status, tt.selected), func(t *testing.T) {
			skill := domain.SkillView{ID: "s", Name: "Skill", Status: tt.status}
			if label := skillLabel(skill, tt.selected); !strings.HasPrefix(label, tt.mark) {
				t.Fatalf("skill label = %q, want %s", label, tt.mark)
			}
			if mark := selectionMark([]domain.SkillView{skill}, map[string]bool{"s": tt.selected}); mark != tt.mark {
				t.Fatalf("group mark = %q, want %s", mark, tt.mark)
			}
		})
	}
	if mark := selectionMark([]domain.SkillView{{ID: "s", Status: "installed"}, {ID: "t", Status: "missing"}}, nil); mark != "[-]" {
		t.Fatalf("partially installed group mark = %q, want [-]", mark)
	}
}

func TestTechnologyStripIsHorizontalBorderedAndNotSelectable(t *testing.T) {
	m := productModel()
	names := []string{"Alpha", "Beta", "Very long technology name"}
	wide := technologyStrip("Detected technologies 3/3", names, 80, m.styles)
	if len(wide) != 4 || !strings.Contains(ansi.Strip(wide[2]), "Alpha · Beta · Very long technology name") {
		t.Fatal("technology list is not horizontal", wide)
	}
	if !strings.Contains(ansi.Strip(wide[1]), "╭") || !strings.Contains(ansi.Strip(wide[3]), "╰") {
		t.Fatal("strip has no border")
	}
	for _, line := range technologyStrip("Detected technologies 3/3", names, 24, m.styles) {
		if ansi.StringWidth(line) > 24 {
			t.Fatal("strip exceeds terminal width", line)
		}
	}
	for _, tab := range []int{0, 1} {
		m.tab = tab
		for _, row := range m.rows() {
			if row.kind == "technology" || row.id == "technologies" {
				t.Fatal("technologies are selectable rows", row)
			}
		}
	}
	m.tab = 0
	m.cursor[0] = 0
	for _, key := range []rune{tea.KeyRight, tea.KeyLeft, tea.KeyEnter} {
		m = productUpdate(t, m, keypress(key))
		if m.modal != "" {
			t.Fatal("technology strip took focus", key)
		}
	}
	if !strings.Contains(screenText(m), "Detected technologies") {
		t.Fatal("strip no longer rendered")
	}
}

func TestSortSelectAppliesValidChoicesAndEscapeCancels(t *testing.T) {
	for target := 0; target < 2; target++ {
		m := productModel()
		if target == 1 {
			m.tab = 1
			m.filters[1].Query = "Shared"
		}
		original := m.sort[target]
		m = productUpdate(t, m, keypress('s'))
		if m.modal != "sort" || m.sortTarget != target {
			t.Fatal("sort did not open a select for the active pane")
		}
		m = productUpdate(t, m, keypress(tea.KeyDown))
		if m.sort[target] != original {
			t.Fatal("navigation committed sort before confirmation")
		}
		choice := sortChoices(target)[m.sortChoice].id
		m = productUpdate(t, m, keypress(tea.KeyEnter))
		if m.sort[target] != choice || m.modal != "" {
			t.Fatal("sort selection was not applied")
		}
		m = productUpdate(t, m, keypress('s'))
		tab := m.tab
		m = productUpdate(t, m, keypress(tea.KeyTab))
		if m.tab != tab {
			t.Fatal("select leaked Tab to page navigation")
		}
		m = productUpdate(t, m, keypress(tea.KeyEsc))
		if m.sort[target] != choice || m.modal != "" {
			t.Fatal("cancel changed the sort")
		}
		for _, option := range sortChoices(target) {
			if err := catalog.ValidateSort(option.id, false, target == 1); err != nil {
				t.Fatal("select exposed invalid sort", err)
			}
		}
	}
}

func TestStaleOperationResultsDoNotReplaceCurrentState(t *testing.T) {
	m := productModel()
	m.generation = 3
	m.loading = true
	m.modal = "sort"
	updated, cmd := m.Update(noseyOperation{generation: 2, message: "done"})
	next, ok := updated.(noseyModel)
	if !ok {
		t.Fatal("unexpected model type")
	}
	if cmd != nil || next.modal != "sort" || !next.loading || next.generation != 3 {
		t.Fatal("stale operation disturbed the newer load or modal")
	}
	updated, _ = m.Update(installReady{generation: 2})
	next, ok = updated.(noseyModel)
	if !ok || next.modal != "sort" || !next.loading {
		t.Fatal("stale installer result disturbed the newer load or modal")
	}
}

func TestCollapseReposSettingCollapsesRepositoriesButHonorsToggles(t *testing.T) {
	repoRows := func(m noseyModel) (repos, skills int) {
		for _, row := range m.rows() {
			if row.kind == "skill" {
				skills++
			}
			if row.kind == "group" && strings.HasPrefix(row.id, "repo:") {
				repos++
			}
		}
		return repos, skills
	}
	m := productModel()
	if repos, skills := repoRows(m); repos != 1 || skills != 1 {
		t.Fatal("repositories are not expanded by default", repos, skills)
	}
	m.session.Resolved.Settings.UI.CollapseRepos = true
	repos, skills := repoRows(m)
	if repos != 1 || skills != 0 {
		t.Fatal("repository group was not collapsed by default", repos, skills)
	}
	for i, row := range m.rows() {
		if row.kind == "group" && strings.HasPrefix(row.id, "repo:") {
			m.cursor[0] = i
		}
	}
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if _, skills = repoRows(m); skills != 1 {
		t.Fatal("Enter did not expand a default-collapsed repository", skills)
	}
	if groups := len(m.rows()); groups < 3 {
		t.Fatal("technology group must stay expanded", groups)
	}
}

func TestCollapseReposSettingIsSavedAndResetsToggles(t *testing.T) {
	m := editableSettingsModel(t)
	m.collapsed["repo:a:repo"] = false
	m, _ = m.setting(noseyRow{kind: "setting", id: "collapse"})
	m.form.inputs[0].SetValue("true")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	resolved, err := m.session.Store.Resolve()
	if err != nil || !resolved.Settings.UI.CollapseRepos || !m.session.Resolved.Settings.UI.CollapseRepos || len(m.collapsed) != 0 {
		t.Fatal("setting not saved", err, m.collapsed)
	}
}

func TestUpdateCheckMarksInstalledSkillsAndSurvivesInventoryRefresh(t *testing.T) {
	m := productModel()
	m.tab = 2
	m.session.Store = settings.Store{Workspace: t.TempDir()}
	m = productUpdate(t, m, keypress('u'))
	if !m.loading || m.busyLabel == "" || !strings.Contains(screenText(m), "Checking installed skills for updates") {
		t.Fatal("update check did not show progress", m.busyLabel)
	}
	generation := m.generation
	m, cmd := updateModel(t, m, noseyUpdates{generation: generation, check: installation.UpdateCheck{Names: []string{"alpha"}}})
	if m.loading || cmd == nil || !strings.Contains(m.message, "alpha") || !slices.Equal(m.updates, []string{"alpha"}) {
		t.Fatal("update result not applied", m.message)
	}
	id := "s"
	skill := m.session.Catalog.Skills[id]
	skill.Source.SkillName = "alpha"
	m.session.Catalog.Skills[id] = skill
	inv := installation.Inventory{Complete: true, Skills: []installation.InventoryItem{{SkillID: &id, Name: "alpha", Status: "installed"}, {Name: "beta", Status: "installed"}}}
	m, _ = updateModel(t, m, noseyInventory{generation: generation, inventory: inv})
	if m.inventory.Skills[0].Status != "outdated" || m.inventory.Skills[1].Status != "installed" || m.session.Catalog.InstallStatus["s"] != "outdated" {
		t.Fatal("outdated status not applied", m.inventory.Skills)
	}
	if !strings.Contains(m.rows()[0].label, "update available") {
		t.Fatal(m.rows()[0])
	}
	m, _ = updateModel(t, m, noseyUpdates{generation: generation, err: errors.New("offline")})
	if m.err == nil || m.loading {
		t.Fatal("failed check not reported")
	}
}

func TestUpdateCheckIgnoresRepeatKeyWhileLoading(t *testing.T) {
	m := productModel()
	m.tab = 2
	m.session.Store = settings.Store{Workspace: t.TempDir()}
	m = productUpdate(t, m, keypress('u'))
	next, cmd := updateModel(t, m, keypress('u'))
	if cmd != nil {
		t.Fatal("second key started another check")
	}
	if !next.loading || next.busyLabel == "" {
		t.Fatal("second key disturbed the running check")
	}
}

func TestUpdateCheckReportsIncompleteResults(t *testing.T) {
	m := productModel()
	m.tab = 2
	m.loading = true
	check := installation.UpdateCheck{Names: []string{"alpha"}, Failures: []string{"Failed to check skills from o/two"}}
	m, _ = updateModel(t, m, noseyUpdates{generation: m.generation, check: check})
	if !strings.Contains(m.message, "alpha") || !strings.Contains(m.warning, "o/two") || !strings.Contains(screenText(m), "o/two") {
		t.Fatal("partial check not surfaced", m.message, m.warning)
	}
	m.loading = true
	m, _ = updateModel(t, m, noseyUpdates{generation: m.generation, check: installation.UpdateCheck{NothingToCompare: true}})
	if strings.Contains(m.message, "up to date") || m.warning != "" {
		t.Fatal("uncompared skills reported as current", m.message, m.warning)
	}
}

func TestBusyLabelEndsWithItsLoad(t *testing.T) {
	m := productModel()
	m.loading, m.busyLabel = true, "Checking installed skills for updates…"
	m, _ = updateModel(t, m, noseyOperation{generation: m.generation, message: "done"})
	if !m.loading || m.busyLabel != "" {
		t.Fatal("newer load kept the old busy label", m.loading, m.busyLabel)
	}
	m.busyLabel = "Checking installed skills for updates…"
	m, _ = updateModel(t, m, noseyUpdates{generation: m.generation - 1})
	if !m.loading || m.busyLabel == "" {
		t.Fatal("stale result touched the current load")
	}
	m, _ = updateModel(t, m, noseyLoaded{generation: m.generation, err: errors.New("offline")})
	if m.loading || m.busyLabel != "" || !strings.Contains(screenText(m), "offline") {
		t.Fatal("busy label outlived its load", m.loading, m.busyLabel)
	}
}

func TestTechnologyStripStaysOnOneLineAndFullListIsReachable(t *testing.T) {
	m := productModel()
	names := []string{}
	m.session.Catalog.Technologies = map[string]domain.Technology{}
	m.scan.Technologies = nil
	for i := range 30 {
		id, name := fmt.Sprintf("t%02d", i), fmt.Sprintf("Technology %02d", i)
		names = append(names, name)
		m.session.Catalog.Technologies[id] = domain.Technology{ID: id, Name: name}
		m.scan.Technologies = append(m.scan.Technologies, domain.Detection{ID: id})
	}
	for _, width := range []int{78, 40, 24} {
		if lines := technologyStrip("Detected", names, width, m.styles); len(lines) != 4 {
			t.Fatalf("width %d wrapped to %d lines", width, len(lines))
		}
	}
	m = productUpdate(t, m, keypress('t'))
	if m.modal != "details" || !strings.Contains(m.details, "Technology 00") || !strings.Contains(m.details, "Technology 29") {
		t.Fatal("full technology list not reachable", m.modal, m.details)
	}
	m.modal, m.loading = "", true
	m = productUpdate(t, m, keypress('t'))
	if m.modal != "" {
		t.Fatal("technology list opened during a load")
	}
}

func scoredModel() noseyModel {
	m := productModel()
	hi, lo := 90.0, 40.0
	m.session.Catalog.Skills = map[string]domain.Skill{
		"hi":   {ID: "hi", Name: "High", Technologies: []string{"a"}, RepositoryID: "repo", Score: &hi},
		"lo":   {ID: "lo", Name: "Low", Technologies: []string{"a"}, RepositoryID: "repo", Score: &lo},
		"none": {ID: "none", Name: "None", Technologies: []string{"a"}, RepositoryID: "repo"},
	}
	m.scan.Skills = []domain.SkillView{
		{ID: "hi", Name: "High", Status: "unknown", Priority: "optional", Technologies: []string{"a"}, RepositoryID: "repo", Score: &hi},
		{ID: "lo", Name: "Low", Status: "unknown", Priority: "optional", Technologies: []string{"a"}, RepositoryID: "repo", Score: &lo},
		{ID: "none", Name: "None", Status: "unknown", Priority: "optional", Technologies: []string{"a"}, RepositoryID: "repo"},
	}
	m.scan.SkillTotal = 3
	return m
}
func skillRowIDs(m noseyModel) []string {
	ids := []string{}
	for _, row := range m.rows() {
		if row.kind == "skill" {
			ids = append(ids, row.id)
		}
	}
	return ids
}
func rowText(m noseyModel) string {
	text := []string{}
	for _, row := range m.rows() {
		text = append(text, row.label)
	}
	return strings.Join(text, "\n")
}

func TestMinScoreSettingHidesLowResultsAndReportsCountOnScan(t *testing.T) {
	m := scoredModel()
	if ids := skillRowIDs(m); len(ids) != 3 || strings.Contains(rowText(m), "hidden below") {
		t.Fatal("threshold off must show everything silently", ids)
	}
	m.session.Resolved.Settings.UI.MinScore = 50
	if ids := skillRowIDs(m); !slices.Equal(ids, []string{"hi"}) {
		t.Fatal("low and unscored skills must be hidden", ids)
	}
	if text := rowText(m); !strings.Contains(text, "1/3") || !strings.Contains(text, "2 hidden below score 50") {
		t.Fatal("results page does not report filtered count", text)
	}
	m.session.Resolved.Settings.UI.MinScore = 100
	if text := rowText(m); !strings.Contains(text, "3 hidden below score 100") || !strings.Contains(text, "Relax the score/quality settings") {
		t.Fatal("all-hidden state does not explain the threshold", text)
	}
}

func TestMinScoreSettingAppliesToExploreAndKeepsStricterFilter(t *testing.T) {
	m := scoredModel()
	m.tab = 1
	m.filters[1].Query = "o"
	m.session.Resolved.Settings.UI.MinScore = 50
	if ids := skillRowIDs(m); !slices.Equal(ids, []string{"hi"}) {
		t.Fatal("explore did not apply threshold", ids)
	}
	if !strings.Contains(rowText(m), "hidden below score 50") {
		t.Fatal("explore does not report filtered count", rowText(m))
	}
	strict := 95.0
	m.filters[1].MinScore = &strict
	if ids := skillRowIDs(m); len(ids) != 0 {
		t.Fatal("stricter interactive filter must win over the setting", ids)
	}
}

func TestMinScoreSettingIsValidatedAndSaved(t *testing.T) {
	m := editableSettingsModel(t)
	m, _ = m.setting(noseyRow{kind: "setting", id: "minscore"})
	m.form.inputs[0].SetValue("abc")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.err == nil || m.modal != "form" || m.session.Resolved.Settings.UI.MinScore != 0 {
		t.Fatal("invalid score was accepted", m.err)
	}
	m.form.inputs[0].SetValue("101")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.err == nil || m.modal != "form" {
		t.Fatal("out-of-range score was accepted")
	}
	m.form.inputs[0].SetValue("60")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	resolved, err := m.session.Store.Resolve()
	if err != nil || resolved.Settings.UI.MinScore != 60 || m.session.Resolved.Settings.UI.MinScore != 60 || m.modal != "" {
		t.Fatal("score not saved", err, m.session.Resolved.Settings.UI.MinScore)
	}
}

func TestQualityAndRepoScoreSettingsHideAndReport(t *testing.T) {
	m := scoredModel()
	good, poor := "good", "fair"
	avgHi, avgLo := 80.0, 30.0
	for i := range m.scan.Skills {
		switch m.scan.Skills[i].ID {
		case "hi":
			m.scan.Skills[i].Quality, m.scan.Skills[i].RepositoryAverage = &good, &avgHi
		case "lo":
			m.scan.Skills[i].Quality, m.scan.Skills[i].RepositoryAverage = &poor, &avgLo
		}
	}
	m.session.Resolved.Settings.UI.MinQuality = "good"
	if ids := skillRowIDs(m); !slices.Equal(ids, []string{"hi"}) {
		t.Fatal("quality threshold must hide lower and unassessed skills", ids)
	}
	if text := rowText(m); !strings.Contains(text, "2 hidden below quality good") {
		t.Fatal("quality count missing", text)
	}
	m.session.Resolved.Settings.UI.MinQuality = "excellent"
	if ids := skillRowIDs(m); len(ids) != 0 {
		t.Fatal("excellent threshold must hide good skills", ids)
	}
	m.session.Resolved.Settings.UI.MinQuality = "any"
	m.session.Resolved.Settings.UI.MinRepoScore = 50
	if ids := skillRowIDs(m); !slices.Equal(ids, []string{"hi"}) {
		t.Fatal("repo score threshold not applied", ids)
	}
	m.session.Resolved.Settings.UI.MinScore = 50
	m.session.Resolved.Settings.UI.MinQuality = "good"
	if text := rowText(m); !strings.Contains(text, "2 hidden below score 50, repo score 50, quality good") {
		t.Fatal("combined notice missing", text)
	}
	m.filters[0].Qualities = []string{"fair"}
	if ids := skillRowIDs(m); len(ids) != 0 {
		t.Fatal("interactive quality filter must intersect with the setting", ids)
	}
}

func TestQualityAndRepoScoreSettingsAreValidatedAndSaved(t *testing.T) {
	m := editableSettingsModel(t)
	m, _ = m.setting(noseyRow{kind: "setting", id: "minrepo"})
	m.form.inputs[0].SetValue("-1")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.err == nil || m.modal != "form" {
		t.Fatal("invalid repo score accepted")
	}
	m.form.inputs[0].SetValue("70")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	m, _ = m.setting(noseyRow{kind: "setting", id: "minquality"})
	if len(m.form.choices[0]) != 4 {
		t.Fatal("quality is not a fixed choice", m.form.choices[0])
	}
	m.form.inputs[0].SetValue("good")
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	resolved, err := m.session.Store.Resolve()
	if err != nil || resolved.Settings.UI.MinRepoScore != 70 || resolved.Settings.UI.MinQuality != "good" || m.session.Resolved.Settings.UI.MinQuality != "good" {
		t.Fatal("settings not saved", err, resolved.Settings.UI)
	}
}
