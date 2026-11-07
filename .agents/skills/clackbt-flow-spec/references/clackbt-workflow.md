# clackbt-specific design and contract workflow

## Discovery → plan → approval

1. Read the user brief or inspect source (`cmd/go-cli-template`, `internal/clack/prompts`, `internal/clack/core`, `examples/forms`, tests, current `go.mod`). Distinguish observed facts, inferred behavior, and proposed feature additions.
2. Show a **provisional plan** describing each prompt step, its result-field binding and model constructor, validation, selection/options, TTY detection, disabled/CI/pipe behavior, cancellation vs explicit No, downstream effects, output streams, and exit codes. Interview only for unanswered material decisions. Revise plan when answers arrive.
3. Request explicit approval before scaffolding/writing feature specifications. Do not mark unresolved decisions `ready`. Log approval and decisions in `DECISIONS.md`.
4. For full mode, create the canonical `manifest.json`, `screens.json`, `machine.json`, `contracts.json`, `clackbt.json`, `design.json`, `scenarios.json`, and Markdown alongside them under `<repo>/.work/flow-spec/<feature>/`. For an existing-app contracts-only extraction, create canonical `evidence.json` and document source line ranges instead of invented machine states. No speculative observed claims.
5. Run `node <skill>/scripts/clackbt.mjs build --repo-root <repo> --feature <slug> --strict`. Correct errors; deliver the viewer, sample renderings, and honest test status.

## Modeling rules

- Prefer `surface: inline`, `interaction: native-workflow` for a multi-question Clack sequence. The parent `RunNative()` owns the Bubble Tea event loop; every prompt is a `*Model` constructor satisfying `core.ComposablePrompt`. `Step`/`ConditionalStep`, nested `Run()`, and callback-only custom prompts cannot appear in a native chain.
- A one-off prompt uses `interaction: single-prompt` and a verified `prompts.Text`/`Confirm`/etc helper. For static banners, messages, and tasks use `interaction: output-only`. `fullscreen` is a separate `internal/ui` design surface, not a Clack prompt.
- Each `clackbt.json` step must bind a unique `field` to an exported field of the planned result struct. Document the Go type and how it matches the model value. `ConditionalModelStep` may be used only with a condition explained in the state/guard contracts.
- `steps[].screen_id` must name a screen in `screens.json`, the same screen must be `presentation: inline`, `cli.interaction: prompted`, `cli.stdin: tty`; related `states` must exist in `machine.json`. Actual keybindings and validation outcomes must be backed by transitions and event contracts, not merely visual previews.
- `clackbt.json` is a *backend binding manifest* that maps canonical screen/flow IDs to source APIs; canonical `screens.json`/`machine.json`/`contracts.json` still govern behavior and output. Do not invent source implementation by providing backend binding.
- Specify `tty.required`, `tty.non_tty` and `io.stdout`/`io.stderr`/`io.machine_output`; never let styling corrupt a claimed machine-readable stream. A TTY prompt is not a valid fallback for piped stdin unless the user explicitly defines one.
- Cancellation `core.ErrCancelPrompt`, a response of `false` from `ConfirmModel`, and `context.Canceled` are distinct. Record mapping to CLI exit statuses at Cobra boundary; do not assume a default policy.
- Mark explicit labels, required/defaults, validators, option values, secret masking, and path safety. For generic select/reorder APIs, use comparable Go values. `Password` results must never appear in mockup fixtures, logs, example transcripts or state snapshots.
- On tasks, side effects must be idempotency-aware; favor `RunTasks` over `Tasks` for observable failure. Spinners and banners have static CI/redirected-output fallbacks. Avoid implying live animation in HTML.
- Clack transcript mockups are illustrative, not byte-identical renders; normal Go code and actual terminal run are the verification source.

## Recommended test cases

- Happy path for each model / multi-step chain; field-binding correctness and model type compatibility.
- Empty required input, validator error then retry, invalid selection, boundaries/options, path access failure.
- Ctrl+C/Esc/EOF behavior distinguished from user choosing No, context cancellation and operation errors.
- No-TTY/noninteractive/CI/pipe, output stream and exit-code behavior, ANSI/no-color/dumb terminal and narrow width.
- Native workflow persistence/history, conditional step skipping, initial field defaults, banners alongside active prompt.
- Unit tests for command and domain behavior; Bubble Tea interaction tests via `github.com/charmbracelet/x/exp/teatest/v2` as used by `internal/clack/prompts/`.
