package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (m *noseyModel) configureFormChoices() {
	m.form.choices = map[int][]sortOption{}
	m.form.choiceIndex = map[int]int{}
	m.form.multiple = map[int]bool{}
	boolean := []sortOption{{"false", "Disabled"}, {"true", "Enabled"}}
	switch m.form.kind {
	case "setting":
		switch m.form.labels[0] {
		case "ui.mode":
			m.form.choices[0] = []sortOption{{"fullscreen", "Fullscreen"}, {"inline", "Inline"}}
		case "installation.method":
			m.form.choices[0] = []sortOption{{"symlink", "Symlink"}, {"copy", "Copy"}}
		case "installation.defaultMode":
			m.form.choices[0] = []sortOption{{"execute", "Execute after confirmation"}, {"print", "Print commands"}}
		case "installation.upstreamTelemetry":
			m.form.choices[0] = []sortOption{{"inherit", "Inherit upstream preference"}, {"disabled", "Disabled"}}
		case "detection.enableMcpPackage", "installation.verbose":
			m.form.choices[0] = boolean
		case "installation.agents":
			m.form.multiple[0] = true
			for id, name := range installation.AgentNames() {
				m.form.choices[0] = append(m.form.choices[0], sortOption{id, name})
			}
			for _, id := range csv(m.form.inputs[0].Value()) {
				if !slices.ContainsFunc(m.form.choices[0], func(option sortOption) bool { return option.id == id }) {
					m.form.choices[0] = append(m.form.choices[0], sortOption{id, "Unknown agent: " + id})
				}
			}
			slices.SortFunc(m.form.choices[0], func(a, b sortOption) int { return strings.Compare(a.label, b.label) })
		}
	case "registry":
		m.form.choices[0] = []sortOption{{m.form.inputs[0].Value(), m.form.inputs[0].Value()}}
		m.form.choices[1] = boolean
		m.form.choices[3] = []sortOption{{"", "Keep registry"}, {"REMOVE", "Remove registry from user settings"}}
	case "new":
		m.form.choices[1] = []sortOption{{"empty", "Empty registry"}, {"workspace", "Seed from workspace technologies"}}
		for id := range m.session.Catalog.Roots {
			m.form.choices[2] = append(m.form.choices[2], sortOption{id, id})
		}
		slices.SortFunc(m.form.choices[2], func(a, b sortOption) int { return strings.Compare(a.id, b.id) })
		m.form.multiple[3] = true
		for id, tech := range m.session.Catalog.Technologies {
			m.form.choices[3] = append(m.form.choices[3], sortOption{id, tech.Name})
		}
		slices.SortFunc(m.form.choices[3], func(a, b sortOption) int { return strings.Compare(a.label+a.id, b.label+b.id) })
		m.form.choices[4], m.form.choices[5] = boolean, boolean
	}
	for field, options := range m.form.choices {
		if m.form.multiple[field] {
			m.form.inputs[field].CharLimit = 0
		}
		selected := csv(m.form.inputs[field].Value())
		for i, option := range options {
			if option.id == m.form.inputs[field].Value() || m.form.multiple[field] && slices.Contains(selected, option.id) {
				m.form.choiceIndex[field] = i
				break
			}
		}
	}
	if m.tab == 3 {
		switch m.form.kind {
		case "new", "add", "trust":
			m.settingsAnchor = m.form.kind
		}
	}
}
func (m noseyModel) updateChoiceField(key tea.KeyPressMsg) (noseyModel, tea.Cmd) {
	field := m.form.index
	options := m.form.choices[field]
	index := m.form.choiceIndex[field]
	moved := false
	switch key.String() {
	case "up", "k", "left":
		index = (index + len(options) - 1) % len(options)
		moved = true
	case "down", "j", "right":
		index = (index + 1) % len(options)
		moved = true
	case "pgup":
		index = max(0, index-5)
		moved = true
	case "pgdown":
		index = min(len(options)-1, index+5)
		moved = true
	case "home":
		index = 0
		moved = true
	case "end":
		index = len(options) - 1
		moved = true
	case "space":
		if m.form.multiple[field] {
			selected := csv(m.form.inputs[field].Value())
			if at := slices.Index(selected, options[index].id); at >= 0 {
				selected = slices.Delete(selected, at, at+1)
			} else {
				selected = append(selected, options[index].id)
			}
			m.form.inputs[field].SetValue(strings.Join(selected, ","))
		} else {
			m.form.inputs[field].SetValue(options[index].id)
		}
	}
	m.form.choiceIndex[field] = index
	if moved && !m.form.multiple[field] {
		m.form.inputs[field].SetValue(options[index].id)
	}
	return m, nil
}
func settingFieldLabel(label string) string {
	names := map[string]string{
		"ui.mode": "Display mode (next launch)", "installation.agents": "Target agents", "installation.method": "Install method", "installation.defaultMode": "Default install action", "installation.upstreamTelemetry": "Upstream telemetry", "installation.verbose": "Verbose installation", "detection.enableMcpPackage": "MCP inspection", "updates.ttlHours": "Refresh interval (hours)", "detection.maxFileBytes": "Maximum inspected file (bytes)", "detection.exclude": "Excluded paths (comma separated)",
		"Enabled: true or false": "Enabled", "Remove: type REMOVE": "Action", "Template: empty or workspace": "Template", "Template registry ID": "Template registry", "Include technology IDs (comma separated)": "Technologies to include", "Initialize Git: true or false": "Initialize Git", "Register for this user: true or false": "Register for this user",
	}
	if name, ok := names[label]; ok {
		return name
	}
	return label
}
func (m noseyModel) settingsFormLines() ([]string, int) {
	lines := []string{}
	focus := 0
	if m.details != "" {
		lines = append(lines, m.styles.muted.Render(storage.Safe(m.details)))
	}
	for field, input := range m.form.inputs {
		focused := field == m.form.index
		label := settingFieldLabel(m.form.labels[field])
		mark := "  "
		if focused {
			mark = "› "
			focus = len(lines)
		}
		options := m.form.choices[field]
		if len(options) == 0 {
			value := storage.Safe(input.Value())
			if focused {
				value = input.View()
			}
			lines = append(lines, mark+m.styles.heading.Render(label)+": "+value, "")
			continue
		}
		selected := csv(input.Value())
		summary := []string{}
		for _, option := range options {
			if option.id == input.Value() || m.form.multiple[field] && slices.Contains(selected, option.id) {
				summary = append(summary, option.label)
			}
		}
		value := strings.Join(summary, ", ")
		if value == "" {
			value = "None selected"
		}
		lines = append(lines, mark+m.styles.heading.Render(label)+": "+value)
		if focused {
			index := m.form.choiceIndex[field]
			start := max(0, index-2)
			end := min(len(options), start+5)
			start = max(0, end-5)
			for i := start; i < end; i++ {
				choice := options[i]
				checked := choice.id == input.Value()
				if m.form.multiple[field] {
					checked = slices.Contains(selected, choice.id)
				}
				box := "○"
				if checked {
					box = "●"
				}
				if m.form.multiple[field] {
					box = "[ ]"
					if checked {
						box = "[x]"
					}
				}
				text := "    " + box + " " + storage.Safe(choice.label)
				if i == index {
					text = "  › " + box + " " + m.styles.accent.Render(storage.Safe(choice.label))
					focus = len(lines)
				}
				lines = append(lines, text)
			}
			if len(options) > 5 {
				lines = append(lines, m.styles.muted.Render(fmt.Sprintf("    %d–%d of %d options", start+1, end, len(options))))
			}
		}
		lines = append(lines, "")
	}
	if m.err != nil {
		lines = append(lines, m.styles.warn.Render(storage.Safe(m.err.Error())))
	}
	lines = append(lines, m.styles.muted.Render("↑↓ Choose · Space Toggle · Tab Next field · Enter Save · Esc Cancel"))
	return lines, focus
}
func (m noseyModel) settingsPage() ([]string, int) {
	lines := []string{}
	focus := 0
	for i, row := range m.rows() {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.styles.row(row, row.id == m.settingsAnchor, m.width-2))
		if row.id != m.settingsAnchor {
			continue
		}
		var editor []string
		active := 0
		if m.modal == "form" {
			editor, active = m.settingsFormLines()
		} else {
			editor = strings.Split(m.details, "\n")
			active = m.modalOffset
		}
		lines = append(lines, "")
		focus = len(lines) + active
		for _, line := range editor {
			lines = append(lines, ansi.Truncate("    "+line, max(1, m.width-2), "…"))
		}
	}
	return lines, focus
}

func (m *noseyModel) openDisplaySelect() {
	m.settingsAnchor = "display"
	m.openForm("setting", []string{"ui.mode"}, []string{m.session.Resolved.Settings.UI.Mode})
}
