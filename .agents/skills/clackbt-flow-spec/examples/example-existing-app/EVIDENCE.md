# Implementation evidence — existing-app example

## Inspected source

- Single source file: `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py`
- Verification: static inspection only; file hash pinned in `evidence.json`.
- Coverage: task list output, detail text, key dispatcher, state fields, JSON repository, deletion validation.
- Limitation: no runtime tests were executed; this is an illustrative fixture, not the user's application.

## Contract-to-source index

| Contract ref | File:line | Classification |
| --- | --- | --- |
| `ui.components.task-list-view` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:43` | observed |
| `ui.components.task-delete-action` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:55` | observed |
| `ui.components.task-back-action` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:52` | observed |
| `state.fields.screen` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:38` | observed |
| `state.fields.selected_id` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:39` | observed |
| `state.fields.error_message` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:40` | observed |
| `events.task.open` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:51` | observed |
| `events.task.delete.request` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:55` | observed |
| `events.nav.back` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:54` | observed |
| `events.task.deleted` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:67` | observed |
| `events.task.delete.failed` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:64` | observed |
| `commands.task.delete` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:61` | observed |
| `data.models.Task` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:12` | observed |
| `data.sources.tasks` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:21` | observed |
| `validation.rules.task-id-required` | `.agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py:25` | observed |

## Gaps

None within the stated static fixture scope. No dynamic extensions/plugins represented.

## Drift

All observed claims have full-file SHA-256 to detect stale source citations.
