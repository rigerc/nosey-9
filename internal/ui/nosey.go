package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
)

type noseyLoaded struct {
	generation int
	session    app.Session
	scan       domain.ScanResult
	recs       catalog.Recommendations
	err        error
}
type noseyInventory struct {
	inventory  installation.Inventory
	generation int
}
type noseyPlan struct {
	plan       installation.Plan
	err        error
	generation int
}
type noseyOperation struct {
	message    string
	err        error
	generation int
}
type noseyRow struct{ kind, id, label string }
type noseyForm struct {
	kind        string
	labels      []string
	inputs      []textinput.Model
	index       int
	choices     map[int][]sortOption
	choiceIndex map[int]int
	multiple    map[int]bool
}
type noseyModel struct {
	ctx                         context.Context
	cwd, config                 string
	session                     app.Session
	scan                        domain.ScanResult
	recs                        catalog.Recommendations
	inventory                   installation.Inventory
	generation                  int
	loading                     bool
	err                         error
	tab, section, width, height int
	cursor                      [4]int
	filters                     [3]domain.Filters
	sort                        [3]string
	selected                    map[string]bool
	collapsed                   map[string]bool
	query                       textinput.Model
	search                      bool
	viewport                    viewport.Model
	modal                       string
	form                        noseyForm
	details, message            string
	plan                        installation.Plan
	extraApproved               bool
	action                      string
	busy                        spinner.Model
	rowCache                    []noseyRow
	modalOffset                 int
	sortChoice                  int
	sortTarget                  int
	copyReady                   bool
	styles                      noseyStyles
	displayMode                 string
	settingsAnchor              string
	refreshOnStart              bool
}

