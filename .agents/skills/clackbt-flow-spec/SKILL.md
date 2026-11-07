---
name: clackbt-flow-spec
description: Only when explicitly requested: design, audit, or reconstruct inline Clack-style Go CLI prompt flows specifically for rigerc/clackbt (the Cobra and Bubble Tea v2 CLI starter). Produce model-to-result bindings, canonical screen/state/event/command/validation contracts, TTY/cancellation semantics, source-backed evidence, and offline terminal previews. Supports native multi-step Workflow/ModelStep/RunNative, one-shot prompts, menus, paths, banners, progress, and prompt gallery integration. Always plan, clarify, and obtain approval before generating feature artifacts.
compatibility: Agent Skills-compatible coding agent with filesystem access. Node.js 18+, Python 3.9+; no third-party runtime dependencies for the included generators.
metadata:
  version: "2.0.0"
  upstream-skill: "tui-screen-flow-spec 1.6.0"
  target-repository: "rigerc/clackbt"
  output-version: "1.0.0"
---

# clackbt flow specification skill

This is the **clackbt-specific successor** to the generic `tui-screen-flow-spec` skill. Its primary surface is an **inline Clack-style prompt** within an existing terminal scrollback, implemented with Go, Cobra, and **native Bubble Tea v2**. Preserve the original skill's contracts, evidence, deterministic viewers, and state mockups, but bind prompt steps to **real `internal/clack` Go APIs**, not Ink/OpenTUI/termcn or imaginary third-party imports.

**Opt-in only:** do not invoke this skill for routine command changes, bug fixes, refactors, or tests. Only use it when the user explicitly requests a formal flow specification, contract extraction, or preview artifacts.

**Default deliverable when invoked:** design specs and implementation-ready contracts, *not automatically changed Go application code*. Only write/modify application Go source if explicitly asked. An HTML preview is not proof that code compiles, a Go program was executed, or a terminal faithfully rendered a frame.

## Non-negotiable rules

1. **Inspect first, especially existing code.** Read the target repo's `AGENTS.md`, current `go.mod`, `cmd/go-cli-template/prompt.go`, `internal/clack/README.md`, `internal/clack/prompts/README.md`, `internal/clack/core/README.md`, actual model/params implementations, and relevant tests before making API-specific claims. See [repository source map](references/clackbt-source.md). `rigerc/clackbt` was private when this variant was built; access only through permitted connectors or a local clone. Do not presume it is public. If unavailable, distinguish known snapshot from current unverified source.
2. **Provisional plan → clarification → revision → explicit approval.** Before writing generated artifacts, show the user a brief proposed flow: command invocation, question types and order, field names and types, conditional branches, validation, cancellation versus declined confirmation, output/exit behavior and terminal constraints. Ask focused, material questions, revise the plan after answers, and request an explicit go-ahead. Do not treat initial plan as approved. Record actual approval in `DECISIONS.md`. Do not fabricate approval or call a plan `ready` with unresolved blockers. See [elicitation](references/elicitation.md).
3. **One Bubble Tea event loop.** For multi-step prompts, prefer `prompts.Workflow(&result).ModelStep(...).RunNative()` and `ConditionalModelStep` when needed. Each step's constructor must return `core.ComposablePrompt` (`prompts.*Model`). Do **not** use `Step`, `ConditionalStep`, `ForkStep`, `LogStep` inside `RunNative()`, and never nest a prompt `.Run()` inside another active Bubble Tea program. `Run()` supports legacy mixed steps but is not the default native form.
4. **Use actual Go type and scope rules.** `internal/clack/...` is part of the starter's Go module, not a public `clackbt` import path. Read the **current** `go.mod` before choosing imports; the source snapshot used `github.com/go-cli-template`. Each `ModelStep("Field", ...)` binds to a corresponding **exported settable result struct field**. Missing/mismatched fields can panic. Select/reorder values must satisfy Go type constraints; no JavaScript validators or TS/Ink props.
5. **Keep CLI behavior scriptable.** A prompted flow requires explicit TTY handling. Specify the result if stdin or stdout is not an interactive terminal, how `--yes` / explicit flags (if any) bypass prompts, when output is human-readable versus machine-readable, stdout vs stderr, side effects, and process exit codes. Do not assume the example CLI's `requireInteractive` and generated API automatically handle every pipe or CI case.
6. **Distinguish cancellation modes.** `prompts.IsCancel(err)` checks `core.ErrCancelPrompt`; it is not equivalent to a `ConfirmModel` returning `false`. Context cancellation and failures are separate. Do not invent signal exit statuses. Never call `prompts.ExitOnError(err)` from reusable code: it invokes `os.Exit`.
7. **Fixed root and output.** Resolve project root via `git rev-parse --show-toplevel`, or obtain an explicitly identified root. Write every generated feature under `<repo-root>/.work/flow-spec/<feature-slug>/` (lowercase kebab-case); do not write generated output in this skill folder or app source. No symlink traversal.
8. **Canonical artifacts.** `contracts.json` governs UI/state/event/command/data/validation contracts; `screens.json` and `machine.json` govern layout and state transitions in full-mode; `clackbt.json` binds those IDs to clackbt model constructors, exported Go fields, runtime policy, and sample data. Markdown cannot override canonical JSON. `evidence.json` is canonical for source-backed reverse engineering. Schema versions are `1.0.0` unless otherwise defined for optional visual config.
9. **No unverified observed behavior.** In existing-app extraction, cite exact source paths and line numbers plus source hashes when marking ready. README assertions and this skill's examples alone are not implementation evidence. Separate `observed`, `inferred`, `proposed`, and `user-confirmed` claims. Do not execute target Go commands, test suite, build scripts, or remote code without permission; JSON validation and local offline HTML generation are safe.
10. **Validate and render.** After approval and authoring, run `node <skill-root>/scripts/clackbt.mjs build --repo-root <repo-root> --feature <slug> --strict`. Fix errors, then deliver the contract explorer, storyboard/per-state TXT/SVG where applicable, `clackbt-preview.html` and `clackbt-transcript.txt`. These are **illustrative static views**, not live runtime. If Node unavailable, use `python3 <skill-root>/scripts/validate.py ...` and clearly state which visual artifacts were not generated.

