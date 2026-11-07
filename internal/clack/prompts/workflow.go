package prompts

import (
	"context"
	"errors"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/banner"
	"github.com/go-cli-template/internal/clack/reflectutil"
)

// WorkflowStep is one step in a workflow.
type WorkflowStep struct {
	Name string
	// CONTRACT: workflow steps return heterogeneous values.
	Prompt    func() (any, error)
	Condition func() bool
	SetResult bool
	// Model creates a native Bubble Tea child model, without nesting programs.
	// CONTRACT: Run/Step callbacks return heterogeneous workflow values.
	Model func() core.ComposablePrompt
}

// WorkflowBuilder composes workflow steps.
type WorkflowBuilder struct {
	steps          []*WorkflowStep
	onCancel       func(step string, err error)
	result         any
	input          *os.File
	output         *os.File
	context        context.Context
	animatedBanner *workflowBannerConfig
}

// workflowBannerConfig stores a single optional animated header. Its timing
// belongs to the workflow's Bubble Tea model, not a nested program.
type workflowBannerConfig struct {
	config    banner.Config
	animation BannerAnimationOptions
}

// WithAnimatedBanner renders a FIGlet banner above every native prompt step,
// revealing its rows while the user can continue interacting with the form.
// The banner remains visible after the animation completes. It is rendered
// statically when output is redirected, in CI, or on a dumb terminal.
func (w *WorkflowBuilder) WithAnimatedBanner(config banner.Config, animation BannerAnimationOptions) *WorkflowBuilder {
	w.animatedBanner = &workflowBannerConfig{config: config, animation: animation}
	return w
}

func (w *WorkflowBuilder) prepareAnimatedBanner(output *os.File) (*animatedBannerModel, error) {
	if w.animatedBanner == nil {
		return nil, nil
	}
	cfg := w.animatedBanner.config
	if cfg.Parser == "html" {
		return nil, errors.New("go-clack: animated workflow banners require terminal output, not html")
	}
	if cfg.Width == 0 {
		if width, _, err := term.GetSize(int(output.Fd())); err == nil && width >= 20 {
			cfg.Width = width
		}
	}
	art, err := banner.Render(cfg)
	if err != nil {
		return nil, err
	}
	static := !term.IsTerminal(int(output.Fd())) || os.Getenv("CI") != "" || os.Getenv("TERM") == "dumb"
	return newAnimatedBannerModel(art, w.animatedBanner.animation, static)
}

// Step adds a new step to the workflow with a prompt function to gather the step's result.
// CONTRACT: workflow steps return heterogeneous values.
func (w *WorkflowBuilder) Step(name string, prompt func() (any, error)) *WorkflowBuilder {
	w.steps = append(w.steps, &WorkflowStep{
		Name: name, Prompt: prompt,
		SetResult: true,
	})
	return w
}

// ConditionalStep adds a new step that runs only if the provided condition is true.
// CONTRACT: workflow steps return heterogeneous values.
func (w *WorkflowBuilder) ConditionalStep(name string, condition func() bool, prompt func() (any, error)) *WorkflowBuilder {
	w.steps = append(w.steps, &WorkflowStep{
		Name:      name,
		Condition: condition,
		Prompt:    prompt,
		SetResult: true,
	})
	return w
}

// ModelStep registers a native go-clack prompt as a child of one Bubble Tea
//
//	workflow. Example: ModelStep("Name", func() core.ComposablePrompt {
//	    return core.NewTextPrompt(core.TextPromptParams{Render: ...})
//	}). All consecutive model steps run in the same terminal program.
func (w *WorkflowBuilder) ModelStep(name string, model func() core.ComposablePrompt) *WorkflowBuilder {
	w.steps = append(w.steps, &WorkflowStep{Name: name, Model: model, SetResult: true})
	return w
}

// ConditionalModelStep is the composable equivalent of ConditionalStep.
func (w *WorkflowBuilder) ConditionalModelStep(name string, condition func() bool, model func() core.ComposablePrompt) *WorkflowBuilder {
	w.steps = append(w.steps, &WorkflowStep{Name: name, Condition: condition, Model: model, SetResult: true})
	return w
}

// WithIO sets the terminal streams for the composed Bubble Tea workflow.
func (w *WorkflowBuilder) WithIO(input, output *os.File) *WorkflowBuilder {
	w.input, w.output = input, output
	return w
}

// WithContext sets the lifetime of a composed Bubble Tea workflow.
func (w *WorkflowBuilder) WithContext(ctx context.Context) *WorkflowBuilder {
	w.context = ctx
	return w
}

