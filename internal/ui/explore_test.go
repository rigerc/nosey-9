package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/domain"
)

func TestExploreLandingShowsSearchAndRegistryOverview(t *testing.T) {
	m := productModel()
	m.tab = 1
	m.session.Resolved.Settings.Registries = []domain.RegistryRef{
		{ID: "official", Enabled: true, Source: domain.RegistrySource{Kind: "git", URI: "https://github.com/owner/registry", Ref: "main"}},
		{ID: "disabled", Enabled: false, Source: domain.RegistrySource{Kind: "local", Path: "/disabled"}},
	}
	m.session.Resolved.Pending = []domain.RegistryRef{{ID: "workspace:proposal", Enabled: true, Source: domain.RegistrySource{Kind: "local", Path: "/pending"}}}
	m.session.Catalog.Roots = map[string]string{"official": "/cache/registry"}
	skill := m.session.Catalog.Skills["s"]
	skill.RegistryID = "official"
	m.session.Catalog.Skills["s"] = skill
	for _, row := range m.rows() {
		if row.kind == "skill" || row.kind == "group" {
			t.Fatal("empty Explore query dumped the skill catalog")
		}
	}
	screen := ansi.Strip(m.View().Content)
	for _, text := range []string{"Search:", "Registries", "official  [loaded]", "1 skills · 1 repositories", "disabled  [disabled]", "workspace:proposal  [awaiting approval]"} {
		if !strings.Contains(screen, text) {
			t.Fatal("overview omitted registry context", text, screen)
		}
	}
	m.search = true
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.rows()[m.cursor[1]].kind != "registry-overview" {
		t.Fatal("Enter from search did not focus a registry")
	}
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.modal != "details" || !strings.Contains(m.details, "https://github.com/owner/registry") || !strings.Contains(m.details, "Requested ref: main") {
		t.Fatal("registry details unavailable")
	}
}
func TestExploreSearchFocusAndResults(t *testing.T) {
	m := productModel()
	m = productUpdate(t, m, keypress(tea.KeyTab))
	if m.tab != 1 || !m.search {
		t.Fatal("Explore did not focus its search input")
	}
	m = productUpdate(t, m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.filters[2].Query != "s" {
		t.Fatal("typing did not update live search")
	}
	found := false
	for _, row := range m.rows() {
		if row.kind == "skill" {
			found = true
		}
	}
	if !found {
		t.Fatal("nonempty query did not show skill results")
	}
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	if m.search {
		t.Fatal("Enter did not release input focus for result navigation")
	}
	m = productUpdate(t, m, keypress('/'))
	if !m.search || m.query.Value() != "s" {
		t.Fatal("search could not regain focus with its query intact")
	}
}
