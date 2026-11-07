# Authoring and revision playbook

## New feature

Decide whether each view is fullscreen or inline before designing layout. Inline views use `presentation: "inline"`, `cli` metadata, scrolling output/prompt nodes and terminal exit codes. Native Clack prompt features also require `clackbt.json` with verified Go model/field bindings. For clackbt-specific forms, start with [clackbt workflow](clackbt-workflow.md); the generic [inline CLI workflow](inline-cli.md) remains useful for stream-level contracts.

1. Read the brief and present an **initial provisional plan (v0)** in conversation: scope, likely screens and flows, constraints, assumptions and alternatives. **Do this before asking the user design questions.** Follow the interview protocol in [elicitation.md](elicitation.md).
2. Interview the user in focused rounds. After each reply, incorporate decisions into a visible **revised plan (v1, v2, …)**, briefly explain the delta, and ask the next material questions. Keep iterating until blockers are answered, delegated or removed from scope.
3. Present the consolidated plan and request **explicit approval to generate**. If changes are requested, revise and interview again. Record the approved plan version, user sign-off and prior rounds in `DECISIONS.md` once writing begins. Do not scaffold or write generated artifacts before approval.
4. Derive the repo root using `git rev-parse --show-toplevel` (preferred), or have the user specify the root. Example: `ROOT=$(git rev-parse --show-toplevel)`. The root must be known before writing, but a provisional plan can be discussed earlier.
5. Select a feature slug: `[a-z0-9]+(-[a-z0-9]+)*`. **Only after approval and question closure**, optionally run `node <skill-root>/scripts/clackbt.mjs scaffold --repo-root "$ROOT" --feature "$SLUG" --mode greenfield` to prepare a draft in the required feature directory.
6. Start from the Markdown and JSON templates in `assets/templates/`. Replace every placeholder with domain-specific content. Preserve required filenames, array/object shapes, and version string. Never ship unfilled templates as final output.
7. For clackbt prompts, author `clackbt.json` to bind exported Go result fields, constructor APIs, native runner, TTY/cancellation, and screens/states. Author `contracts.json` event IDs and data sources first; author screen nodes and state-machine transitions using those same IDs. Synchronize Markdown last.
8. Run `node <skill-root>/scripts/clackbt.mjs build --repo-root "$ROOT" --feature "$SLUG" --strict`. Correct all errors; this generates both `validation-report.json` and the offline `contracts-viewer.html`. To rebuild all feature folders use `build --all`, or `watch --all` while editing canonical JSON.
9. Optionally run `skills-ref validate /path/to/clackbt-flow-spec` when that reference tool is installed, to validate the skill package metadata (separate from output validation).
10. Present exact paths, validation result, and the HTML explorer; include assumptions recorded from delegated choices.

## Update an existing feature

- Read `manifest.json`, canonical JSON and `DECISIONS.md` before proposing changes; inspect evidence where present.
- **Present an initial change plan/delta in chat**, including what screens/contracts and stable IDs would change and what might break. Do not edit files yet.
- Interview the user about newly material unknowns and choices; revise the change plan after each response, and get **explicit approval** of the final scope before editing. Do not repeat already-settled questions unless the requirements have changed.
- Keep stable IDs for unchanged semantics. Treat removed nodes, events, or states as breaking contract changes; document in `DECISIONS.md` along with plan versions and approval.
- Update `SPEC.md`, `FLOWS.md`, `CONTRACTS.md`, and machine-readable files as a single change; rerun the validator and regenerate the HTML viewer.
- Do not overwrite unrelated feature folders; do not write generated artifacts outside `.work/flow-spec/<feature>/`.

## Failure handling

- If repo root cannot be established, ask for the root **before writing**, not necessarily before showing v0.
- If the user cannot answer a material question and has not delegated defaults, continue the interview or stop with stated blockers; do not generate artifacts.
- If the user has not approved the final plan, do not scaffold, generate files, or mark status `ready`.
- If validator reports errors, leave status `draft`, fix or report the exact errors, and do not claim handoff-ready.
- If Python is unavailable, execute `references/contracts.md` manual checklist and clearly label validation as *manual and unverified by script*.
- If generating output solely as a downloadable starter outside a user's repo, preserve the correct repository-relative layout in the package; disclose that it must be copied into the user's repository. Do not claim it was committed or installed.

## Document contracts for an existing application

1. Follow [existing-app.md](existing-app.md) for read-only source inspection, evidence inventory and the plan-first interview. Show v0 **after source inspection**, using observed facts rather than inventing behavior.
2. Set `manifest.source_mode` to `existing-app` and `manifest.artifact_scope` to `contracts-only` (default) or `full` (if screens and complete flows are requested). Set a stable feature slug for the existing surface.
3. Ask questions and **revise the extraction plan in visible rounds**. Obtain user approval of the consolidated scope; only then create `.work/flow-spec/<feature>/`. Do not modify app code or run untrusted code without permission.
4. Start with `assets/templates/manifest.existing-app.template.json` and `assets/templates/evidence.template.json`. Fill `contracts.json` from observed application behavior. Produce `EVIDENCE.md` with the inventory, evidence mapping, limitations, and known gaps. Record plan changes, decisions and approval in `DECISIONS.md`.
5. For contracts-only, omit screen tree and machine files entirely; use `CONTRACTS.md`, `DECISIONS.md`, `EVIDENCE.md`, `manifest.json`, `contracts.json`, `evidence.json`, `validation-report.json`, and `contracts-viewer.html`. For full reconstruction, include the same screen/flow artifacts as greenfield and both evidence files.
6. Every top-level contract entity must appear in `evidence.json` claims with exact `ref` and source locations. If a contract is only inferred, identify its uncertainty and keep the manifest in `draft`.
7. Build the interactive viewer, validate, and deliver. Indicate which behavior was derived from source versus user confirmation, plus unverified areas.

## Re-scan for drift

When source changes after contract extraction, reread changed files and **present the proposed contract delta for interview and approval** before applying a semantic update. Rerun the validator and regenerate the HTML viewer after edits. If source hashes were recorded, a mismatch fails validation until citations/contracts are updated. Re-inspect changed handlers, screens and models before bumping the baseline. `ready` means **internally consistent and source-backed at the inspected revision**, not that runtime semantics have been proven.