// RunNosey opens the single-column four-tab product interface.
// When refresh is true the initial registry load bypasses the TTL window.
func RunNosey(ctx context.Context, cwd, config, mode string, refresh bool) error {
	if mode != "inline" && mode != "fullscreen" {
		return fmt.Errorf("invalid UI display mode")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := newNoseyModel(ctx, cwd, config)
	model.displayMode = mode
	model.refreshOnStart = refresh
	_, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	return err
}
func newNoseyModel(ctx context.Context, cwd, config string) noseyModel {
	input := textinput.New()
	input.Prompt = "Search: "
	input.CharLimit = 200
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(14))
	m := noseyModel{ctx: ctx, cwd: cwd, config: config, width: 80, height: 24, loading: true, generation: 1, selected: map[string]bool{}, collapsed: map[string]bool{}, query: input, viewport: vp}
	m.displayMode = "fullscreen"
	m.styles = newNoseyStyles()
	m.busy = spinner.New(spinner.WithSpinner(spinner.Dot))
	for i := range m.filters {
		m.filters[i] = domain.DefaultFilters()
	}
	m.sort = [3]string{"name-asc", "priority-desc", "relevance"}
	return m
}
func (m noseyModel) load() tea.Cmd { return m.loadWith(false) }
func (m noseyModel) loadWith(refresh bool) tea.Cmd {
	generation := m.generation
	return func() tea.Msg {
		s, err := app.Open(m.ctx, m.cwd, m.config, refresh)
		if err != nil {
			return noseyLoaded{generation: generation, err: err}
		}
		scan, recs, err := s.Scan(m.ctx, domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
		return noseyLoaded{generation, s, scan, recs, err}
	}
}
func (m noseyModel) Init() tea.Cmd { return tea.Batch(m.loadWith(m.refreshOnStart), m.busy.Tick) }
func (m noseyModel) readInventory() tea.Cmd {
	session, generation := m.session, m.generation
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
		defer cancel()
		return noseyInventory{inventory: session.Installer.Inventory(ctx, session.Catalog), generation: generation}
	}
}
func (m noseyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(spinner.TickMsg); ok {
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.busy, cmd = m.busy.Update(msg)
		return m, cmd
	}
	m.rowCache = nil
	next, cmd := m.update(msg)
	if next.modal != m.modal {
		next.modalOffset = 0
	}
	if !m.loading && next.loading {
		cmd = tea.Batch(cmd, next.busy.Tick)
	}
	next.rowCache = nil
	next.rowCache = next.rows()
	return next, cmd
}
func (m noseyModel) update(msg tea.Msg) (noseyModel, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = value.Width
		m.height = value.Height
		if m.displayMode == "inline" {
			m.height = min(24, value.Height)
		}
		m.viewport.SetWidth(max(10, value.Width-2))
		m.viewport.SetHeight(max(1, m.height-10))
		return m, nil
	case noseyLoaded:
		if value.generation != m.generation {
			return m, nil
		}
		m.loading = false
		m.err = value.err
		if value.err == nil {
			m.session = value.session
			m.scan = value.scan
			m.recs = value.recs
			for id := range m.selected {
				if _, ok := m.session.Catalog.Skills[id]; !ok || m.session.Catalog.InstallStatus[id] == "installed" {
					delete(m.selected, id)
					m.message = "Unavailable selections were removed after refresh."
				}
			}
		}
		if value.err == nil {
			return m, m.readInventory()
		}
		return m, nil
	case noseyInventory:
		if value.generation != m.generation {
			return m, nil
		}
		m.inventory = value.inventory
		m.session.ApplyInventory(value.inventory)
		for i := range m.scan.Skills {
			status := m.session.Catalog.InstallStatus[m.scan.Skills[i].ID]
			if status == "" {
				status = "unknown"
			}
			m.scan.Skills[i].Status = status
			if status == "installed" {
				delete(m.selected, m.scan.Skills[i].ID)
			}
		}
		return m, nil
	case noseyPlan:
		if value.generation != m.generation {
			// A refresh or settings change superseded the state this plan was built from.
			return m, nil
		}
		m.loading = false
		m.err = value.err
		if value.err == nil {
			m.plan = value.plan
			m.copyReady = value.plan.CommandsReady() == nil
			m.modal = "plan"
			m.extraApproved = false
		}
		return m, nil
	case noseyOperation:
		if value.generation != m.generation {
			// Report the outcome, but leave the newer load, modal and loading state alone.
			m.err = value.err
			if value.err == nil {
				m.message = value.message
			}
			return m, nil
		}
		m.loading = false
		m.err = value.err
		m.message = ""
		if value.err == nil {
			m.message = value.message
			m.modal = ""
			m.generation++
			m.loading = true
			return m, m.load()
		}
		return m, m.readInventory()
	case tea.KeyPressMsg:
		key := value.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if key == "q" && !m.search && m.modal == "" {
			return m, tea.Quit
		}
		if m.loading && (m.modal != "" || !m.search && (key == "i" || key == "r" || key == "enter" || key == "a" || key == "n")) {
			return m, nil
		}
		if m.session.Store.UserPath == "" && m.err != nil && key != "q" && key != "r" {
			return m, nil
		}
		if m.modal != "" {
			return m.updateModal(value)
		}
		if key == "tab" || key == "shift+tab" {
			m.search = false
			m.query.Blur()
			delta := 1
			if key == "shift+tab" {
				delta = 3
			}
			m.tab = (m.tab + delta) % 4
			if m.tab == 1 {
				m.query.SetValue(m.filters[2].Query)
				m.search = true
				return m, m.query.Focus()
			}
			if m.tab == 2 && m.session.Store.Workspace != "" {
				return m, m.readInventory()
			}
			return m, nil
		}
		if m.search {
			if m.tab == 1 && (key == "enter" || key == "down") {
				m.search = false
				m.query.Blur()
				m.cursor[1] = 0
				for i, row := range m.rows() {
					if row.kind == "group" || row.kind == "skill" || row.kind == "registry-overview" {
						m.cursor[1] = i
						break
					}
				}
				return m, nil
			}
			if key == "esc" {
				m.search = false
				m.query.Blur()
				return m, nil
			}
			var command tea.Cmd
			m.query, command = m.query.Update(msg)
			i := m.activeFilter()
			m.filters[i].Query = m.query.Value()
			m.filters[i].QueryTarget = "both"
			return m, command
		}
		rows := m.rows()
		cursor := min(m.cursor[m.tab], max(0, len(rows)-1))
		m.cursor[m.tab] = cursor
		switch key {
		case "q":
			return m, tea.Quit
		case "up", "k":
			m.cursor[m.tab] = max(0, cursor-1)
		case "down", "j":
			m.cursor[m.tab] = min(max(0, len(rows)-1), cursor+1)
			if m.tab == 0 && len(rows) > 0 && rows[cursor].kind == "technology" {
				for i := cursor + 1; i < len(rows); i++ {
					if rows[i].kind != "technology" {
						m.cursor[0] = i
						break
					}
				}
			}
		case "/":
			if m.tab < 2 {
				m.search = true
				m.query.SetValue(m.filters[m.activeFilter()].Query)
				m.query.Focus()
				return m, textinput.Blink
			}
		case "left", "right":
			if m.tab == 0 && len(rows) > 0 && rows[cursor].kind == "technology" {
				delta := 1
				if key == "left" {
					delta = -1
				}
				next := cursor + delta
				if next >= 0 && next < len(rows) && rows[next].kind == "technology" {
					m.cursor[0] = next
				}
				break
			}
			if m.tab == 0 && (len(rows) == 0 || rows[cursor].kind != "group") {
				if key == "left" {
					m.section = 0
				} else {
					m.section = 1
				}
				for i, row := range rows {
					if m.section == 0 && row.kind == "technology" || m.section == 1 && row.kind == "skill" {
						m.cursor[m.tab] = i
						break
					}
				}
			} else if m.tab < 2 && len(rows) > 0 && rows[cursor].kind == "group" {
				m.collapsed[rows[cursor].id] = key == "left"
			}
		case "space":
			if len(rows) > 0 && rows[cursor].kind == "group" {
				m.toggleGroup(rows[cursor].id)
			}
			if len(rows) > 0 && rows[cursor].kind == "skill" && m.session.Catalog.InstallStatus[rows[cursor].id] != "installed" {
				id := rows[cursor].id
				m.selected[id] = !m.selected[id]
				if !m.selected[id] {
					delete(m.selected, id)
				}
			}
		case "a":
			if m.tab < 2 {
				all := true
				for _, row := range rows {
					if row.kind == "skill" && m.session.Catalog.InstallStatus[row.id] != "installed" && !m.selected[row.id] {
						all = false
					}
				}
				for _, row := range rows {
					if row.kind == "skill" && m.session.Catalog.InstallStatus[row.id] != "installed" {
						if all {
							delete(m.selected, row.id)
						} else {
							m.selected[row.id] = true
						}
					}
				}
			} else if m.tab == 3 {
				m.openForm("add", []string{"Registry ID", "Absolute local path or Git URL"}, []string{"", ""})
			}
		case "x":
			if m.tab < 2 {
				m.filters[m.activeFilter()] = domain.DefaultFilters()
			}
		case "r":
			if m.tab == 2 {
				return m, m.readInventory()
			}
			m.generation++
			m.loading = true
			return m, m.loadWith(true)
		case "f":
			if m.tab < 2 {
				f := m.filters[m.activeFilter()]
				m.openForm("filters", []string{"Query", "Technology IDs", "Registry IDs", "Repository IDs", "Status", "Quality", "Minimum score", "Minimum repository average", "Priority", "Score state"}, []string{f.Query, strings.Join(f.Technologies, ","), strings.Join(f.Registries, ","), strings.Join(f.Repositories, ","), strings.Join(f.Statuses, ","), strings.Join(f.Qualities, ","), number(f.MinScore), number(f.MinRepositoryAverage), strings.Join(f.Priorities, ","), f.ScoreState})
			}
		case "s":
			if m.tab < 2 {
				m.openSort()
			}
		case "i":
			if m.tab < 2 && len(m.selected) > 0 {
				return m.installFromSettings()
			}
		case "n":
			if m.tab == 3 {
				m.openNewRegistryForm()
			}
		case "?":
			m.modal = "details"
			m.details = "Tab / Shift+Tab: change section\nArrows: navigate; ←/→: technology strip or group collapse\nSpace: select skill or group; Enter: expand/collapse group\nLeft/right: navigate technologies; Down: recommendations\na: select visible skills\nf: filters; s: sort; /: search; x: clear filters\ni: review all selected skills, including hidden selections\nr: refresh registries (or inventory on the Installed tab); q: quit; Escape: cancel dialog\nSettings: Enter edits; a adds existing; n creates registry"
		case "enter":
			if len(rows) == 0 {
				break
			}
			row := rows[cursor]
			switch row.kind {
			case "group":
				m.collapsed[row.id] = !m.collapsed[row.id]
			case "technology":
				for _, t := range m.scan.Technologies {
					if t.ID == row.id {
						data, _ := json.MarshalIndent(t.Evidence, "", "  ")
						m.details = string(data)
						m.modal = "details"
					}
				}
			case "skill":
				s := m.session.Catalog.Skills[row.id]
				m.details = s.Name + "\n" + s.Description + "\nSource: " + s.Source.URI + s.Source.Path + "\nSkill name: " + s.Source.SkillName + "\nRegistry: " + s.RegistryID
				m.modal = "details"
			case "registry-overview":
				m.details = m.registryOverviewDetails(row.id)
				m.modal = "details"
			case "inventory":
				for _, item := range m.inventory.Skills {
					if item.Name != row.id {
						continue
					}
					data, _ := json.MarshalIndent(item, "", "  ")
					m.details = string(data)
					m.modal = "details"
					break
				}
			case "setting", "registry", "pending":
				return m.setting(row)
			}
		}
	}
	if m.tab == 0 {
		rows := m.rows()
		if m.cursor[0] < len(rows) {
			row := rows[m.cursor[0]]
			if row.kind == "skill" || row.kind == "group" || row.kind == "heading" && row.id == "skills" {
				m.section = 1
			} else if row.kind == "technology" || row.kind == "heading" && row.id == "technologies" {
				m.section = 0
			}
		}
	}
	return m, nil
}
