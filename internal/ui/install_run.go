package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
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
}

type installReady struct {
	cli        installation.CLI
	err        error
	generation int
}
type installStepDone struct {
	index int
	err   error
}
type installCopied struct{ err error }

func (m noseyModel) installer() installation.Service {
	installer := m.session.Installer
	installer.Settings = m.session.Resolved.Settings
	return installer
}

// openInstall builds the commands for every selected skill, including those hidden by filters.
func (m noseyModel) openInstall() (noseyModel, tea.Cmd) {
	cfg := m.session.Resolved.Settings
	ids := []string{}
	for id := range m.selected {
		ids = append(ids, id)
	}
	opts := installation.Options{IDs: ids, Agents: slices.Clone(cfg.Installation.Agents), Method: cfg.Installation.Method}
	commands, err := m.installer().Build(m.session.Catalog, m.recs, opts)
	m.warning = ""
	m.err = err
	if err != nil {
		return m, nil
	}
	m.install = installRun{commands: commands, printOnly: cfg.Installation.Mode == "print"}
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
		installer, ctx, generation := m.installer(), m.ctx, m.generation
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
	if ready.err != nil {
		m.loading = false
		m.err = ready.err
		return m, nil
	}
	m.install.cli = ready.cli
	return m, m.runInstallStep(0)
}

func (m noseyModel) runInstallStep(index int) tea.Cmd {
	command := m.installer().AddProcess(m.ctx, m.install.cli, m.install.commands[index])
	return tea.ExecProcess(command, func(err error) tea.Msg { return installStepDone{index: index, err: err} })
}

// applyInstallStep continues to the next source. The installer exits 0 when cancelled,
// so success here only means it finished; the reloaded inventory reports what is installed.
func (m noseyModel) applyInstallStep(done installStepDone) (noseyModel, tea.Cmd) {
	total := len(m.install.commands)
	if done.err == nil && done.index+1 < total {
		return m, m.runInstallStep(done.index + 1)
	}
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

func (m noseyModel) installPageLines() []string {
	skills := 0
	for _, command := range m.install.commands {
		skills += len(command.Skills)
	}
	intro := "The skills installer will ask you to confirm each source."
	if m.install.printOnly {
		intro = "Print mode: nothing runs here. Copy the commands to run them yourself."
	}
	lines := []string{m.styles.heading.Render(fmt.Sprintf("Install %s from %s", countLabel(skills, "skill"), countLabel(len(m.install.commands), "source"))), intro, ""}
	for _, command := range m.install.commands {
		lines = append(lines, sourceLabel(command))
		for _, skill := range command.Skills {
			line := "  " + storage.Safe(skill.Name)
			if skill.Required {
				line += "  (required)"
			}
			lines = append(lines, line)
		}
	}
	agents := "installer will ask"
	if configured := m.session.Resolved.Settings.Installation.Agents; len(configured) > 0 {
		agents = strings.Join(configured, ", ")
	}
	lines = append(lines, "", "Agents: "+storage.Safe(agents))
	if m.install.printOnly {
		lines = append(lines, "")
		for _, command := range m.install.commands {
			lines = append(lines, storage.Safe(command.Text()))
		}
	}
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), max(10, m.width-2), true), "\n")
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
