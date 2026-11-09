package ui

import (
	"context"
	"fmt"
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
	m.filters[2].Query = "Shared"
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
	m.filters[2].Query = "no-match"
	if len(m.selected) != 1 {
		t.Fatal("filter cleared hidden selection")
	}
	m.session.Resolved.Settings = settings.Defaults()
	m.session.Resolved.Settings.Installation.Agents = []string{"codex"}
	skill := m.session.Catalog.Skills["s"]
	skill.Source = domain.Source{Kind: "git", URI: "owner/repo", SkillName: "shared"}
	m.session.Catalog.Skills["s"] = skill
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "install" || m.err != nil || len(m.install.commands) != 1 || m.install.commands[0].Skills[0].ID != "s" {
		t.Fatal("hidden selection did not reach the install page", m.modal, m.err)
	}
}
func TestNoseyScanFocusTargetsIndependentFilterState(t *testing.T) {
	m := productModel()
	m = productUpdate(t, m, keypress(tea.KeyRight))
	if m.section != 1 {
		t.Fatal("right did not focus recommendations")
	}
	m = productUpdate(t, m, keypress('f'))
	m.form.inputs[0].SetValue("skill-query")
	updated, _ := m.submitForm()
	m = updated
	if m.filters[1].Query != "skill-query" || m.filters[0].Query != "" {
		t.Fatal("independent Scan filters were conflated")
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
func TestSharedResultTreeShowsBadgesAndKeepsEvidenceInDetails(t *testing.T) {
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
		m.filters[2].Query = "Shared"
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
	m.tab = 0
	m.rowCache = nil
	for i, row := range m.rows() {
		if row.kind == "technology" {
			m.cursor[0] = i
			break
		}
	}
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.modal != "details" || !strings.Contains(m.details, "private-evidence/package.json") {
		t.Fatal("evidence is no longer accessible in details")
	}
}
func TestLargeResultWindowBoundsRenderingAndRetainsFocus(t *testing.T) {
	m := productModel()
	m.tab = 1
	m.filters[2].Query = "skill"
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
	m.filters[2].Query = "skill"
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
	m.filters[2].Query = "Alpha"
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
			m.filters[2].Query = "Shared"
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

func TestTechnologyStripIsHorizontalBorderedAndKeepsFocusVisible(t *testing.T) {
	m := productModel()
	rows := []noseyRow{{"heading", "technologies", "Detected technologies 3/3"}, {"technology", "a", "Alpha"}, {"technology", "b", "Beta"}, {"technology", "c", "Very long technology name"}}
	wide := technologyStrip(rows, 2, 80, m.styles)
	if len(wide) != 4 || !strings.Contains(ansi.Strip(wide[2]), "Alpha · › Beta · Very long technology name") {
		t.Fatal("technology list is not horizontal", wide)
	}
	if !strings.Contains(ansi.Strip(wide[1]), "╭") || !strings.Contains(ansi.Strip(wide[3]), "╰") {
		t.Fatal("strip has no border")
	}
	narrow := technologyStrip(rows, 3, 32, m.styles)
	if !strings.Contains(ansi.Strip(narrow[2]), "› Very long technology") {
		t.Fatal("focused technology scrolled out of strip", narrow)
	}
	for _, line := range narrow {
		if ansi.StringWidth(line) > 32 {
			t.Fatal("strip exceeds terminal width", line)
		}
	}
	m.scan.Technologies = append(m.scan.Technologies, domain.Detection{ID: "b"})
	m.cursor[0] = 1
	m = productUpdate(t, m, keypress(tea.KeyRight))
	if m.rows()[m.cursor[0]].id != "b" || m.section != 0 {
		t.Fatal("right arrow left the technology strip")
	}
	m = productUpdate(t, m, keypress(tea.KeyDown))
	if m.section != 1 {
		t.Fatal("down arrow did not enter recommendations")
	}
}

func TestSortSelectAppliesValidChoicesAndEscapeCancels(t *testing.T) {
	for target := 0; target < 3; target++ {
		m := productModel()
		m.section = target
		if target == 2 {
			m.tab = 1
			m.filters[2].Query = "Shared"
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
			if err := catalog.ValidateSort(option.id, target == 0, target == 2); err != nil {
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
