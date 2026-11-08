package ui

import (
	tea "charm.land/bubbletea/v2"
)

type sortOption struct{ id, label string }

func sortChoices(target int) []sortOption {
	if target == 0 {
		return []sortOption{{"name-asc", "Name · A to Z"}, {"name-desc", "Name · Z to A"}, {"evidence-count-desc", "Most evidence"}, {"matched-rules-desc", "Most matched rules"}}
	}
	options := []sortOption{{"score-desc", "Highest skill score"}, {"quality-desc", "Highest quality"}, {"repository-average-desc", "Highest repository score"}, {"name-asc", "Name · A to Z"}}
	if target == 2 {
		return append([]sortOption{{"relevance", "Most relevant"}}, options...)
	}
	return append([]sortOption{{"priority-desc", "Highest recommendation priority"}}, options...)
}
func (m *noseyModel) openSort() {
	m.modal = "sort"
	m.sortTarget = m.activeFilter()
	m.sortChoice = 0
	for i, option := range sortChoices(m.sortTarget) {
		if option.id == m.sort[m.sortTarget] {
			m.sortChoice = i
			break
		}
	}
}
func (m noseyModel) updateSort(key tea.KeyPressMsg) (noseyModel, tea.Cmd) {
	options := m.selectChoices()
	switch key.String() {
	case "up", "k", "shift+tab":
		m.sortChoice = (m.sortChoice + len(options) - 1) % len(options)
	case "down", "j", "tab":
		m.sortChoice = (m.sortChoice + 1) % len(options)
	case "home":
		m.sortChoice = 0
	case "end":
		m.sortChoice = len(options) - 1
	case "enter":
		value := options[m.sortChoice].id
		m.sort[m.sortTarget] = value
		m.modal = ""
	}
	return m, nil
}

func (m noseyModel) selectChoices() []sortOption { return sortChoices(m.sortTarget) }
func (m noseyModel) selectValue() string         { return m.sort[m.sortTarget] }
