# Bundled Clack-style prompt runtime

`internal/clack/` contains a **vendored, adapted** Go prompt runtime migrated to [Bubble Tea v2](https://github.com/charmbracelet/bubbletea). It supplies both high-level styled helpers and low-level models for composing terminal forms. It is included in this CLI starter, **not** published here as a standalone public Go module.

Because the packages live under `internal/`, only code within the parent module's permitted import tree can import them. If you want to use them in a different project, move/copy the source into that project's module (maintaining applicable licenses) or extract a standalone package.

## Choose an API

| Use case | Package / API |
| --- | --- |
| Ask one question | `prompts.Text`, `prompts.Confirm`, `prompts.Select`, etc. |
| Run several questions in one program | `prompts.Workflow(&result).ModelStep(...).RunNative()` |
| Build a custom themed prompt | `core.NewTextPrompt` and other `core.New*Prompt` constructors |
| Show progress or messages | `prompts.Spinner`, `prompts.RunTasks`, `prompts.Intro`, `prompts.Note`, etc. |
| Explore every built-in prompt | `go run ./examples/forms list` |

## Composed form example

The prompt constructors used for `ModelStep` return models implementing `core.ComposablePrompt`:

```go
import (
    "github.com/rigerc/nosey-9/internal/clack/core"
    "github.com/rigerc/nosey-9/internal/clack/prompts"
)

var answers struct {
    Name    string
    Confirm bool
}

err := prompts.Workflow(&answers).
    ModelStep("Name", func() core.ComposablePrompt {
        return prompts.TextModel(prompts.TextParams{
            Message: "Your name?",
            Required: true,
        })
    }).
    ModelStep("Confirm", func() core.ComposablePrompt {
        return prompts.ConfirmModel(prompts.ConfirmParams{
            Message: "Proceed?",
            InitialValue: true,
        })
    }).
    RunNative()
if err != nil {
    return err
}
```

`RunNative` requires only model-based steps. Legacy callback `Step` methods still exist for compatibility, but they cannot be run inside the same native program. Use native model workflows for new multi-step terminal experiences.

`prompts.IsCancel(err)` reports `core.ErrCancelPrompt`; handle cancellation separately from operational errors when appropriate. For example, `examples/forms/main.go` treats cancellation as a clean end of a demo.

## Further reading

- [Core prompt models](core/README.md): building custom prompts.
- [Styled prompt APIs](prompts/README.md): constructors, workflows, output, and cancellation.
- [Form gallery](../../examples/forms/README.md): examples you can run.

**License:** the bundled runtime retains its upstream MIT license in [LICENSE](LICENSE). Its internal third-party files have their own source notices where applicable.