## Choose workflow

- **New clackbt prompt feature**: default `source_mode: brief`, `artifact_scope: full`, `presentation: inline`, `interaction: native-workflow` for multiple questions. Follow [clackbt workflow](references/clackbt-workflow.md) and [generic contracts](references/contracts.md).
- **One-question prompt**: `interaction: single-prompt`; use verified one-shot helpers (`prompts.Text`, `Confirm`, etc.) and typed result/error contract, unless it must compose with other active prompts.
- **Output-only sequence**: `interaction: output-only` for messages/notes/spinners/tasks/banners; no `ModelStep`. Specify redirects, static fallbacks and errors. `prompts.RunTasks` reports errors; `prompts.Tasks` discards them.
- **Existing clackbt project**: read-only inventory, extract current contracts with `evidence.json`; default `artifact_scope: contracts-only`. Present a provisional extraction plan *after inspection*, then interview and request approval. Use full scope only on request. See [existing-app rules](references/existing-app.md).
- **Mixed fullscreen demo + inline prompts**: represent separate screens/presentation modes if needed; model explicit transitions. `internal/ui` fullscreen Bubble Tea list is **not** a Clack prompt. The default read-only list is not a file manager.

## clackbt binding manifest

For a full-mode prompt flow, add `clackbt.json` matching [schema](assets/schemas/clackbt.schema.json). It contains:

- `backend: "clackbt-go"`, `go_module`, command, `presentation: "inline"`, and `interaction: "native-workflow" | "single-prompt" | "output-only"`.
- `implementation.runner: "RunNative"` and `allow_legacy_steps: false` for native chains; `runner: "one-shot"` for one-off prompts.
- `tty` gate, `io` output stream contract, `cancellation` error/handler/policy and terminal success code.
- `result_struct` map of actual exported Go fields and Go types.
- Ordered `steps`: `id`, `field`, `screen_id`, `states`, `api` (e.g. `TextModel`, `ConfirmModel`, `SelectModel`), typed `params` mirroring current Go source, and **non-secret** sample `preview` values. Use `conditional` / `condition` when the flow contains a guard.

The adapter validates model name, field binding, known screen/state IDs, native-runner consistency, TTY/prompt surface, cancellation semantics and secret preview leakage. It does **not** compile a Go program or prove current Go param compatibility. Check actual source signatures and add Go tests if implementation is requested.

## Prompt/API catalog

| Prompt kind | Styled model constructor | Typical result |
| --- | --- | --- |
| Text | `prompts.TextModel` | `string` |
| Password | `prompts.PasswordModel` | `string` (never expose example value) |
| Confirm | `prompts.ConfirmModel` | `bool` |
| Select, SelectKey | `prompts.SelectModel[T]`, `prompts.SelectKeyModel[T]` | comparable `T` |
| MultiSelect, GroupMultiSelect | `prompts.MultiSelectModel[T]`, `prompts.GroupMultiSelectModel[T]` | `[]T` |
| Reorder | `prompts.ReorderModel[T]` | `[]T` |
| Path / SelectPath | `prompts.PathModel`, `prompts.SelectPathModel` | `string` |
| MultiSelectPath | `prompts.MultiSelectPathModel` | slice |

One-shot counterparts omit `Model` (`Text`, `Confirm`, `Select`, etc.). For custom rendering, see `core.New*Prompt` in current source; a low-level constructor alone is **not** a styled Clack workflow.

## CLI and examples

```bash
# From the installed skill root
node scripts/clackbt.mjs help
node scripts/clackbt.mjs scaffold --repo-root /path/to/repo --feature configure-deployment --surface inline
# Agent fills all canonical files after user approval
node scripts/clackbt.mjs build --repo-root /path/to/repo --feature configure-deployment --strict
node scripts/clackbt.mjs demo --repo-root /path/to/repo
node scripts/clackbt.mjs test
```

The scaffold is intentionally an **incomplete draft**, not an approved specification. For an illustrative two-step example see `examples/example-clackbt-prompt/` and [README](README.md). `scripts/tui.mjs` remains as a compatibility alias.

## Handoff checklist

Before claiming complete: confirmed actual Go module/imports and prompt signatures; all step-to-field mappings and state references pass; each UI event has a transition, each command side effect has outcomes; TTY, keyboard/validation, cancel/decline, stderr/stdout, secret handling, terminal exit statuses and noninteractive paths are explicit; `DECISIONS.md` logs approval; existing-app observed claims have evidence; HTML/TXT/SVG snapshots are marked synthetic; validators were actually run; runtime tests reported separately and truthfully. For behavior change requests, re-run the approval process for the affected scope, update canonical JSON and Markdown together, and rebuild.
