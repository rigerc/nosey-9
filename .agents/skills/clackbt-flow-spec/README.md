# clackbt Flow Spec — Agent Skill v2.0.0

A specialized successor to the uploaded **TUI Screen Flow Spec v1.6.0**. This build is for [`rigerc/clackbt`](https://github.com/rigerc/clackbt): a Go/Cobra starter with an **internal**, adapted Clack-style prompt runtime running **Bubble Tea v2**. It produces structured prompt/workflow specifications, source-grounded contracts, state graphs, terminal snapshots and standalone HTML previews. It **does not** install, compile, or execute clackbt itself.

## What's different from v1.6.0

| Before | clackbt-specific rebuild |
| --- | --- |
| Generic full-screen TUI/inline CLI | **Inline Clack prompt workflows by default**, optional other surfaces kept for migration |
| Framework-neutral component types and optional termcn registry | **Real clackbt Go model catalog**: TextModel, ConfirmModel, SelectModel, etc.; termcn importer not included |
| Unbound screen/state JSON | Added **`clackbt.json`** Go API, result struct, input/TTY/output/cancellation bindings |
| Generic prompt mockups | Added **`clack-prompt`, `clack-message`, `clack-note`, `clack-spinner`** adapters and `clackbt-preview.html` / `clackbt-transcript.txt` |
| Basic contract validation | Added **clackbt semantic validation** for fields, model names, native workflow restrictions, interactive screens, secret examples, cancel handling |
| Generic examples | New **source-informed native two-step prompt example** plus retained legacy regression fixtures |

## Repository-specific behavior

- `internal/clack/core/` and `/prompts/` are packages **inside** the starter's Go module, not a separately published package. On the source snapshot consulted, imports used `github.com/go-cli-template/internal/clack/...`; always follow the current project's `go.mod` after any rename.
- The real `cmd/go-cli-template/prompt.go` builds two native model steps, `Name` (`TextModel`) and `Confirm` (`ConfirmModel`), and calls `RunNative`. `.Run()` on prompts must not be nested in a running Bubble Tea update loop.
- `ModelStep("Name",...)` needs an exported, settable `Name` field on the result struct. A false confirmation means **declined**, not a `core.ErrCancelPrompt` user interrupt.
- `prompts.IsCancel(err)` is for prompt interrupts; `ExitOnError` calls `os.Exit`. `RunTasks` propagates task errors where `Tasks` discards them.
- Inline prompts require a TTY contract; output streaming/redirects, error handling and process exit codes must be consciously designed.

See [clackbt source map](references/clackbt-source.md) and [workflow guidance](references/clackbt-workflow.md). The repository was **private** when inspected, so use an authorized connector or local checkout to update observations.

## Installation

Copy or unzip the folder `clackbt-flow-spec` into your agent skills directory, or install from an extracted copy into a target repository:

```bash
node /path/to/clackbt-flow-spec/scripts/clackbt.mjs install \
  --repo-root /path/to/target-repo
```

This puts the skill in `.agents/skills/clackbt-flow-spec/` and task shortcuts in `.agents/prompts/`. To overwrite an earlier install, use `--force`; to omit examples/tests in the installed copy, use `--minimal`.

Requirements for the skill's **offline tooling** are Node.js >=18 and Python >=3.9. No npm dependencies. The target Go app itself has separate requirements (the inspected `go.mod` specified **Go 1.26.10**).

## Quick start

For an illustrative project without touching your app code:

```bash
# Create validated worked examples inside a project's output directory
node scripts/clackbt.mjs demo --repo-root /path/to/project

# Run the source-informed native two-question example
node scripts/clackbt.mjs build --repo-root /path/to/project \
  --feature example-clackbt-prompt --strict

# Generate an intentionally incomplete draft for a new feature
node scripts/clackbt.mjs scaffold --repo-root /path/to/project \
  --feature configure-release --surface inline \
  --title 'Configure release'

# Once the agent has authored JSON/Markdown after plan approval:
node scripts/clackbt.mjs build --repo-root /path/to/project \
  --feature configure-release --strict
```

For real user work: first propose a provisional plan in chat, clarify unknown decisions and obtain explicit sign-off. Do not treat `scaffold` as permission to write new specs. New artifacts always go to `<repo-root>/.work/flow-spec/<feature-slug>/` for compatibility with the original skill.

## Files produced

| File | Role |
| --- | --- |
| `manifest.json` | Title, scope, mode, draft/ready status, assumptions/questions |
| `clackbt.json` | Go module, Cobra invocation, native workflow steps and result bindings, terminal/output policies |
| `screens.json`, `machine.json` | Inline prompt views and state transitions (full mode) |
| `contracts.json` | Typed UI/state/events/commands/data/validation contracts |
| `evidence.json` | Source claims for existing-app extraction; not used in greenfield |
| `SPEC.md`, `FLOWS.md`, `CONTRACTS.md`, `DECISIONS.md` | Human-readable specification and decision/approval record |
| `validation-report.json` | Structural, cross-file and clackbt-specific checks |
| `contracts-viewer.html` | Interactive offline contract inspector |
| `storyboard.html`, `mockups/*` | Generated multi-state storyboards and TXT/SVG/ANSI semantic mockups |
| `clackbt-preview.html`, `clackbt-transcript.txt` | New clack-style illustrated prompt transcript snapshots |

Full-mode `screens.json` and `machine.json` still use schema version `1.0.0`; `clackbt.json` also uses `1.0.0` and is documented by [clackbt JSON schema](assets/schemas/clackbt.schema.json). `design.json` and `scenarios.json` are optional illustrative preview configuration. `contracts.json` is the behavioral authority; `clackbt.json` is an implementation-binding overlay, not a substitute.

## Command reference

```text
clackbt.mjs help
clackbt.mjs install --repo-root PATH [--force] [--minimal]
clackbt.mjs inventory --repo-root PATH [--limit N]
clackbt.mjs scaffold --repo-root PATH --feature SLUG [--mode greenfield|existing|existing-full] [--surface inline|fullscreen]
clackbt.mjs validate --repo-root PATH --feature SLUG
clackbt.mjs visualize --repo-root PATH --feature SLUG
clackbt.mjs clack-preview --repo-root PATH --feature SLUG
clackbt.mjs mockup --repo-root PATH --feature SLUG [--cols N --rows N --viewport NAME --scenario NAME --outputs txt,svg,ansi,html]
clackbt.mjs build --repo-root PATH --feature SLUG [--strict]
clackbt.mjs build --repo-root PATH --all [--strict]
clackbt.mjs watch --repo-root PATH --all [--strict]
clackbt.mjs demo --repo-root PATH [--force]
clackbt.mjs test
```

`watch` watches contract files on the current process; no asynchronous agent work is performed. Validators and renderers operate only on local JSON/Markdown. Do not interpret passing **spec validation** as passing **Go tests**.

## Example native workflow in clackbt

This comes from the verified source API shape, **not** a generated project source file:

```go
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
```

Include TTY gating and proper error handling in the real Cobra command. The source-informed example at `examples/example-clackbt-prompt/` covers input acceptance, retry, explicit No, Ctrl+C, success/failure and output boundaries. Its Ctrl+C **exit code 130 is proposed** for illustration; do not claim the repository implements it.

## Validation and maintenance

```bash
node scripts/clackbt.mjs test
# For a real target, only if authorized to execute app code and matching Go installed:
go test ./internal/clack/core/...
go test ./internal/clack/prompts/...
go test ./...
```

Skill tests include Python contract validation, Node mockup regression tests, and clackbt adapter checks. Runtime Go tests require the target repository and its toolchain; this package does not vendor or execute clackbt Go source. If the target repository changes, re-read its README, `AGENTS.md`, Go sources and tests, refresh `references/clackbt-source.md` and revise the adapter catalog as necessary. Existing extracted evidence can drift; refresh paths/digests rather than treating prior evidence as current.

## Rights and provenance

This skill adapts the user-supplied `tui-screen-flow-spec` v1.6.0 architecture and documentation to clackbt. It does **not** redistribute clackbt's private Go source. The underlying clackbt runtime maintains its own MIT/third-party notices; do not remove them when implementing app changes. Keep any original skill license files preserved in the package.
