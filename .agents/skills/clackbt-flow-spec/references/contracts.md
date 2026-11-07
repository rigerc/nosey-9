# Canonical artifact definitions — schema version 1.0.0

In full mode the four baseline JSON files are the source of truth; in existing-app mode, `evidence.json` adds provenance. Contracts-only mode requires only `manifest.json`, `contracts.json` and `evidence.json` as machine-readable files. Use the corresponding JSON Schemas under `assets/schemas/` for field types. The bundled validator also performs cross-file semantic checks not expressible in isolated JSON Schemas.

## manifest.json

Fields: `schema_version`, `feature_id` (matches folder slug), `title`, `status` (`draft` or `ready`), `entry_screen`, `source_brief`, `files` (fixed baseline artifact filenames), `assumptions` (string list), and `open_questions` (string list). `ready` requires `open_questions: []` and no validation errors. The `files` list must contain `SPEC.md`, `FLOWS.md`, `CONTRACTS.md`, `DECISIONS.md`, `manifest.json`, `screens.json`, `machine.json`, and `contracts.json`; include `validation-report.json` after validation.

## screens.json

- `screens[]`: unique `id`, `title`, `purpose`, `min_size: {cols, rows}`, `regions[]`, `focus_order[]`, `keybindings[]`, `states[]`.
- Regions form recursive nodes, each with `id`, `kind`, optional `label`, `orientation`, `data_ref`, `focusable`, `on` (mapping gesture such as `activate` to a declared event ID), and `children[]`.
- Node IDs are unique **within a screen**; for UI contract cross references, use `(screen, node_id)`.
- A focus order must reference focusable nodes on that screen exactly once, in keyboard traversal order. Non-focusable nodes must not appear in it.
- `keybindings` entries have `key`, `event` and optionally `scope` (`screen` or a node ID). The same normalized key cannot occur more than once in the same scope of a given screen. Normalization is case-insensitive for modifiers and named keys (`Ctrl+S` = `ctrl+s`, `Enter` = `enter`; `Shift+g` = `G`) but a bare single character keeps its case, so `g` and `G` are distinct bindings. A node-scoped key that shadows a screen-level key is reported as a warning. Keybindings invoke declared events.
- `states[]` lists the machine state IDs that render this screen. Each machine state belongs to exactly one screen.

## machine.json

- `initial`: ID of a state; must belong to the manifest entry screen.
- `states[]`: `{id, screen, terminal, description}`. Terminal state means end of this modeled feature flow, not process termination.
- `guards[]`: `{id, expression, description}`; expressions are framework-agnostic, human-readable predicates with explicit types/fields.
- `transitions[]`: `{id, from, event, to, guard, commands, description}`; `guard` is `null` or declared guard ID; `commands` are declared command IDs.
- Model paths for success, failure, retries, cancellation, and Back/Escape where applicable. Never use implicit transitions or implicit side effects.
- If distinct outcomes follow a command, use separate outcome events and transitions. A command-triggered transition may go to a pending state; the command declares every outcome event it may emit.
- All reachable nonterminal states need outgoing transitions. Every state must be reachable from `initial`. Unused guards are reported as warnings.
- Transitions must be deterministic: when several transitions share the same `from` state and `event`, each needs a distinct guard. An unguarded transition next to others, or the same guard twice, is an error. (The validator cannot prove guards are mutually exclusive; review that manually.)
- Events that are declared but never referenced, and `command`/`system` events that no transition handles, are reported as warnings.

## contracts.json

All sections are mandatory, though some collections may be empty where legitimately inapplicable.

- `ui.components[]`: `{id, screen, node_id, kind, props, emits}`. One per interactive or data-bound node. `props[]` include `{name, type, required, source}` (source may be a data source ID, a state-field name, or a literal explanation). `emits[]` are declared event IDs. `kind` matches the screen node kind.
- `state.fields[]`: `{name, type, initial, scope, description}`. Scope `feature` or screen ID. `state.invariants[]`: `{id, expression, description}`. Initial values must match declared conceptual types; the validator performs only basic checks, not a language-specific type check.
- `events[]`: `{id, payload, description, origin}`. Payload is a map of field names to framework-neutral type strings, or `{}` for no payload. Origin is `ui`, `command`, or `system`.
- `commands[]`: `{id, trigger, input, outcomes, side_effect, idempotent}`. `trigger` and each `outcomes[]` are event IDs. `input` maps field names to type strings. Commands are the effect boundary; no unmodeled storage or network effects.
- `data.models[]`: `{name, fields}`. Fields are `{name, type, required, description}`. `data.sources[]`: `{id, model, cardinality, provider, description}`. Cardinality `one` or `many`; model must exist, provider is a concrete technology-independent source description, not an unexplained `TBD` in a ready spec.
- `validation.rules[]`: `{id, target, when, predicate, message}`. `target` uses `Model.field` or state-field name, `when` is an event ID or named lifecycle stage (e.g. `on_submit`), `predicate` is a deterministic expression, and `message` is user-visible feedback.

