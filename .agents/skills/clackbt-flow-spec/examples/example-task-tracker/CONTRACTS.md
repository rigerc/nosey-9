# Example task tracker — implementation contracts

## UI contracts

`task-list-view` binds `items: Task[]` to data source `tasks` and emits `task.open(task_id: string)`. `create-task-action` emits `task.create.start`. `detail-content` binds `task: Task` to `selected-task`. `detail-back-action` emits `nav.back`. `create-title-field` binds `value: string` to `draft_title`, emits `form.changed(title: string)`. Save and Cancel emit `task.create.submit` and `nav.back`, respectively.

## State contract

- `selected_task_id: string|null = null` — currently selected task.
- `draft_title: string = ""` — edited title.
- `error_message: string = ""` — validation or storage error.
- The detail view requires a selected task ID; inputs are disabled in `create.saving`.

## Event contract

Events are typed in `contracts.json`, including `task.open`, `task.loaded`, `task.load.failed`, `task.create.start`, `form.changed`, `task.create.submit`, `task.created`, `task.create.failed`, `nav.back`, `app.exit`.

## Commands

- `task.load(task_id: string)` is idempotent and reads the local repository. Emits `task.loaded` or `task.load.failed`.
- `task.create(title: string)` is not idempotent and persists a task. Emits `task.created` or `task.create.failed`; refreshes the task list on success.

## Data and validation

Models `Task(id, title, completed)` and `TaskDraft(title)` are in `contracts.json`. `tasks` and `selected-task` are local-repository data sources. Rule `title-required` checks `trim(title) != ""` upon `task.create.submit` and displays **Title is required.**

All machine-readable signatures, types, and identifiers are authoritative in `contracts.json`.
