# Styled prompts (`internal/clack/prompts`)

`prompts` wraps `internal/clack/core` with the starter's Clack-style terminal appearance. It offers one-shot prompts, composable native Bubble Tea v2 models, messages, notes, banners, spinners, and tasks.

These paths are **internal to the starter module**. Do not describe them as an independently installable package from a separate project. For runnable examples, see [`examples/forms/`](../../../examples/forms/README.md).

## Single interactive question

```go
package main

import (
    "fmt"
    "os"

    "github.com/rigerc/nosey-9/internal/clack/prompts"
)

func main() {
    name, err := prompts.Text(prompts.TextParams{
        Message: "What's your name?",
        Required: true,
    })
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    fmt.Printf("Hello, %s!\n", name)
}
```

`prompts.Text` returns `(string, error)`; similar one-shot functions exist for the other forms. They require a terminal for interaction.

## Prompt families

| Family | One-shot helper | Composable constructor |
| --- | --- | --- |
| Text | `Text` | `TextModel` |
| Password | `Password` | `PasswordModel` |
| Confirmation | `Confirm` | `ConfirmModel` |
| Single selection | `Select[T]` | `SelectModel[T]` |
| Multi-selection | `MultiSelect[T]` | `MultiSelectModel[T]` |
| Grouped multi-selection | `GroupMultiSelect[T]` | `GroupMultiSelectModel[T]` |
| Key-based selection | `SelectKey[T]` | `SelectKeyModel[T]` |
| Reordering | `Reorder[T]` | `ReorderModel[T]` |
| Path entry | `Path` | `PathModel` |
| Path selection | `SelectPath` | `SelectPathModel` |
| Multiple paths | `MultiSelectPath` | `MultiSelectPathModel` |

The generic selection/reorder types use comparable values. Each API has its own options structure, such as `TextParams`, `SelectParams[T]`, or `ConfirmParams`.

## Compose a multi-question workflow

Use `Workflow(&result)` and `ModelStep` to run a series of models under **one Bubble Tea program**:

```go
import (
    "github.com/rigerc/nosey-9/internal/clack/core"
    "github.com/rigerc/nosey-9/internal/clack/prompts"
)

var result struct {
    Name    string
    Confirm bool
}

err := prompts.Workflow(&result).
    ModelStep("Name", func() core.ComposablePrompt {
        return prompts.TextModel(prompts.TextParams{
            Message: "Your name?", Required: true,
        })
    }).
    ModelStep("Confirm", func() core.ComposablePrompt {
        return prompts.ConfirmModel(prompts.ConfirmParams{
            Message: "Continue?", InitialValue: true,
        })
    }).
    RunNative()
if err != nil {
    return err
}
```

`ModelStep` names populate **exported fields with matching names** on the result struct. Name mismatches or non-settable fields are treated as programmer errors and may panic. `ConditionalModelStep` adds a model only if its condition is true.

`RunNative` rejects legacy `Step` callbacks; they run separately under `Run()`, which groups consecutive model steps but executes callbacks synchronously. Prefer `RunNative` for new forms.

## Standalone banner animation effects

`AnimatedBanner` accepts a `BannerAnimationOptions.Effect` value. Existing
callers that omit `Effect` retain the original row-by-row reveal.

```go
err := prompts.AnimatedBanner("WELCOME", prompts.BannerOptions{
    Config: banner.Config{Font: "standard", Gradient: &banner.GradientAurora},
}, prompts.BannerAnimationOptions{
    Effect:        prompts.BannerEffectWipe,
    FrameInterval: 55 * time.Millisecond,
})
```

Available effects:

- `BannerEffectRows` (default): reveal each row in order.
- `BannerEffectWipe`: reveal terminal columns from left to right; ANSI styles
  and wide glyphs are handled with Charm's ANSI-aware text utilities.
- `BannerEffectGradientSweep`: shift the actual rendered TrueColor stops
  across the artwork. When the banner has no RGB colors, this falls back
  to a plain horizontal wipe.
