# Plan-first interview and iterative approval gate

## Principle

**Show the user a provisional initial plan before interviewing them.** For a new terminal UI (fullscreen TUI or inline CLI), derive v0 from the supplied brief. For an existing TUI, **inspect code and tests first**, then show an evidence-grounded extraction/reconstruction plan. Plans are conversation drafts, **not generated spec/contract files**. Mark proposed behavior, assumptions, and open choices explicitly. The user should be able to criticize the structure of the plan, not only answer an intake questionnaire.

A first plan is a starting hypothesis, **not approval**. Interview in rounds, incorporate feedback, show the revised plan and change summary, and request explicit sign-off only once material blockers are resolved or delegated. Do not create or scaffold deliverables before that sign-off. Prefer the host's native structured-question/AskTool when available; otherwise ask concise questions in chat. Never fabricate question-tool calls.

**“Choose sensible defaults,” “you decide,” or equivalent delegates the named choices**, not necessarily every unknown and not approval of the entire plan. Record the defaults and delegation. An explicit “approved,” “go ahead,” “proceed with this plan,” or equivalent is plan sign-off; silence, a partial answer, and “looks interesting” are not. If the user explicitly requests a shorter/one-pass process, keep the provisional plan and approval checkpoint, and ask only unresolved material blockers.

## What the initial plan must contain

- **Goal and boundaries:** target user and jobs, included/excluded capabilities, and (for existing apps) inspected source surface and intended scope (`contracts-only` or `full`).
- **Workflow sketch:** likely screens and entry/exit/navigation paths for greenfield; code surfaces, contract categories and provenance approach for existing apps. Include major states, effects, errors, and constraints where known.
- **Confidence markers:** separate **given/observed** facts from **provisional suggestions** and **open decisions**. For existing apps, never call a speculative screen or transition observed.
- **Interview starting point:** 2–5 highest-impact questions or a concise first topical batch, each tied to a decision that could change the plan. Invite free-form edits (e.g. “What would you change about this plan?”).

Keep v0 short enough for a person to review. Do **not** exhaustively design every component or presuppose answers in the plan. If there is too little information to propose implementation semantics, present a scope/process plan with explicit placeholders and ask the first blocker questions rather than inventing behavior.

## Materiality test

Ask if different plausible answers would change screen count/purpose, layout tree, navigation, keybindings/focus, guards or states, data shape/source, async boundaries, commands/effects, form validation, confirmations, authorization/privacy, output location, terminal capabilities, or acceptance criteria. Skip trivia that will not change artifacts. Existing-app questions concern *unknown* behavior, not observed facts already established from source.

## Interview and revision loop

1. **Prepare v0:** read the brief or inspect the existing application and identify known facts, likely approach, alternatives, and open decisions. Present v0 in chat **before any material design questions**.
2. **Interview:** ask a focused, coherent group of material questions (typically 2–5); use multiple-choice only where choices are genuinely bounded. Ask for corrections to the plan as a whole. Do not launch an enormous questionnaire.
3. **Apply answers visibly:** after each user response, state decisions made/delegated, show the updated plan (v1, v2, …), and summarize what changed since the previous version. Note contradictions and newly uncovered gaps rather than silently choosing a side.
4. **Repeat selectively:** ask the next questions for remaining blockers or dependencies, grouped by topic. Do not repeat settled questions. If the user revises an earlier answer, supersede it explicitly in the plan and decision history.
5. **Approval checkpoint:** when material decisions are answered, delegated or explicitly excluded, show one consolidated plan, outstanding **nonblocking** assumptions, and ask **“Approve this plan so I can generate the artifacts, or what should change?”** Require an affirmative go-ahead. If feedback changes scope, return to step 3; if blockers remain, keep interviewing.
6. **Author only after approval:** scaffold/generate files, and log Q-IDs, answers, decisions, plan revisions, delegated assumptions and the approving user response in `DECISIONS.md`. No plan approval means no generated artifacts and no `ready` status.
7. **Re-enter the loop for revisions:** when later requests change a material design decision or contract, present the proposed delta first, interview about its effects, get approval for the updated plan, then synchronize JSON/Markdown/evidence and rerun validation.