// ForkStep adds a new step that runs a sub-workflow if the provided condition is true.
func (w *WorkflowBuilder) ForkStep(name string, condition func() bool, subWorkflow func() *WorkflowBuilder) *WorkflowBuilder {
	// CONTRACT: workflow steps return heterogeneous values.
	w.steps = append(w.steps, &WorkflowStep{
		Name:      name,
		Condition: condition,
		Prompt: func() (any, error) {
			err := subWorkflow().Run()
			return nil, err
		},
		SetResult: false,
	})
	return w
}

// LogStep adds a step that performs an action without affecting the workflow's result.
// Typically used for logging or other side effects.
func (w *WorkflowBuilder) LogStep(name string, action func()) *WorkflowBuilder {
	// CONTRACT: workflow steps return heterogeneous values.
	w.steps = append(w.steps, &WorkflowStep{
		Name: name,
		Prompt: func() (any, error) {
			action()
			return nil, nil
		},
		SetResult: false,
	})
	return w
}

// CustomStep adds a custom step to the workflow. The custom step can define its own behavior.
func (w *WorkflowBuilder) CustomStep(step *WorkflowStep) *WorkflowBuilder {
	w.steps = append(w.steps, step)
	return w
}

// OnCancel sets a callback function to be called if any step encounters an error and the workflow is onCanceled.
func (w *WorkflowBuilder) OnCancel(onCancel func(step string, err error)) *WorkflowBuilder {
	w.onCancel = onCancel
	return w
}

// Run executes workflow steps. Native ModelStep entries are grouped into one
// Bubble Tea session; legacy Step callbacks remain synchronous for source
// compatibility (they cannot be composed without changing their signature).
func (w *WorkflowBuilder) Run() error {
	for i := 0; i < len(w.steps); {
		if w.steps[i].Model != nil {
			end := i + 1
			for end < len(w.steps) && w.steps[end].Model != nil {
				end++
			}
			if err := w.runNative(w.steps[i:end]); err != nil {
				return err
			}
			i = end
			continue
		}
		step := w.steps[i]
		i++
		if step.Condition != nil && !step.Condition() {
			continue
		}
		if step.Prompt == nil {
			continue
		}
		value, err := step.Prompt()
		if err != nil {
			if w.onCancel != nil {
				w.onCancel(step.Name, err)
			}
			return err
		}
		if step.SetResult {
			w.assign(step.Name, value)
		}
	}
	return nil
}

// RunNative requires all steps to be ModelStep/ConditionalModelStep and drives
// the entire workflow within a single Bubble Tea program.
func (w *WorkflowBuilder) RunNative() error {
	for _, step := range w.steps {
		if step.Model == nil {
			return errors.New("go-clack: RunNative requires ModelStep; legacy Step callbacks cannot be composed")
		}
	}
	return w.runNative(w.steps)
}

// CONTRACT: Step callbacks return heterogeneous workflow values.
func (w *WorkflowBuilder) assign(name string, result any) {
	if err := reflectutil.AssignField(w.result, capitalize(name), result); err != nil {
		// PANICS: a workflow that names an unknown or non-settable result field
		// is a programmer error, not runtime input.
		panic(err)
	}
}

func (w *WorkflowBuilder) runNative(steps []*WorkflowStep) error {
	if len(steps) == 0 {
		return nil
	}
	input, output := w.input, w.output
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	ctx := w.context
	if ctx == nil {
		ctx = context.Background()
	}
	header, headerErr := w.prepareAnimatedBanner(output)
	if headerErr != nil {
		return headerErr
	}
	model := &workflowModel{builder: w, steps: steps, banner: header}
	opts := []tea.ProgramOption{tea.WithInput(input), tea.WithOutput(output), tea.WithContext(ctx)}
	if input == os.Stdin && !isTerminalInput() {
		opts = append(opts, tea.WithInput(nil), tea.WithWindowSize(80, 24))
	}
	program := tea.NewProgram(model, opts...)
	core.BeginProgram(program, output)
	defer core.EndProgram(program)
	core.SetPersistentForWorkflow(output, func() { program.Send(workflowRefreshMsg{}) })
	defer core.EndPersistentForWorkflow(output)
	_, err := program.Run()
	if model.err != nil {
		return model.err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Workflow returns a new WorkflowBuilder instance, allowing you to define a workflow that populates the provided value.
// The value should be a struct, and each field will be populated by a corresponding step in the workflow.
// If the field does not correspond to a struct's field, it panics.
// CONTRACT: Workflow accepts the dynamic result struct used for field-name assignment.
func Workflow(v any) *WorkflowBuilder {
	return &WorkflowBuilder{result: v}
}

func isTerminalInput() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func capitalize(str string) string {
	if str == "" {
		return str
	}
	return strings.ToUpper(string(str[0])) + (str[1:])
}
