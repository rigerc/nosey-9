# Agent instructions — nosey

This repository is the **nosey application**, not the ClackBT starter.
Bundled example commands and agent skills are references, not product
requirements. Favor application behavior over starter maintenance.

- Implement the requested behavior, keep Cobra parsing thin, and use testable
  packages under `internal/`.
- Add relevant success/error-path tests and run `go test ./...` when possible.
- Keep flags, Cobra command descriptions, examples, and `--help` accurate.
- Do **not** update README or other Markdown for routine commands, bug fixes,
  and refactors. Update README when installation or essential user-facing
  workflows materially change or the task explicitly asks for docs.
- Do not create flow specifications, storyboards, documentation sites, or
  generated artifacts unless explicitly requested. The optional
  `clackbt-flow-spec` skill writes to ignored `.work/flow-spec/`.
- Treat demonstration commands, prompts, and the read-only directory-list UI
  as replaceable examples, not product requirements.
- Preserve licenses and upstream notices in `internal/clack/`,
  `third_party/`, and bundled skills.
- Do not run publishing, sync or external generators unless requested.
- Summarize changed behavior and verification in chat; don't produce a new
  Markdown report for every task.
