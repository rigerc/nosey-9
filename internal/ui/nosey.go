package ui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/storage"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
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
	plan installation.Plan
	err  error
}
type noseyOperation struct {
	message string
	err     error
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
}

// RunNosey opens the single-column four-tab product interface.
func RunNosey(ctx context.Context, cwd, config, mode string) error {
	if mode != "inline" && mode != "fullscreen" {
		return fmt.Errorf("invalid UI display mode")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := newNoseyModel(ctx, cwd, config)
	model.displayMode = mode
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
func (m noseyModel) load() tea.Cmd {
	generation := m.generation
	return func() tea.Msg {
		s, err := app.Open(m.ctx, m.cwd, m.config, false)
		if err != nil {
			return noseyLoaded{generation: generation, err: err}
		}
		scan, recs, err := s.Scan(m.ctx, domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
		return noseyLoaded{generation, s, scan, recs, err}
	}
}
func (m noseyModel) Init() tea.Cmd { return tea.Batch(m.load(), m.busy.Tick) }
func (m noseyModel) readInventory() tea.Cmd {
	session, generation := m.session, m.generation
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
		defer cancel()
		return noseyInventory{inventory: session.Installer.Inventory(ctx, session.Catalog), generation: generation}
	}
}
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
			return m, m.load()
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
			m.details = "Tab / Shift+Tab: change section\nArrows: navigate; ←/→: technology strip or group collapse\nSpace: select skill or group; Enter: expand/collapse group\nLeft/right: navigate technologies; Down: recommendations\na: select visible skills\nf: filters; s: sort; /: search; x: clear filters\ni: review all selected skills, including hidden selections\nr: refresh; q: quit; Escape: cancel dialog\nSettings: Enter edits; a adds existing; n creates registry"
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
func (m *noseyModel) openForm(kind string, labels, values []string) {
	m.modal = "form"
	m.details = ""
	m.form = noseyForm{kind: kind, labels: labels}
	for i, value := range values {
		input := textinput.New()
		input.SetValue(value)
		input.CharLimit = 512
		input.Prompt = ""
		input.SetWidth(60)
		if i == 0 {
			input.Focus()
		}
		m.form.inputs = append(m.form.inputs, input)
	}
	m.configureFormChoices()
}
func (m noseyModel) updateModal(key tea.KeyPressMsg) (noseyModel, tea.Cmd) {
	if key.String() == "esc" {
		m.modal = ""
		m.err = nil
		return m, nil
	}
	if m.modal == "sort" {
		return m.updateSort(key)
	}
	if m.modal == "details" {
		switch key.String() {
		case "up", "k":
			m.modalOffset = max(0, m.modalOffset-1)
		case "down", "j":
			m.modalOffset++
		case "pgdown":
			m.modalOffset += m.viewport.Height()
		case "pgup":
			m.modalOffset = max(0, m.modalOffset-m.viewport.Height())
		}
		return m, nil
	}
	if m.modal == "plan" {
		switch key.String() {
		case "a":
			m.extraApproved = !m.extraApproved
		case "c":
			if err := m.plan.CommandsReady(); err != nil {
				m.err = err
				return m, nil
			}
			return m, func() tea.Msg {
				lines := []string{}
				for _, cmd := range m.plan.Commands {
					lines = append(lines, installation.CommandText(cmd.Argv))
				}
				err := clipboard.WriteAll(strings.Join(lines, "\n"))
				return noseyOperation{message: "Commands copied", err: err}
			}
		case "enter":
			if m.action == "print" {
				return m, nil
			}
			if m.plan.Approval.Required && !m.extraApproved {
				m.err = fmt.Errorf("press a to explicitly approve the displayed source override before execution")
				return m, nil
			}
			approval := ""
			if m.plan.Approval.Digest != nil {
				approval = *m.plan.Approval.Digest
			}
			m.loading = true
			return m, func() tea.Msg {
				err := m.session.Installer.Execute(m.ctx, m.plan, approval, true)
				return noseyOperation{message: "Project installation verified", err: err}
			}
		}
		return m, nil
	}
	switch key.String() {
	case "tab", "shift+tab":
		m.form.inputs[m.form.index].Blur()
		delta := 1
		if key.String() == "shift+tab" {
			delta = len(m.form.inputs) - 1
		}
		m.form.index = (m.form.index + delta) % len(m.form.inputs)
		m.form.inputs[m.form.index].Focus()
		return m, textinput.Blink
	case "enter", "ctrl+s":
		return m.submitForm()
	default:
		if len(m.form.choices[m.form.index]) > 0 {
			return m.updateChoiceField(key)
		}
		var cmd tea.Cmd
		m.form.inputs[m.form.index], cmd = m.form.inputs[m.form.index].Update(key)
		return m, cmd
	}
}
func (m noseyModel) submitForm() (noseyModel, tea.Cmd) {
	values := []string{}
	for _, input := range m.form.inputs {
		values = append(values, input.Value())
	}
	m.err = nil
	switch m.form.kind {
	case "trust":
		if values[0] != m.session.Resolved.Fingerprint || values[0] == "" {
			m.err = fmt.Errorf("fingerprint does not match")
			return m, nil
		}
		if err := m.session.Store.Trust(values[0], false); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "registry":
		refs, err := m.session.Store.UserRegistries()
		if err != nil {
			m.err = err
			return m, nil
		}
		index := slices.IndexFunc(refs, func(r domain.RegistryRef) bool { return r.ID == values[0] })
		if index < 0 {
			m.err = fmt.Errorf("only user-owned registry entries can be edited here")
			return m, nil
		}
		if values[3] == "REMOVE" {
			refs = slices.Delete(refs, index, index+1)
		} else {
			enabled, err := strconv.ParseBool(values[1])
			if err != nil {
				m.err = err
				return m, nil
			}
			priority, err := strconv.Atoi(values[2])
			if err != nil {
				m.err = err
				return m, nil
			}
			refs[index].Enabled = enabled
			refs[index].Priority = priority
			ttl, err := strconv.Atoi(values[6])
			if err != nil {
				m.err = err
				return m, nil
			}
			refs[index].TTLHours = ttl
			if refs[index].Source.Kind == "local" {
				refs[index].Source.Path = values[4]
			} else {
				if err := registry.ValidateGit(values[4], values[5]); err != nil {
					m.err = err
					return m, nil
				}
				refs[index].Source.URI, refs[index].Source.Ref = values[4], values[5]
			}
		}
		if err = m.session.Store.Registries(refs); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "filters":
		f := m.filters[m.activeFilter()]
		f.Query = values[0]
		if m.tab == 0 && m.section == 1 {
			f.RecommendationTechnologies = csv(values[1])
		} else {
			f.Technologies = csv(values[1])
		}
		f.Registries = csv(values[2])
		f.Repositories = csv(values[3])
		f.Statuses = csv(values[4])
		f.Qualities = csv(values[5])
		var err error
		f.MinScore, err = parseNumber(values[6])
		if err != nil {
			m.err = err
			return m, nil
		}
		f.MinRepositoryAverage, err = parseNumber(values[7])
		if err != nil {
			m.err = err
			return m, nil
		}
		f.Priorities = csv(values[8])
		f.ScoreState = values[9]
		if !slices.Contains([]string{"any", "scored", "unscored"}, f.ScoreState) {
			m.err = fmt.Errorf("score state must be any, scored or unscored")
			return m, nil
		}
		if err := catalog.ValidateFilters(m.session.Catalog, f); err != nil {
			m.err = err
			return m, nil
		}
		m.filters[m.activeFilter()] = f
		m.modal = ""
	case "setting":
		key := m.form.labels[0]
		data := []byte(values[0])
		switch key {
		case "installation.agents":
			data, _ = json.Marshal(csv(values[0]))
		case "installation.method", "installation.defaultMode", "installation.upstreamTelemetry", "ui.mode":
			data, _ = json.Marshal(values[0])
		case "detection.exclude":
			data, _ = json.Marshal(csv(values[0]))
		}
		err := m.session.Store.Set(key, data, false)
		if err != nil {
			m.err = err
			return m, nil
		}
		if key == "ui.mode" {
			m.session.Resolved.Settings.UI.Mode = values[0]
			m.modal = ""
			m.message = "Display mode saved; applies next launch."
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "add":
		path := values[1]
		source := domain.RegistrySource{Kind: "git", URI: path}
		if strings.HasPrefix(path, "/") {
			source = domain.RegistrySource{Kind: "local", Path: path}
		} else if err := registry.ValidateGit(path, ""); err != nil {
			m.err = err
			return m, nil
		}
		refs, err := m.session.Store.UserRegistries()
		if err != nil {
			m.err = err
			return m, nil
		}
		refs = append(refs, domain.RegistryRef{ID: values[0], Source: source, Enabled: true, Priority: 20})
		if err = m.session.Store.Registries(refs); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "new":
		opts := registry.InitOptions{Path: values[0], RegistryID: values[1], Version: "0.1.0"}
		var err error
		opts.CI, err = strconv.ParseBool(values[2])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.Git, err = strconv.ParseBool(values[3])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.Register, err = strconv.ParseBool(values[4])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.GitHubRepo = strings.TrimSpace(values[5])
		opts.Public = values[6] == "public"
		if opts.GitHubRepo != "" && !registry.ValidGitHubRepo(opts.GitHubRepo) {
			m.err = fmt.Errorf("GitHub repository must be OWNER/NAME")
			return m, nil
		}
		m.modal = ""
		m.loading = true
		return m, func() tea.Msg {
			r, err := m.session.Registry.Init(m.ctx, m.session.Catalog, opts)
			message := "Created registry " + r.Path
			if err == nil && opts.GitHubRepo != "" {
				message += " and published https://github.com/" + opts.GitHubRepo
			}
			return noseyOperation{message: message, err: err}
		}
	}
	return m, nil
}

// openNewRegistryForm opens the registry-creation form shared by the shortcut and the settings row.
func (m *noseyModel) openNewRegistryForm() {
	m.openForm("new", []string{"New absolute registry path", "Template registry ID", "GitHub CI: true or false", "Initialize Git: true or false", "Register for this user: true or false", "GitHub repository: OWNER/NAME or blank", "GitHub repository visibility"}, []string{"", "nosey:official", "false", "false", "false", "", "private"})
	m.details = "Creates an empty registry. A GitHub repository needs git identity and an authenticated gh CLI."
}
func (m noseyModel) setting(row noseyRow) (noseyModel, tea.Cmd) {
	m.settingsAnchor = row.id
	switch row.id {
	case "display":
		m.openDisplaySelect()
	case "agents":
		m.openForm("setting", []string{"installation.agents"}, []string{strings.Join(m.session.Resolved.Settings.Installation.Agents, ",")})
	case "method":
		m.openForm("setting", []string{"installation.method"}, []string{m.session.Resolved.Settings.Installation.Method})
	case "ttl":
		m.openForm("setting", []string{"updates.ttlHours"}, []string{strconv.Itoa(m.session.Resolved.Settings.Updates.TTL)})
	case "mcp":
		m.openForm("setting", []string{"detection.enableMcpPackage"}, []string{strconv.FormatBool(m.session.Resolved.Settings.Detection.MCP)})
	case "mode":
		m.openForm("setting", []string{"installation.defaultMode"}, []string{m.session.Resolved.Settings.Installation.Mode})
	case "telemetry":
		m.openForm("setting", []string{"installation.upstreamTelemetry"}, []string{m.session.Resolved.Settings.Installation.Telemetry})
	case "verbose":
		m.openForm("setting", []string{"installation.verbose"}, []string{strconv.FormatBool(m.session.Resolved.Settings.Installation.Verbose)})
	case "bytes":
		m.openForm("setting", []string{"detection.maxFileBytes"}, []string{strconv.FormatInt(m.session.Resolved.Settings.Detection.MaxBytes, 10)})
	case "exclude":
		m.openForm("setting", []string{"detection.exclude"}, []string{strings.Join(m.session.Resolved.Settings.Detection.Exclude, ",")})
	case "trust":
		m.openForm("trust", []string{"Type the full fingerprint to approve"}, []string{""})
		m.details = m.session.Resolved.Fingerprint
	case "revoke":
		err := m.session.Store.Trust("", true)
		m.err = err
		if err == nil {
			m.generation++
			return m, m.load()
		}
	case "new":
		m.openNewRegistryForm()
	case "add":
		m.openForm("add", []string{"Registry ID", "Absolute local path or Git URL"}, []string{"", ""})
	default:
		if row.kind == "pending" {
			m.details = "Workspace: " + m.cwd + "\nFingerprint: " + m.session.Resolved.Fingerprint
			m.modal = "details"
		} else {
			for _, ref := range m.session.Resolved.Settings.Registries {
				if ref.ID != row.id {
					continue
				}
				location := ref.Source.URI
				if ref.Source.Kind == "local" {
					location = ref.Source.Path
				}
				m.openForm("registry", []string{"ID", "Enabled: true or false", "Priority", "Remove: type REMOVE", "Source URL or absolute path", "Requested Git ref", "Refresh hours (0 inherits)"}, []string{ref.ID, strconv.FormatBool(ref.Enabled), strconv.Itoa(ref.Priority), "", location, ref.Source.Ref, strconv.Itoa(ref.TTLHours)})
			}
		}
	}
	return m, nil
}
func (m noseyModel) View() tea.View {
	if m.height < 9 || m.width < 24 {
		v := tea.NewView(ansi.Truncate("Nosey: enlarge terminal (q quits)", max(1, m.width), ""))
		v.AltScreen = m.displayMode != "inline"
		return v
	}
	brand := m.styles.title.Render(":^) nosey")
	tabs := []string{"Scan", "Explore", "Manage", "Settings"}
	for i := range tabs {
		if i == m.tab {
			tabs[i] = m.styles.accent.Render("[" + tabs[i] + "]")
		}
	}
	menu := strings.Join(tabs, "   ")
	if ansi.StringWidth(brand)+2+ansi.StringWidth(menu) > m.width {
		menu = strings.Join(tabs, " ")
	}
	if ansi.StringWidth(brand)+2+ansi.StringWidth(menu) > m.width {
		menu = tabs[m.tab]
	}
	header := brand + "  " + menu + "\n" + m.styles.muted.Render(storage.Safe(m.cwd))
	lines := []string{}
	if m.loading {
		lines = append(lines, m.busy.View()+" Loading workspace…  ·  q Quit")
	}
	if m.err != nil && m.modal == "" {
		lines = append(lines, m.styles.warn.Render("Error: "+storage.Safe(m.err.Error())))
	}
	if m.tab == 1 {
		query := m.query
		if query.Value() != m.filters[2].Query {
			query.SetValue(m.filters[2].Query)
		}
		lines = append(lines, ansi.Truncate(query.View(), m.width-2, "…"), "")
	} else if m.search {
		lines = append(lines, m.query.View())
	}
	settingsFocus := 0
	settingsEditing := m.tab == 3 && m.modal != ""
	if settingsEditing {
		lines, settingsFocus = m.settingsPage()
	} else {
		switch m.modal {
		case "sort":
			title := "Sort results"
			lines = append(lines, m.styles.heading.Render(title), "")
			for i, choice := range m.selectChoices() {
				mark := "  ○ "
				label := choice.label
				if choice.id == m.selectValue() {
					label += "  (current)"
				}
				if i == m.sortChoice {
					mark = "› ● "
					label = m.styles.accent.Render(label)
				}
				lines = append(lines, ansi.Truncate(mark+label, m.width-2, "…"))
			}
		case "form":
			lines = append(lines, m.details)
			for i, label := range m.form.labels {
				mark := "  "
				if i == m.form.index {
					mark = "> "
				}
				lines = append(lines, mark+m.styles.heading.Render(label)+": "+m.form.inputs[i].View(), "")
			}
			lines = append(lines, "Enter Apply  ·  Tab Next field  ·  Esc Cancel")
		case "details":
			lines = append(lines, m.details)
		case "plan":
			lines = append(lines, fmt.Sprintf("Review all %d selected skills (includes selections hidden by filters)", len(m.selected)))
			for _, cmd := range m.plan.Commands {
				lines = append(lines, installation.CommandText(cmd.Argv))
			}
			if m.plan.Approval.Digest != nil {
				lines = append(lines, "Digest: "+*m.plan.Approval.Digest)
			}
			if m.plan.Approval.Required {
				lines = append(lines, fmt.Sprintf("Additional source approval: %t — press a to approve", m.extraApproved))
			}
			switch {
			case m.action == "print":
				lines = append(lines, "Print mode: review commands above · c Copy prepared commands · Esc Close")
			case m.copyReady:
				lines = append(lines, "Enter Execute  ·  c Copy commands  ·  Esc Cancel")
			default:
				lines = append(lines, "Enter Execute  ·  Esc Cancel  ·  Copy unavailable until local sources are prepared")
			}
		default:
			rows := m.rows()
			cursor := m.cursor[m.tab]
			if m.tab == 0 {
				lines = append(lines, technologyStrip(rows, cursor, m.width-2, m.styles)...)
				start := 0
				for start < len(rows) && (rows[start].kind == "technology" || rows[start].kind == "heading" && rows[start].id == "technologies") {
					start++
				}
				rows = rows[start:]
				cursor -= start
			}
			lines = append(lines, resultWindow(rows, cursor, 0, max(1, m.viewport.Height()-len(lines)), m.width-2, m.styles)...)
		}
	}
	if m.err != nil && m.modal != "" && !settingsEditing {
		lines = append(lines, m.styles.warn.Render("Error: "+storage.Safe(m.err.Error())))
	}
	if m.message != "" {
		lines = append(lines, m.styles.good.Render(m.message))
	}
	body := strings.Join(lines, "\n")
	vp := m.viewport
	vp.SetContent(body)
	switch {
	case settingsEditing:
		vp.SetYOffset(max(0, settingsFocus-max(3, vp.Height()/2)))
	case m.modal == "sort":
		vp.SetYOffset(max(0, m.sortChoice+3-vp.Height()))
	case m.modal == "form":
		vp.SetYOffset(max(0, m.form.index*2+2-vp.Height()+3))
	case m.modal == "details":
		vp.SetYOffset(m.modalOffset)
	default:
		vp.SetYOffset(0)
	}
	footer := "↑↓ Move · Space Select · f Filter · s Sort · i Install · Tab Section"
	if m.tab == 1 {
		footer = "/ Search · ↑↓ Browse · Enter Details · Tab Section · q Quit"
		if m.search {
			footer = "Type Search · Enter Browse · Esc Unfocus · Tab Section"
		}
	}
	if m.tab == 2 {
		footer = "↑↓ Move · r Refresh inventory · ? Help · Tab Section · q Quit"
	}
	if m.tab == 3 {
		footer = "↑↓ Move · Enter Edit · a Add existing · n New registry · Tab Section · q Quit"
	}
	if m.modal != "" {
		footer = "Tab Next field · ↑↓ Choose · Space Toggle · Enter Save · Esc Cancel"
		if m.modal == "sort" {
			footer = "↑↓ Choose · Enter Apply · Esc Cancel"
		}
		if m.modal == "plan" {
			footer = "c Copy prepared commands · Esc Close"
			if m.action != "print" {
				footer = "Enter Execute · c Copy commands · Esc Cancel"
				if m.plan.Approval.Required {
					footer = "a Approve source · Enter Execute · c Copy commands · Esc Cancel"
				}
			}
		}
	}
	visibleIDs := map[string]bool{}
	for _, row := range m.rows() {
		if row.kind == "skill" && m.selected[row.id] {
			visibleIDs[row.id] = true
		}
	}
	visible := len(visibleIDs)
	headerLines := strings.Split(header, "\n")
	for i := range headerLines {
		headerLines[i] = ansi.Truncate(headerLines[i], m.width, "…")
	}
	header = strings.Join(headerLines, "\n")
	if m.width < 80 && m.modal == "" {
		footer = "↑↓ Move · ? Help · Tab Section · q Quit"
	}
	footer = m.styles.footer(fmt.Sprintf("%d selected total · %d selected visible", len(m.selected), visible), footer, m.width)
	content := header + "\n\n" + vp.View() + "\n\n" + footer
	v := tea.NewView(content)
	v.AltScreen = m.displayMode != "inline"
	v.WindowTitle = "Nosey"
	return v
}
func csv(value string) []string {
	result := []string{}
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
func number(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
func parseNumber(value string) (*float64, error) {
	if value == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
		return nil, fmt.Errorf("threshold must be 0..100")
	}
	return &v, nil
}