## Cross-file acceptance checklist

1. All JSON is parseable, has matching `schema_version: "1.0.0"`, and satisfies documented required fields.
2. IDs unique within their namespaces; machine initial and manifest entry screen exist and agree.
3. Every region/node belongs to a screen, node IDs are unique per screen, focus order resolves exactly the focusable nodes, and every keybinding has a declared event.
4. Every state maps to a real screen and appears in that screen's `states`; every transition from/to, guard, event, and command resolves; all states reachable and nonterminal states have outgoing transitions.
5. UI components map to real node IDs/kinds and emit declared events; emitted UI events are handled by some transition whose `from` state renders that screen.
6. Data-bound nodes resolve to declared `data.sources`; every data source resolves to a model; the same IDs and types are used in Markdown.
7. Commands reference declared trigger and outcome events; outcome event origins are `command`.
8. Validation targets resolve to model fields or declared state fields; referenced event-based triggers exist; messages are non-empty.
9. No `TBD`/`TODO` markers (upper case, in Markdown prose outside code spans), `{{template}}` tokens, or unresolved material questions remain when status is `ready`. Ordinary words such as a `todo` task status are fine.
10. All generated artifacts remain under the fixed repo-relative output directory. `validation-report.json` reflects the latest validation run.

The built-in Python validator checks structural and referential basics, not full execution semantics, universal type correctness, accessibility compliance, or the truth of guards; review those manually.


## Brownfield (existing-app) contract extraction

The greenfield baseline above remains unchanged. The optional `manifest.source_mode` selects `brief` or `existing-app` (default `brief`). The optional `manifest.artifact_scope` selects `full` or `contracts-only` (default `full`); `contracts-only` is allowed only for `existing-app`. In contracts-only mode, `manifest.entry_screen` may be omitted; `screens.json` and `machine.json` are not required. The canonical `contracts.json` structure remains identical. UI component `screen` and `node_id` values identify real rendered/handled surfaces derived from the source, not invented layout nodes. `state.fields[].scope` may name an observed screen or `feature`.

Required contracts-only files are `CONTRACTS.md`, `DECISIONS.md`, `EVIDENCE.md`, `manifest.json`, `contracts.json`, `evidence.json`, and `validation-report.json`. Existing-app full mode requires the regular greenfield files **plus** `EVIDENCE.md` and `evidence.json`.

`evidence.json` contains a claim for every contract entity. In existing-app full mode the claim refs also include `screens.<id>`, `machine.states.<id>` and `machine.transitions.<id>`; they are required (an error) once `status` is `ready` and reported as warnings while the baseline is `draft`. A claim `ref` has one of these forms: `ui.components.<id>`, `state.fields.<name>`, `state.invariants.<id>`, `events.<id>`, `commands.<id>`, `data.models.<name>`, `data.sources.<id>`, `validation.rules.<id>`. Each claim has `classification` (`observed`, `user-confirmed`, `inferred`, `proposed`), an explanatory `rationale`, and zero or more `sources`, each with a repo-relative `path`, 1-based `line_start`, `line_end`, and optionally a source `kind` (`code`, `test`, `config`, `doc`; inferred from the file extension when omitted) and a full-file SHA-256 `sha256`. `observed` requires source locations, at least one non-`doc` source, and a `sha256` on every source once the baseline is `ready`; generated artifacts under `.work/flow-spec/` cannot be cited as evidence; `user-confirmed` requires a rationale citing one or more `Q-<number>` IDs that all exist in `DECISIONS.md` (the legacy `Q1` spelling is still read); `inferred`/`proposed` prevent `ready`. `evidence.json.gaps` lists unresolved or resolved interpretation gaps with a status. Open material gaps prevent `ready`. See `assets/schemas/evidence.schema.json`.

The validator checks contract internal references, claim coverage, source existence and line ranges, optional source digests, no inferred/proposed `ready` claims, and the standard question gate. It cannot prove event/control flow completeness, execution behavior, or semantic correspondence of source citations to claims. Review those during handoff.


## Type strings (recommended vocabulary)

Payload, prop, field and input types are free-form strings, but contracts are portable between agents when they share one vocabulary: scalars `string`, `int`, `number`, `bool`, `timestamp`; collections `T[]` and `map<string,T>`; optional `T?`; enumerations `enum(a|b|c)`; references to a declared model by its exact name. The validator does not enforce this vocabulary.

## Inline CLI-specific contracts

For inline CLI views, use `screens[*].presentation="inline"` and `screens[*].cli` for invocation, input/interaction mode; use `machine.states[*].exit_code` for terminal outcomes. Document option/argument parsing, stdin/TTY behavior, stdout vs stderr boundaries, shell exit status, structured output modes, redirection, Ctrl+C and EOF, and transient progress in human-readable spec and contract descriptions. See [inline CLI guidance](inline-cli.md). The preview is not a process emulator.
