# Prompt form gallery

`examples/forms` is a **separate example executable** bundled with the CLI starter. It showcases the Clack-style prompts migrated to native Bubble Tea v2. Completed forms render inline, preserving terminal scrollback.

## Run

From the repository root with the Go toolchain declared by `go.mod`:

```sh
go run ./examples/forms list             # list available demos
go run ./examples/forms text             # run one prompt
go run ./examples/forms multi-select     # run a selection form
go run ./examples/forms all              # run all demos in sequence
go run ./examples/forms help             # usage summary
```

With Just and `zsh`, use `just forms list`, `just forms text`, or `just forms` (the last defaults to `all`). Forms require an interactive terminal.

## Available examples

| Demo | Demonstration |
| --- | --- |
| `banner` | Static FIGlet banner |
| `banner-animated` | Inline FIGlet banner revealed row by row (static when redirected or in CI) |
| `banner-effects` | Horizontal wipe, moving gradient sweep, and dither fade |
| `banner-workflow-sweep` | Gradient sweep animation while a native text prompt is interactive |
| `banner-workflow` | FIGlet banner animating over an interactive Bubble Tea form |
| `messages` | Intro, step, message, info, success, warn, error, outro |
| `note` | Styled note box |
| `text` | Required text input and validation |
| `password` | Masked input |
| `confirm` | Yes/no decision |
| `select` | Filterable single choice |
| `multi-select` | Multiple choices with hints |
| `group-multi-select` | Grouped multiple choices |
| `select-key` | Choose by key press |
| `reorder` | Rearrange an ordered list |
| `path` | Path text input and completion |
| `select-path` | Browse and choose a filesystem path |
| `multi-select-path` | Choose several paths |
| `spinner` | Work-in-progress feedback and final status |
| `tasks` | Sequential task feedback |

## Source layout

- `main.go`: demo registry, argument dispatch, and cancellation handling.
- `input.go`: text, password, confirmation, and path input demos.
- `selection.go`: select, multi-select, grouped select, select-key, reorder.
- `path_selection.go`: filesystem path selection examples.
- `output.go`: banner, messages, note, spinner, and tasks.

Every demo is an ordinary Go function returning `error`. `main.go` recognizes cancellation using `prompts.IsCancel(err)` and prints `Cancelled.` without treating it as an application failure.

## Copy a form into your CLI

For one prompt, use `prompts.Text` / `prompts.Confirm` and handle the returned error. For a multi-question form, prefer the native workflow below:

```go
import (
    "github.com/go-cli-template/internal/clack/core"
    "github.com/go-cli-template/internal/clack/prompts"
)

var answers struct{ Name string }
err := prompts.Workflow(&answers).
    ModelStep("Name", func() core.ComposablePrompt {
        return prompts.TextModel(prompts.TextParams{
            Message: "Name?", Required: true,
        })
    }).
    RunNative()
if err != nil {
    return err
}
```

The packages live under `internal/` and are importable only from the permitted module tree. To use them in a different Go module, bring the source into that module and keep the upstream licenses.

See [the runtime overview](../../internal/clack/README.md), [styled API](../../internal/clack/prompts/README.md), and [sample CLI command](../../cmd/go-cli-template/prompt.go).
