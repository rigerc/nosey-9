package ui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
)

// installRun holds the plans shown on the install page and, while running, the operation that
// holds the workspace lock.
type installRun struct {
	// kind is "install" (the zero value), "update" or "uninstall".
	kind      string
	plans     []installation.SourcePlan
	update    installation.UpdatePlan
	remove    installation.UninstallPlan
	removed   []installation.RemoveResult
	op        *installation.Operation
	results   []installation.SourceResult
	agents    []string
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	cancelled bool
	index     int
}

// total is the number of steps: sources for install and update, skills for uninstall.
func (r installRun) total() int {
	if r.kind == "uninstall" {
		return len(r.remove.Skills)
	}
	return len(r.plans)
}

// noun and verb describe the operation in progress and its outcome.
func (r installRun) noun() string {
	switch r.kind {
	case "update":
		return "Update"
	case "uninstall":
		return "Removal"
	}
	return "Installation"
}

// action names the operation for the cancel hint.
func (r installRun) action() string {
	switch r.kind {
	case "update":
		return "update"
	case "uninstall":
		return "removal"
	}
	return "install"
}

// release frees the operation lock; it is idempotent.
func (r *installRun) release() {
	if r.op != nil {
		_ = r.op.Close()
		r.op = nil
	}
}

type installReady struct {
	op         *installation.Operation
	err        error
	generation int
}
type installStepDone struct {
	generation int
	index      int
	result     installation.SourceResult
	removal    installation.RemoveResult
	err        error
}

func (m noseyModel) installer() installation.Service {
	installer := m.session.Installer
	installer.Settings = m.session.Resolved.Settings
	return installer
}

// openInstall builds the commands for every selected skill, including those hidden by filters.
func (m noseyModel) openInstall() (noseyModel, tea.Cmd) {
	return m.openInstallForAgents(m.session.Resolved.Settings.Installation.Agents)
}

func (m noseyModel) openInstallForAgents(agents []string) (noseyModel, tea.Cmd) {
	cfg := m.session.Resolved.Settings
	if len(agents) == 0 {
		m.openForm("install-agents", []string{"installation.agents"}, []string{""})
		m.details = "Choose target agents for this install. Settings will not change."
		return m, nil
	}
	ids := []string{}
	for id := range m.selected {
		ids = append(ids, id)
	}
	opts := installation.Options{IDs: ids, Agents: slices.Clone(agents), Method: cfg.Installation.Method, Yes: len(agents) > 0}
	plans, err := m.installer().Build(m.session.Catalog, m.recs, opts)
	m.warning = ""
	m.err = err
	if err != nil {
		return m, nil
	}
	m.install = installRun{plans: plans, agents: slices.Clone(agents)}
	m.modal = "install"
	return m, nil
}

// openUpdate plans an update of the outdated catalog skill under the cursor. It reuses the
// finished update check, so planning needs no network access; the confirmation page then shows
// the reasons and locations before anything runs.
func (m noseyModel) openUpdate(name string) (noseyModel, tea.Cmd) {
	var item *installation.InventoryItem
	for i := range m.inventory.Skills {
		if m.inventory.Skills[i].Name == name {
			item = &m.inventory.Skills[i]
		}
	}
	m.err, m.message, m.warning = nil, "", ""
	if item == nil || item.SkillID == nil || item.Status != "outdated" {
		return m, nil
	}
	check := installation.UpdateCheck{Names: m.updates}
	plan, err := m.installer().PlanUpdate(m.ctx, m.session.Catalog, m.recs, installation.UpdateOptions{IDs: []string{*item.SkillID}, Check: &check})
	if err != nil {
		m.err = err
		return m, nil
	}
	if plan.Empty() {
		m.message = storage.Safe(name) + " is already up to date."
		return m, nil
	}
	m.install = installRun{kind: "update", plans: plan.Plans, update: plan}
	m.modal = "install"
	return m, nil
}

