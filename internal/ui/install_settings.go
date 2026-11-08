package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/installation"
)

func (m noseyModel) installFromSettings() (noseyModel, tea.Cmd) {
	cfg := m.session.Resolved.Settings
	if len(cfg.Installation.Agents) == 0 {
		m.err = fmt.Errorf("choose target agents in Settings before installing")
		return m, nil
	}
	ids := []string{}
	for id := range m.selected {
		ids = append(ids, id)
	}
	opts := installation.Options{IDs: ids, Agents: slices.Clone(cfg.Installation.Agents), Method: cfg.Installation.Method}
	installer := m.session.Installer
	installer.Settings = cfg
	effectiveCatalog, recommendations, ctx := m.session.Catalog, m.recs, m.ctx
	m.action = cfg.Installation.Mode
	m.loading = true
	m.err = nil
	return m, func() tea.Msg {
		plan, err := installer.Plan(ctx, effectiveCatalog, recommendations, opts)
		return noseyPlan{plan: plan, err: err}
	}
}
