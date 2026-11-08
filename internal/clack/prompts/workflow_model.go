package prompts

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/clack/core"
)

// workflowModel is a Bubble Tea parent model; child prompts never launch
// nested programs and retain their own state, key bindings, and theme.
type workflowModel struct {
	builder *WorkflowBuilder
	steps   []*WorkflowStep
	index   int
	active  core.ComposablePrompt
	history []string
	banner  *animatedBannerModel
	err     error
	done    bool
}

type workflowRefreshMsg struct{}

func (m *workflowModel) Init() tea.Cmd {
	cmd := m.advance()
	if m.done || m.banner == nil || m.banner.complete() {
		return cmd
	}
	return tea.Batch(cmd, bannerTick(m.banner.interval))
}

func (m *workflowModel) advance() tea.Cmd {
	for m.index < len(m.steps) {
		step := m.steps[m.index]
		if step.Condition != nil && !step.Condition() {
			m.index++
			continue
		}
		m.active = step.Model()
		if m.active == nil {
			m.err = core.ErrCancelPrompt
			m.done = true
			return tea.Quit
		}
		cmd := m.active.Init()
		if !m.active.Finished() {
			return cmd
		}
		if errCmd := m.complete(); errCmd != nil {
			return errCmd
		}
		return cmd
	}
	m.done = true
	m.active = nil
	return tea.Quit
}

func (m *workflowModel) complete() tea.Cmd {
	if err := m.active.ResultError(); err != nil {
		m.err = err
		m.done = true
		if m.builder.onCancel != nil {
			m.builder.onCancel(m.steps[m.index].Name, err)
		}
		return tea.Quit
	}
	step := m.steps[m.index]
	if step.SetResult {
		m.builder.assign(step.Name, m.active.Result())
	}
	m.history = append(m.history, strings.TrimRight(m.active.BodyView().Content, "\n"))
	m.active = nil
	m.index++
	return m.advance()
}

func (m *workflowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	if _, ok := msg.(workflowRefreshMsg); ok {
		return m, nil
	}
	// The workflow owns banner timing. Do not forward animation messages
	// to the focused prompt or use the standalone banner's tea.Quit command.
	if _, ok := msg.(bannerTickMsg); ok {
		if m.banner == nil || m.banner.complete() {
			return m, nil
		}
		if m.banner.advance() {
			return m, bannerTick(m.banner.interval)
		}
		return m, nil
	}
	cmd := m.active.UpdateEmbedded(msg)
	if m.active.Finished() {
		return m, m.complete()
	}
	return m, cmd
}

func (m *workflowModel) View() tea.View {
	parts := make([]string, 0, 2+len(m.history))
	if header := core.PersistentHeader(); header != "" {
		parts = append(parts, header)
	}
	if m.banner != nil {
		parts = append(parts, m.banner.View().Content)
	}
	parts = append(parts, m.history...)
	if m.active != nil {
		parts = append(parts, strings.TrimRight(m.active.BodyView().Content, "\n"))
	}
	return tea.NewView(strings.Join(parts, "\n"))
}