// openUninstall plans the removal of a tracked skill; untracked skills and requirement
// conflicts are reported as errors without opening the confirmation page.
func (m noseyModel) openUninstall(name string) (noseyModel, tea.Cmd) {
	m.err, m.message, m.warning = nil, "", ""
	if !m.inventory.Tracked(name) {
		return m, nil
	}
	plan, err := m.installer().PlanUninstall(m.ctx, m.session.Catalog, m.recs, installation.UninstallOptions{Targets: []string{name}})
	if err != nil {
		m.err = err
		return m, nil
	}
	m.install = installRun{kind: "uninstall", remove: plan}
	m.modal = "install"
	return m, nil
}

func (m noseyModel) updateInstallPage(key string) (noseyModel, tea.Cmd) {
	if key == "enter" {
		m.err = nil
		m.loading = true
		m.install.ctx, m.install.cancel = context.WithCancel(m.ctx)
		m.install.running, m.install.cancelled, m.install.index = true, false, -1
		installer, ctx, generation, kind, remove := m.installer(), m.install.ctx, m.generation, m.install.kind, m.install.remove
		return m, func() tea.Msg {
			op, err := installer.Begin(ctx)
			if err == nil && kind == "uninstall" {
				if err = op.RecheckUninstall(ctx, remove); err != nil {
					_ = op.Close()
					op = nil
				}
			}
			return installReady{op: op, err: err, generation: generation}
		}
	}
	m.scrollModal(key)
	return m, nil
}

func (m noseyModel) applyInstallReady(ready installReady) (noseyModel, tea.Cmd) {
	if ready.generation != m.generation {
		_ = ready.op.Close()
		return m, nil
	}
	if m.install.cancelled {
		_ = ready.op.Close()
		m.install.cancel()
		m.install.running = false
		m.loading = false
		m.modal = ""
		m.message = m.install.noun() + " cancelled. No " + map[bool]string{true: "skills", false: "sources"}[m.install.kind == "uninstall"] + " started."
		return m, nil
	}
	if ready.err != nil {
		if m.install.cancel != nil {
			m.install.cancel()
		}
		m.install.running = false
		m.loading = false
		m.err = ready.err
		return m, nil
	}
	m.install.op = ready.op
	m.install.index = 0
	return m, m.runInstallStep(0)
}

func (m noseyModel) runInstallStep(index int) tea.Cmd {
	op, ctx, generation, kind := m.install.op, m.install.ctx, m.generation, m.install.kind
	if kind == "uninstall" {
		skill := m.install.remove.Skills[index]
		return func() tea.Msg {
			removal, err := op.RunRemove(ctx, skill)
			return installStepDone{generation: generation, index: index, removal: removal, err: err}
		}
	}
	plan := m.install.plans[index]
	return func() tea.Msg {
		var result installation.SourceResult
		var err error
		if kind == "update" {
			result, err = op.RunUpdate(ctx, plan)
		} else {
			result, err = op.RunSource(ctx, plan)
		}
		return installStepDone{generation: generation, index: index, result: result, err: err}
	}
}

func (m noseyModel) cancelInstall() (noseyModel, tea.Cmd) {
	m.install.cancelled = true
	if m.install.cancel != nil {
		m.install.cancel()
	}
	return m, nil
}

// applyInstallStep continues only after the source finished and its lock entries were written.
func (m noseyModel) applyInstallStep(done installStepDone) (noseyModel, tea.Cmd) {
	if done.generation != m.generation {
		m.install.release()
		return m, nil
	}
	total := m.install.total()
	if m.install.kind == "uninstall" {
		m.install.removed = append(m.install.removed, done.removal)
	} else {
		m.install.results = append(m.install.results, done.result)
	}
	if m.install.cancelled && done.err == nil {
		done.err = context.Canceled
	}
	if done.err == nil && done.index+1 < total {
		m.install.index = done.index + 1
		return m, m.runInstallStep(done.index + 1)
	}
	m.install.release()
	if m.install.cancel != nil {
		m.install.cancel()
	}
	m.install.running = false
	m.updates = nil
	m.message, m.warning = m.installOutcome(done, total)
	m.modal = ""
	m.generation++
	m.loading = true
	return m, m.load()
}

