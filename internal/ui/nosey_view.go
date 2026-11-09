package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/storage"
)

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
	switch {
	case m.loading && m.modal == "install":
		// Progress renders inside the install page so it never covers its content.
	case m.loading:
		label := "Loading workspace…"
		if m.busyLabel != "" {
			label = m.busyLabel
		}
		lines = append(lines, m.busy.View()+" "+label+"  ·  q Quit")
	}
	if m.err != nil && (m.modal == "" || m.modal == "install") {
		lines = append(lines, ansi.Hardwrap(m.styles.warn.Render("Error: "+storage.Safe(m.err.Error())), max(10, m.width-2), true))
	}
	if m.warning != "" && m.modal == "" {
		lines = append(lines, ansi.Hardwrap(m.styles.warn.Render(storage.Safe(m.warning)), max(10, m.width-2), true))
	}
	if m.tab == 1 {
		query := m.query
		if query.Value() != m.filters[1].Query {
			query.SetValue(m.filters[1].Query)
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
		case "install":
			lines = append(lines, m.installPageLines()...)
		default:
			rows := m.rows()
			cursor := m.cursor[m.tab]
			if m.tab == 0 {
				names := m.technologyNames()
				title := fmt.Sprintf("Detected technologies  %d", len(names))
				lines = append(lines, technologyStrip(title, names, m.width-2, m.styles)...)
			}
			lines = append(lines, resultWindow(rows, cursor, 0, max(1, m.viewport.Height()-len(lines)), m.width-2, m.styles)...)
		}
	}
	if m.err != nil && m.modal != "" && m.modal != "install" && !settingsEditing {
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
	case m.modal == "details" || m.modal == "install":
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
		footer = "↑↓ Move · r Refresh inventory · u Check updates · ? Help · Tab Section · q Quit"
	}
	if m.tab == 3 {
		footer = "↑↓ Move · Enter Edit · a Add existing · n New registry · Tab Section · q Quit"
	}
	if m.modal != "" {
		footer = "Tab Next field · ↑↓ Choose · Space Toggle · Enter Save · Esc Cancel"
		if m.modal == "sort" {
			footer = "↑↓ Choose · Enter Apply · Esc Cancel"
		}
		if m.modal == "install" {
			footer = "Enter Start · c Copy commands · ↑↓ Scroll · Esc Cancel"
			if m.install.running {
				footer = "Esc / Ctrl+C Cancel install · ↑↓ Scroll"
			}
			if m.install.printOnly {
				footer = "c Copy commands · ↑↓ Scroll · Esc Close"
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