The interview may take one round when a brief is complete or several when important choices remain. Do not ask for formal sign-off on every minor answer; **one approval checkpoint at the end** of an iteration sequence is sufficient.

## Question bank (select relevant questions, never use as a rote list)

| Area | Clarify when missing | Useful answer options |
| --- | --- | --- |
| User and goal | Who performs which primary tasks? | end user / operator / administrator |
| Entry/exit | Which view opens first and what marks task completion? | list / dashboard / wizard; save / submit / close |
| Screen navigation | Which screens and Back/Escape behavior? | stack / direct navigation / cancel dialog |
| Terminal footprint | Minimum width/height and narrow-terminal behavior? | fixed minimum / collapse / vertical stack |
| Input and focus | Keyboard-only, mouse optional, focus wrap, shortcuts? | keyboard only / both; wrap / stop |
| Data source | Live service, local file, in-memory, demo? | API / file / local store / unresolved blocker |
| Data semantics | Identifiers, required fields, sort, filters, pagination? | choose from product domain |
| Async/error | What happens on pending, failure, retry, timeout? | blocking overlay / inline status / retry |
| Persistence/effects | Are operations reversible, persisted, or destructive? | persisted / ephemeral; undo / confirmation |
| Validation | Required fields, allowed values, duplicate policy? | explicit domain rule choices |
| Roles/safety | Authorization, sensitive data, destructive actions? | roles; confirm / deny / log |
| Completion | Acceptance behavior and important failure scenarios? | product-specific expected outcomes |

For **inline CLIs**, additionally establish invocation/flag syntax, output channel separation (stdout vs stderr), machine-readable modes, redirected streams, TTY detection, noninteractive fallback, exit codes, Ctrl+C/EOF and progress/spinner cleanup. See [inline CLI guidance](inline-cli.md). These are material contract decisions, not visual styling.

## Decision record

Write the conversation's **substantive** evolution in `DECISIONS.md` when generation begins:

- Stable question IDs `Q-<number>`, question, answer/delegation, source (`user` or `delegated`), and affected contract IDs.
- A plan-review history (v0 → v1 → …), with each revision's feedback and meaningful scope/flow changes; final version and the user's explicit sign-off.
- Any superseded choices, out-of-scope items, and remaining nonblocking assumptions. No unresolved material blockers are compatible with `ready`.

Do not imply that mere presentation of v0 constituted approval. If the user refuses required decisions and does not delegate or reduce scope, stop at the interview stage; explain which decisions block generation.

## Example

Brief: “Design a TUI to delete projects.” Start with a **v0 plan**: project list → select project → confirmation dialog → deletion result, with persistence, reversibility and failure behavior clearly marked unknown. Then ask about confirmation, undo, permissions and storage errors. After each answer, show an updated flow and what changed. On completion, ask for plan approval before specifying the component tree, guard/transitions and `project.delete` command. Do not silently select irreversible deletion.

## Existing-application interview rules

Inspect and enumerate observed screens/components, events, effects, state, data and validation **before drafting the extraction plan**. State what has code/test evidence and what remains an inference. Ask about target feature boundaries, current behavior vs desired change, configuration-dependent behavior, runtime/plugin branches, and conflicts between tests/docs/code. Never ask users to decide how already-observed code works.

If behavior cannot be verified, request runtime traces, additional source, permission to run code/tests, or an explicit scope exclusion; record user assertions as `user-confirmed` rather than `observed`. A plausible inference or a user-approved *proposed* enhancement is still not source-observed. Keep an existing-app baseline `draft` if material inferred/proposed behavior remains within its claimed current-behavior scope.
