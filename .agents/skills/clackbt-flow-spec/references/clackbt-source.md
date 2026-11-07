# clackbt source-of-truth map

Repository: `rigerc/clackbt` (private; inspect using authorized repository access).
Observed source snapshot while adapting: `20118081ca02608ec56cd0440d0bedd6762e3087`. Re-check source on each new use; do not assume this snapshot is current.

## Stack and critical constraints

- `go.mod`: module as observed in docs/imports `github.com/go-cli-template`; treat the **current local go.mod** as authoritative after renaming. `go.mod` declares Go **1.26.10** in the inspected snapshot; verify installed toolchain before tests.
- `cmd/go-cli-template/prompt.go`: Cobra `newPromptCmd()`, `runPrompt`, `requireInteractive(os.Stdin, os.Stdout)`; composed `TextModel` and `ConfirmModel` under `RunNative()`; a negative business confirmation prints `Cancelled.` but is **not** the same as `core.ErrCancelPrompt`.
- `internal/clack/README.md`: vendored, adapted native Bubble Tea v2 prompt runtime; **internal package** within the Go starter, not a standalone publicly importable `clackbt` module.
- `internal/clack/prompts/README.md`: high-level helpers, model constructors, workflow binding, banner effects, tasks, cancellation.
- `internal/clack/core/README.md`: lower-level model constructors, custom rendering, deterministic testing.
- `internal/clack/prompts/workflow.go`: `Workflow`, `ModelStep`, `ConditionalModelStep`, `RunNative`, `WithIO`, `WithContext`, `WithAnimatedBanner`, `OnCancel`. RunNative rejects callback-style `Step`. Workflow field assignment expects exported, settable fields matching step names; mismatches can panic.
- `internal/clack/prompts/workflow_model.go`: a parent Bubble Tea model owns active `core.ComposablePrompt`, preserves previous step render output as history, handles cancellation and banner ticks in the same event loop. Do not launch nested Bubble Tea programs.
- `internal/clack/prompts/theme/`, `symbols/`: Clack-style lines, glyphs, colors and narrow/no-Unicode behavior. Only execution is faithful; generated mockups are approximations.
- `internal/clack/prompts/banner_animation_effects.go`: `BannerEffectRows`, `BannerEffectWipe`, `BannerEffectGradientSweep`, `BannerEffectFade`; static fallbacks on redirected output / CI / dumb terminals.
- `examples/forms/`: runnable gallery for text, password, confirm, select, multi-select, grouped multi-select, key selection, reorder, path, path selection, multiple path selection, messages, notes, spinners, tasks, and banners.
- `internal/ui/`: separate fullscreen Bubble Tea list demo. Do not treat it as the Clack prompt runtime.
- `AGENTS.md`: maintain Cobra scriptable path, gofmt, testing; do not invent file mutation behavior in root read-only demo.

## Verified API catalog (snapshot, update when source changes)

| One-shot helper | Composable model | Low-level core |
| --- | --- | --- |
| `prompts.Text` | `prompts.TextModel` | `core.NewTextPrompt` |
| `prompts.Password` | `prompts.PasswordModel` | `core.NewPasswordPrompt` |
| `prompts.Confirm` | `prompts.ConfirmModel` | `core.NewConfirmPrompt` |
| `prompts.Select[T]` | `prompts.SelectModel[T]` | `core.NewSelectPrompt` |
| `prompts.MultiSelect[T]` | `prompts.MultiSelectModel[T]` | `core.NewMultiSelectPrompt` |
| `prompts.GroupMultiSelect[T]` | `prompts.GroupMultiSelectModel[T]` | `core.NewGroupMultiSelectPrompt` |
| `prompts.SelectKey[T]` | `prompts.SelectKeyModel[T]` | `core.NewSelectKeyPrompt` |
| `prompts.Reorder[T]` | `prompts.ReorderModel[T]` | `core.NewReorderPrompt` |
| `prompts.Path` | `prompts.PathModel` | `core.NewPathPrompt` |
| `prompts.SelectPath` | `prompts.SelectPathModel` | `core.NewSelectPathPrompt` |
| `prompts.MultiSelectPath` | `prompts.MultiSelectPathModel` | `core.NewMultiSelectPathPrompt` |

Output helpers: `Intro`, `Outro`, `Step`, `Message`, `Info`, `Success`, `Warn`, `Error`, `Cancel`, `Note`, `Banner`, `AnimatedBanner`, `Spinner`, `RunTasks`. `Tasks` discards task errors; prefer `RunTasks` when failures matter. `prompts.IsCancel(err)` checks `core.ErrCancelPrompt`. `prompts.ExitOnError` calls `os.Exit` and must not appear in reusable/business code.

## Access and verification

Do read-only source inspection first. Prefer direct GitHub connector for a private repository, or a user-authorized local clone. Read actual signatures/fields before writing Go code. Do not execute tests/build of untrusted repositories without authorization. The standalone skill's validators and HTML renderers inspect JSON contracts only and do not execute Go. If user grants test permission and Go 1.26.10 is available: `go test ./internal/clack/core/...`, `go test ./internal/clack/prompts/...`, `go test ./...`; CLI smoke `go run ./cmd/go-cli-template prompt` needs a TTY. Never label tests run unless run.
