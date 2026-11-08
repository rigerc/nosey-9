package ui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/storage"
)

type noseyStyles struct{ title, accent, heading, repo, muted, focus, good, warn, badge lipgloss.Style }

func newNoseyStyles() noseyStyles {
	style := func(color string, bold bool) lipgloss.Style {
		s := lipgloss.NewStyle().Bold(bold)
		if os.Getenv("NO_COLOR") == "" {
			s = s.Foreground(lipgloss.Color(color))
		}
		return s
	}
	return noseyStyles{title: style("#A78BFA", true), accent: style("#22D3EE", true), heading: style("#C4B5FD", true), repo: style("#67E8F9", true), muted: style("#94A3B8", false), focus: style("#FBBF24", true), good: style("#4ADE80", true), warn: style("#FBBF24", true), badge: style("#A5B4FC", true)}
}
func (s noseyStyles) row(row noseyRow, focused bool, width int) string {
	label := storage.Safe(row.label)
	if row.kind == "skill" {
		return s.skillRow(label, focused, width)
	}
	switch {
	case row.kind == "group" && strings.HasPrefix(row.id, "repo:"):
		label = s.repo.Render(label)
	case row.kind == "group" || row.kind == "heading" || row.kind == "technology" || row.kind == "registry-overview":
		label = s.heading.Render(label)
	case row.kind == "notice":
		label = s.muted.Render(label)
	case row.kind == "setting" || row.kind == "registry":
		key, value, ok := strings.Cut(label, ": ")
		if ok {
			label = s.heading.Render(key) + s.muted.Render(": ") + value
		} else {
			label = s.heading.Render(label)
		}
	}
	// Badge syntax remains readable on terminals without color.
	for _, prefix := range []string{"[score ", "[quality ", "[repo "} {
		start := strings.Index(label, prefix)
		if start < 0 {
			continue
		}
		end := strings.Index(label[start:], "]")
		if end < 0 {
			continue
		}
		end += start + 1
		badge := label[start:end]
		paint := s.badge
		if strings.Contains(badge, "excellent") || strings.Contains(badge, "good") {
			paint = s.good
		}
		if strings.Contains(badge, "poor") || strings.Contains(badge, "fair") {
			paint = s.warn
		}
		label = label[:start] + paint.Render(badge) + label[end:]
	}
	mark := "  "
	if focused {
		mark = s.focus.Render("› ")
	}
	return mark + label
}

// Separate visual spacing from selectable rows so arrow keys never land on empty lines.
func resultSlots(rows []noseyRow) ([]int, []int) {
	slots, positions := []int{}, make([]int, len(rows))
	for i, row := range rows {
		if i > 0 && (row.kind == "heading" || row.kind == "group" || row.kind == "setting" || row.kind == "registry" || row.kind == "pending" || row.kind == "registry-overview") {
			slots = append(slots, -1)
		}
		if i > 0 && row.kind == "group" && strings.HasPrefix(row.id, "tech:") {
			slots = append(slots, -1)
		}
		positions[i] = len(slots)
		slots = append(slots, i)
	}
	return slots, positions
}
func resultWindow(rows []noseyRow, cursor, offset, height, width int, styles noseyStyles) []string {
	slots, positions := resultSlots(rows)
	if len(slots) == 0 {
		return nil
	}
	cursor = min(cursor, len(rows)-1)
	if cursor >= 0 && positions[cursor] < offset {
		offset = positions[cursor]
	}
	if cursor >= 0 && positions[cursor] >= offset+height {
		offset = positions[cursor] - height + 1
	}
	offset = min(max(0, offset), max(0, len(slots)-height))
	lines := []string{}
	for _, i := range slots[offset:min(len(slots), offset+height)] {
		line := ""
		if i >= 0 {
			line = styles.row(rows[i], i == cursor, width)
		}
		lines = append(lines, ansi.Truncate(line, max(1, width), "…"))
	}
	return lines
}

func (s noseyStyles) skillRow(label string, focused bool, width int) string {
	head, tail, ok := strings.Cut(label, "[score ")
	if !ok {
		return label
	}
	score, rest, _ := strings.Cut(tail, "]")
	_, quality, _ := strings.Cut(rest, "[quality ")
	quality, _, _ = strings.Cut(quality, "]")
	badge := s.badge.Render("[" + score + "]")
	paint := s.badge
	if quality == "good" || quality == "excellent" {
		paint = s.good
	}
	if quality == "fair" || quality == "poor" {
		paint = s.warn
	}
	if quality != "" {
		badge += " " + paint.Render("["+quality+"]")
	}
	mark := "  "
	if focused {
		mark = s.focus.Render("› ")
	}
	available := max(8, width-3-ansi.StringWidth(badge))
	head = ansi.Truncate(strings.TrimRight(head, " "), available, "…")
	return mark + lipgloss.NewStyle().Bold(true).Render(head) + " " + badge
}

func technologyStrip(rows []noseyRow, cursor, width int, styles noseyStyles) []string {
	title := "Detected technologies"
	chips := []string{}
	focusedStart, focusedEnd, position := 0, 0, 0
	for i, row := range rows {
		if row.kind == "heading" && row.id == "technologies" {
			title = row.label
		}
		if row.kind != "technology" {
			continue
		}
		label := storage.Safe(row.label)
		if i == cursor {
			label = "› " + label
			focusedStart = position
			focusedEnd = position + ansi.StringWidth(label)
			label = styles.focus.Render(label)
		} else {
			label = styles.accent.Render(label)
		}
		chips = append(chips, label)
		position += ansi.StringWidth(label) + 3
	}
	line := strings.Join(chips, " · ")
	if len(chips) == 0 {
		line = styles.muted.Render("No detected technologies")
	}
	available := max(1, width-4)
	offset := max(0, focusedEnd-available)
	if focusedStart < offset {
		offset = focusedStart
	}
	line = ansi.Cut(line, offset, offset+available)
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#94A3B8")).Padding(0, 1).Width(max(1, width-2))
	if os.Getenv("NO_COLOR") != "" {
		border = border.UnsetBorderForeground()
	}
	return append([]string{styles.heading.Render(title)}, strings.Split(border.Render(line), "\n")...)
}

func (s noseyStyles) footer(summary, bindings string, width int) string {
	contentWidth := max(1, width-4)
	text := ansi.Truncate(summary, contentWidth, "…") + "\n" + ansi.Truncate(bindings, contentWidth, "…")
	return s.muted.Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(max(1, width-2)).Render(text)
}
