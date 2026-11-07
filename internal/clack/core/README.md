# Low-level prompt models (`internal/clack/core`)

`core` implements input handling, prompt state, validation, rendering hooks, and the underlying models used by the styled `prompts` package. This module uses **native Bubble Tea v2**. It is an internal package belonging to the Go CLI starter, not a separately installable upstream dependency.

## Available models

| Prompt model | Constructor |
| --- | --- |
| Text | `core.NewTextPrompt` |
| Password | `core.NewPasswordPrompt` |
| Path input | `core.NewPathPrompt` |
| Confirmation | `core.NewConfirmPrompt` |
| Single selection | `core.NewSelectPrompt` |
| Multi-selection | `core.NewMultiSelectPrompt` |
| Grouped multi-selection | `core.NewGroupMultiSelectPrompt` |
| Select by key | `core.NewSelectKeyPrompt` |
| Ordered reordering | `core.NewReorderPrompt` |
| Select a path | `core.NewSelectPathPrompt` |
| Select multiple paths | `core.NewMultiSelectPathPrompt` |

For pre-styled versions of these models, use `internal/clack/prompts` rather than writing a renderer.

## Create a custom text prompt

The low-level constructor requires a `Render` function. Its prompt carries the current input, state, and cursor position:

```go
package main

import (
    "fmt"
    "os"

    "github.com/go-cli-template/internal/clack/core"
)

func main() {
    p := core.NewTextPrompt(core.TextPromptParams{
        Placeholder: "Ada",
        Required: true,
        Render: func(p *core.TextPrompt) string {
            return fmt.Sprintf("Name?\n%s", p.ValueWithCursor())
        },
    })

    name, err := p.Run()
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    fmt.Println("Hello, " + name + "!")
}
```

This example belongs **inside the same Go module**, due to the `internal/` import rule. A prompt's `.Run()` method launches a standalone Bubble Tea program and returns its typed value and an error. Cancellation is represented by `core.ErrCancelPrompt`.

## Compose instead of nesting

If a CLI asks more than one question, construct styled `prompts.*Model` objects and add them to a `prompts.Workflow` with `ModelStep`. `RunNative()` drives those models in one program. Do not call multiple `.Run()` methods from inside another active Bubble Tea update loop.

See the complete native composition example in [`../README.md`](../README.md) and the real command in `cmd/go-cli-template/prompt.go`.

## Testing and customization

The package includes tests for core prompt behaviors, state transitions, and cancellation. Keep model update logic deterministic where possible; use the repository tests as patterns. Changes to terminal input, rendering, or model contracts should be validated with `go test ./internal/clack/core/...` and the full `go test ./...` suite.

The bundled code retains its upstream [MIT license](../LICENSE).
