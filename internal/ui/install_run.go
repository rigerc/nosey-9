package ui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
)

// installRun holds the commands shown on the install page and the CLI that runs them.
type installRun struct {
	commands  []installation.AddCommand
	cli       installation.CLI
	printOnly bool
	agents    []string
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	cancelled bool
	index     int
}

type installReady struct {
	cli        installation.CLI
	err        error
	generation int
}
type installStepDone struct {
	generation int
	index      int
	err        error
}
type installCopied struct{ err error }

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
	if len(agents) == 0 && cfg.Installation.Mode != "print" {
		m.openForm("install-agents", []string{"installation.agents"}, []string{""})
		m.details = "Choose target agents for this install. Settings will not change."
		return m, nil
	}
	ids := []string{}
	for id := range m.selected {
		ids = append(ids, id)
	}
	opts := installation.Options{IDs: ids, Agents: slices.Clone(agents), Method: cfg.Installation.Method, Yes: len(agents) > 0}
	commands, err := m.installer().Build(m.session.Catalog, m.recs, opts)
	m.warning = ""
	m.err = err
	if err != nil {
		return m, nil
	}
	m.install = installRun{commands: commands, printOnly: cfg.Installation.Mode == "print", agents: slices.Clone(agents)}
	m.modal = "install"
	return m, nil
}

func (m noseyModel) updateInstallPage(key string) (noseyModel, tea.Cmd) {
	switch key {
	case "c":
		lines := []string{}
		for _, command := range m.install.commands {
			lines = append(lines, command.Text())
		}
		return m, func() tea.Msg { return installCopied{err: clipboard.WriteAll(strings.Join(lines, "\n"))} }
	case "enter":
		if m.install.printOnly {
			return m, nil
		}
		m.err = nil
		m.loading = true
		m.install.ctx, m.install.cancel = context.WithCancel(m.ctx)
		m.install.running, m.install.cancelled, m.install.index = true, false, -1
		installer, ctx, generation := m.installer(), m.install.ctx, m.generation
		return m, func() tea.Msg {
			cli, err := installer.Prepare(ctx)
			return installReady{cli: cli, err: err, generation: generation}
		}
	}
	m.scrollModal(key)
	return m, nil
}

func (m noseyModel) applyInstallReady(ready installReady) (noseyModel, tea.Cmd) {
	if ready.generation != m.generation {
		return m, nil
	}
	if m.install.cancelled {
		m.install.cancel()
		m.install.running = false
		m.loading = false
		m.modal = ""
		m.message = "Installation cancelled. No sources started."
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
	m.install.cli = ready.cli
	m.install.index = 0
	return m, m.runInstallStep(0)
}

func (m noseyModel) runInstallStep(index int) tea.Cmd {
	installer, ctx, cli, command, generation := m.installer(), m.install.ctx, m.install.cli, m.install.commands[index], m.generation
	return func() tea.Msg {
		return installStepDone{generation: generation, index: index, err: installer.RunSource(ctx, cli, command)}
	}
}

func (m noseyModel) cancelInstall() (noseyModel, tea.Cmd) {
	m.install.cancelled = true
	if m.install.cancel != nil {
		m.install.cancel()
	}
	return m, nil
}

// applyInstallStep continues only after the source finished and its lock entries were verified.
func (m noseyModel) applyInstallStep(done installStepDone) (noseyModel, tea.Cmd) {
	if done.generation != m.generation {
		return m, nil
	}
	total := len(m.install.commands)
	if m.install.cancelled {
		done.err = context.Canceled
	}
	if done.err == nil && done.index+1 < total {
		m.install.index = done.index + 1
		return m, m.runInstallStep(done.index + 1)
	}
	if m.install.cancel != nil {
		m.install.cancel()
	}
	m.install.running = false
	m.updates = nil
	m.message, m.warning = "", ""
	if done.err != nil {
		m.warning = fmt.Sprintf("Stopped at %s: %v; %s not started.", sourceLabel(m.install.commands[done.index]), done.err, countLabel(total-done.index-1, "later source"))
	} else {
		m.message = fmt.Sprintf("Installer finished for %s. Status updated below.", countLabel(total, "source"))
	}
	m.modal = ""
	m.generation++
	m.loading = true
	return m, m.load()
}

// installStatus describes what the installer is doing; it renders inside the page, never over it.
func (m noseyModel) installStatus() string {
	switch {
	case m.install.cancelled:
		return m.busy.View() + " " + m.styles.warn.Render("Cancelling installation…")
	case m.install.running && m.install.index >= 0:
		return m.busy.View() + " " + m.styles.accent.Render(fmt.Sprintf("Installing source %d of %d", m.install.index+1, len(m.install.commands)))
	case m.install.running:
		return m.busy.View() + " " + m.styles.accent.Render("Preparing installer…")
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

func (m noseyModel) installPageLines() []string {
	skills := 0
	for _, command := range m.install.commands {
		skills += len(command.Skills)
	}
	intro := "Confirm once to install the entire selection into this project. Existing skills with the same names will be replaced. No further prompts."
	if m.install.printOnly {
		intro = "Print mode: nothing runs here. Copy the commands to run them yourself."
	}
	width := max(10, m.width-2)
	lines := []string{m.styles.title.Render(fmt.Sprintf("Install %s from %s", countLabel(skills, "skill"), countLabel(len(m.install.commands), "source")))}
	if status := m.installStatus(); status != "" {
		lines = append(lines, status)
	}
	lines = append(lines, m.styles.muted.Render(intro), "")
	for i, command := range m.install.commands {
		lines = append(lines, m.sourceMarker(i)+" "+m.styles.repo.Render(sourceLabel(command)))
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
	if m.install.printOnly {
		lines = append(lines, "", m.styles.heading.Render("Commands"))
		for _, command := range m.install.commands {
			lines = append(lines, storage.Safe(command.Text()))
		}
	}
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")
}

func (m noseyModel) installBox(text string, width int) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(max(1, min(width, 72)))
	if os.Getenv("NO_COLOR") == "" {
		box = box.BorderForeground(lipgloss.Color("#94A3B8"))
	}
	return box.Render(text)
}

func sourceLabel(command installation.AddCommand) string {
	label := strings.TrimSuffix(strings.TrimPrefix(command.Source, "https://github.com/"), ".git")
	if ref := command.Ref; ref != "" {
		if len(ref) == 40 {
			ref = ref[:7]
		}
		label += " @" + ref
	}
	return storage.Safe(label)
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