- `BannerEffectFade`: reveal glyphs progressively in a stable dither pattern.
  This does not assume a dark terminal background or depend on true alpha.

Every effect restores the exact rendered FIGlet output on its final frame.
Animations automatically use the existing static fallback in CI, for
redirected output, and inside another active Bubble Tea program.

Try `go run ./examples/forms banner-effects`.

## Animated banners inside native workflows

To animate a banner while a form is active, configure the **parent workflow**
rather than launching a second Bubble Tea program:

```go
import (
    "time"

    "github.com/rigerc/nosey-9/internal/clack/core"
    "github.com/rigerc/nosey-9/internal/clack/prompts"
    "github.com/rigerc/nosey-9/internal/clack/prompts/banner"
)

var result struct{ Name string }
err := prompts.Workflow(&result).
    WithAnimatedBanner(
        banner.Config{Text: "WELCOME", Font: "standard", Gradient: &banner.GradientOcean},
        prompts.BannerAnimationOptions{FrameInterval: 75 * time.Millisecond},
    ).
    ModelStep("Name", func() core.ComposablePrompt {
        return prompts.TextModel(prompts.TextParams{Message: "Your name?"})
    }).
    RunNative()
```

`WithAnimatedBanner` renders the art once and advances it inside the same
Bubble Tea event loop as the active prompt. The full banner remains visible
above subsequent steps. The animation automatically becomes static on
redirected output, in CI and on dumb terminals. The existing `SetPersistent`
components remain supported and appear before this banner.

## Choices and validation

Create generic select options using `[]*prompts.SelectOption[string]`. For example:

```go
choice, err := prompts.Select(prompts.SelectParams[string]{
    Message: "Environment?",
    Options: []*prompts.SelectOption[string]{
        {Label: "Development", Value: "dev"},
        {Label: "Production", Value: "prod"},
    },
    Required: true,
})
```

`TextParams` supports `Message`, `Placeholder`, `InitialValue`, `Required`, and a `Validate func(string) error` callback. Do not use JavaScript-style `value.length` expressions: validation is ordinary Go.

## Cancellation

Cancellation is reported as `core.ErrCancelPrompt`. Use `prompts.IsCancel(err)` when cancellation should be distinct from an operational error:

```go
value, err := prompts.Text(prompts.TextParams{Message: "Name?"})
if prompts.IsCancel(err) {
    prompts.Cancel("Cancelled.")
    return nil
}
if err != nil {
    return err
}
_ = value
```

This snippet assumes a surrounding function returning `error`. At a top-level CLI boundary, `prompts.ExitOnError(err)` is also available, but **it calls `os.Exit`**, which is inappropriate for reusable libraries or unit-testable business logic.

## Output and progress APIs

- `Intro`, `Outro`, `Step`, `Message`, `Info`, `Success`, `Warn`, `Error`, `Cancel`: styled terminal output.
- `Note`: bordered formatted note.
- `Banner`: FIGlet-style banner with configurable options.
- `AnimatedBanner`: reveals a FIGlet banner row by row using Bubble Tea's inline renderer; falls back to `Banner` for redirected output, CI, or an active Bubble Tea session.
- `Spinner`: returns a controller with status methods; see `spinner.go` and `examples/forms/output.go`.
- `RunTasks`: runs a sequence of tasks and returns an error; `Tasks` is a convenience wrapper that discards it.

Prefer `RunTasks` when failures matter. Many output helpers write to terminal streams directly; keep those effects at CLI boundaries. The gallery in `examples/forms` demonstrates how they look and how to handle errors.

## Where to look next

- [`../README.md`](../README.md) for the integration overview.
- [`../core/README.md`](../core/README.md) for custom renderers.
- [`../../../examples/forms/README.md`](../../../examples/forms/README.md) to run every demo.
- `cmd/nosey/prompt.go` for a real two-step Cobra command.

The bundled runtime retains its upstream [MIT license](../LICENSE).
