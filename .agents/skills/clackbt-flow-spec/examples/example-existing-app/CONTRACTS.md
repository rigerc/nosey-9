# Implemented TUI contracts — task example (brownfield)

**Scope:** contract-only baseline derived from `tests/fixtures/existing_app.py` in the installed skill; not a proposed design.

## UI components

- `task-list-view` (`task-list / list-output`): displays `Task[]` from `tasks`; Enter uses `task.open`.
- `task-delete-action` (`task-detail / delete-hotkey`): D requests `task.delete.request`; the handler directly invokes `delete_current`.
- `task-back-action` (`task-detail / escape-hotkey`): Esc returns to list, emitting `nav.back`.

## State

- `screen`: active view, initially `task-list`.
- `selected_id`: task identity, initially empty.
- `error_message`: last deletion error, initially empty.

## Event and command contracts

- UI events: `task.open`, `task.delete.request` (normalized name for key-driven request), `nav.back`.
- Command: `task.delete` wraps `TaskRepository.delete(selected_id)` and mutates the JSON file. Outcomes are `task.deleted` or `task.delete.failed`; no reliable idempotency guarantee.
- Event names in the contract are logical names; the implementation dispatches via direct Python calls. The `task.delete.request` name is normalized for the key input rather than a literal return value in source. No event payload is emitted by `on_key`; the task ID is passed as a handler argument / stored state.

## Data

`Task { id: string, title: string }` is read by `TaskRepository.list_tasks` from a local JSON file (`tasks`, many).

## Validation

`task-id-required`: deletion rejects empty identifiers with user message `Select a task before deleting`.

## Scope limitation

This contract-only example does not supply a full tree of widgets, a finite-state machine, or any guarantees about actual terminal rendering technology. See `EVIDENCE.md` and `evidence.json` for backing lines.
