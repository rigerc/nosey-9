# Example task tracker — TUI specification

## Goal and scope

Enable a keyboard-first operator to view a locally stored task list, open a task, create a task with a nonempty title, or exit the feature. The terminal UI is framework-agnostic; this package does **not** implement the runtime.

## Screen catalog

| Screen ID | Purpose | Node layout | Minimum terminal size |
| --- | --- | --- | --- |
| `task-list` | Browse, open, create, exit | vertical container: header, list, create button, help | 60 cols × 20 rows |
| `task-detail` | Show selected task; loading/failure status | vertical container: title, details, status, Back button | 60 × 20 |
| `task-create` | Edit title, validate, save, cancel | vertical container: heading, input, error status, Save, Cancel | 60 × 20 |

## Layout and interaction

`task-list` focus: `tasks-list` → `new-task-button`; Enter opens the selected task, `n` starts creation, `q` exits. `task-detail` focus is `back-button`; Escape returns to list. `task-create` focus: `title-input` → `save-button` → `cancel-button`; Ctrl+S saves, Escape cancels unless saving is in progress. Screens use region trees in `screens.json`, not pixel layouts.

When task details load, the detail status region shows progress. A load error appears in the same region. Creation shows inline validation and persistence errors. While saving, edit controls are disabled to prevent duplicate creation.

## Acceptance criteria

- Opening a task starts `task.load`, then shows detail or error.
- Creating a task with blank/whitespace title never calls persistence and displays **Title is required.**
- A valid submit enters saving, then returns to list on success or shows retryable error on failure.
- Back/Escape returns to list from detail/creation when not saving.
- All listed keybindings and focus targets match `screens.json` and `machine.json`.
- The feature is designed for terminals of at least 60 columns × 20 rows; narrower terminals are out of this illustrative scope.

## Constraints

Keyboard-first. ANSI terminal capabilities other than plain text, focus indication, and keyboard input are not assumed. Error feedback must be visible as text, not color alone.