// installOutcome words the result of a finished or stopped run.
func (m noseyModel) installOutcome(done installStepDone, total int) (message, warning string) {
	later := total - done.index - 1
	if m.install.kind == "uninstall" {
		removed := 0
		notes := m.install.remove.Overridden
		for _, r := range m.install.removed {
			if r.Status == installation.RemoveDone {
				removed++
			}
		}
		if done.err != nil {
			warning = fmt.Sprintf("Stopped at %s: %v; %s not started. %s removed.", storage.Safe(m.install.remove.Skills[done.index].Name), done.err, countLabel(later, "later skill"), countLabel(removed, "skill"))
		} else {
			message = fmt.Sprintf("Removed %s. Status updated below.", countLabel(removed, "skill"))
		}
		if len(notes) > 0 {
			warning = strings.TrimSpace(warning + " " + strings.Join(notes, "; "))
		}
		return message, warning
	}
	notes := []string{}
	installed := 0
	for _, result := range m.install.results {
		notes = append(notes, result.Notes()...)
		installed += len(result.Installed())
	}
	verb, past := "installed", "Installed"
	if m.install.kind == "update" {
		verb, past = "updated", "Updated"
		notes = append(notes, m.install.update.SkippedLines()...)
	}
	if done.err != nil {
		warning = fmt.Sprintf("Stopped at %s: %v; %s not started. %s recorded in skills-lock.json.", m.install.plans[done.index].Label(), done.err, countLabel(later, "later source"), countLabel(installed, verb+" skill"))
	} else {
		message = fmt.Sprintf("%s %s from %s. Status updated below.", past, countLabel(installed, "skill"), countLabel(total, "source"))
	}
	if len(notes) > 0 {
		warning = strings.TrimSpace(warning + " " + strings.Join(notes, "; "))
	}
	return message, warning
}

// installStatus describes what the installer is doing; it renders inside the page, never over it.
func (m noseyModel) installStatus() string {
	switch {
	case m.install.cancelled:
		return m.busy.View() + " " + m.styles.warn.Render("Cancelling "+strings.ToLower(m.install.noun())+"…")
	case m.install.running && m.install.index >= 0:
		unit, verb := "source", "Installing"
		switch m.install.kind {
		case "update":
			verb = "Updating"
		case "uninstall":
			unit, verb = "skill", "Removing"
		}
		return m.busy.View() + " " + m.styles.accent.Render(fmt.Sprintf("%s %s %d of %d", verb, unit, m.install.index+1, m.install.total()))
	case m.install.running:
		label := "Preparing installer…"
		if m.install.kind != "" && m.install.kind != "install" {
			label = "Preparing " + m.install.action() + "…"
		}
		return m.busy.View() + " " + m.styles.accent.Render(label)
	}
	return ""
}

// sourceMarker shows a source's progress: done, running, or waiting.
func (m noseyModel) sourceMarker(i int) string {
	switch {
	case !m.install.running:
		return m.styles.muted.Render("•")
	case i < m.install.index:
		return m.styles.good.Render("✓")
	case i == m.install.index:
		return m.busy.View()
	}
	return m.styles.muted.Render("○")
}

func (m noseyModel) lifecyclePageLines() []string {
	width := max(10, m.width-2)
	var lines []string
	if m.install.kind == "update" {
		lines = append(lines, m.styles.title.Render(fmt.Sprintf("Update %s from %s", countLabel(m.install.update.Count(), "skill"), countLabel(len(m.install.plans), "source"))))
	} else {
		lines = append(lines, m.styles.title.Render("Remove "+countLabel(len(m.install.remove.Skills), "skill")))
	}
	if status := m.installStatus(); status != "" {
		lines = append(lines, status)
	}
	intro := "Confirm once to update the selection in this project. Local edits to these skills will be replaced. No further prompts."
	if m.install.kind == "uninstall" {
		intro = "Confirm once to remove the selection from every agent directory of this project and from skills-lock.json. No further prompts."
	}
	lines = append(lines, m.styles.muted.Render(intro), "")
	if m.install.kind == "update" {
		for i, plan := range m.install.plans {
			lines = append(lines, m.sourceMarker(i)+" "+m.styles.repo.Render(plan.Label()))
			for _, skill := range plan.Skills {
				label := storage.Safe(skill.Name)
				if skill.Required {
					label += " " + m.styles.badge.Render("(required)")
				}
				lines = append(lines, "    "+m.styles.accent.Render("▪")+" "+label, "      "+m.styles.muted.Render(storage.Safe(strings.Join(skill.Reasons, "; "))))
				for _, target := range skill.Targets {
					lines = append(lines, "      "+m.styles.muted.Render("at "+storage.Safe(target)))
				}
			}
		}
		for _, line := range m.install.update.SkippedLines() {
			lines = append(lines, m.styles.muted.Render(storage.Safe(line)))
		}
	} else {
		for i, skill := range m.install.remove.Skills {
			marker := m.styles.muted.Render("•")
			if m.install.running {
				switch {
				case i < m.install.index:
					marker = m.styles.good.Render("✓")
				case i == m.install.index:
					marker = m.busy.View()
				default:
					marker = m.styles.muted.Render("○")
				}
			}
			line := marker + " " + storage.Safe(skill.Name)
			if skill.ID != "" {
				line += " " + m.styles.muted.Render("("+storage.Safe(skill.ID)+")")
			}
			lines = append(lines, line)
		}
		for _, o := range m.install.remove.Overridden {
			lines = append(lines, m.styles.warn.Render("warning: "+storage.Safe(o)))
		}
	}
	lines = append(lines, "", m.installBox(m.styles.heading.Render("Scope:")+" project", width))
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")
}

func (m noseyModel) installPageLines() []string {
	if m.install.kind == "update" || m.install.kind == "uninstall" {
		return m.lifecyclePageLines()
	}
	skills := 0
	for _, command := range m.install.plans {
		skills += len(command.Skills)
	}
	intro := "Confirm once to install the entire selection into this project. Existing skills with the same names will be replaced. No further prompts."
	width := max(10, m.width-2)
	lines := []string{m.styles.title.Render(fmt.Sprintf("Install %s from %s", countLabel(skills, "skill"), countLabel(len(m.install.plans), "source")))}
	if status := m.installStatus(); status != "" {
		lines = append(lines, status)
	}
	lines = append(lines, m.styles.muted.Render(intro), "")
	for i, command := range m.install.plans {
		lines = append(lines, m.sourceMarker(i)+" "+m.styles.repo.Render(command.Label()))
		for _, skill := range command.Skills {
			line := "    " + m.styles.accent.Render("▪") + " " + storage.Safe(skill.Name)
			if skill.Required {
				line += " " + m.styles.badge.Render("(required)")
			}
			lines = append(lines, line)
		}
	}
	agents := "choose target agents before running"
	if configured := m.install.agents; len(configured) > 0 {
		agents = strings.Join(configured, ", ")
	}
	detail := func(key, value string) string { return m.styles.heading.Render(key+":") + " " + value }
	summary := strings.Join([]string{detail("Scope", "project"), detail("Agents", storage.Safe(agents)), detail("Method", m.session.Resolved.Settings.Installation.Method)}, "\n")
	lines = append(lines, "", m.installBox(summary, width))
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")
}

func (m noseyModel) installBox(text string, width int) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(max(1, min(width, 72)))
	if os.Getenv("NO_COLOR") == "" {
		box = box.BorderForeground(lipgloss.Color("#94A3B8"))
	}
	return box.Render(text)
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
