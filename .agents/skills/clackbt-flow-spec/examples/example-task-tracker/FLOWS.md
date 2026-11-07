# Example task tracker — interaction flows

## Primary scenarios

1. **Inspect task**: `list.ready` → (`task.open` / guard `valid-task-id` / command `task.load`) → `detail.loading` → (`task.loaded`) → `detail.ready` → (`nav.back`) → `list.ready`.
2. **Load failure**: `detail.loading` → (`task.load.failed`) → `detail.error` → (`nav.back`) → `list.ready`.
3. **Create task**: `list.ready` → (`task.create.start`) → `create.edit` → (`form.changed`) → `create.edit` → (`task.create.submit` / guard `title-valid` / command `task.create`) → `create.saving` → (`task.created`) → `list.ready`.
4. **Invalid title**: `create.edit` → (`task.create.submit` / guard `title-empty`) → `create.invalid`; editing returns to `create.edit`, while repeated empty submit remains invalid.
5. **Save failure and retry**: `create.saving` → (`task.create.failed`) → `create.error`; edit and retry, or cancel.
6. **Exit**: `list.ready` → (`app.exit`) → terminal `exit.done`.

## Transition authority

`machine.json` is the authoritative list of all guarded transitions, state updates, commands, and terminal states. There are no implicit navigation edges. Guard `title-valid` and `title-empty` partition nonempty/empty submissions; no save command runs when the title is empty.

## Exceptional behavior

- Missing task ID prevents opening details via `valid-task-id`.
- Detail read and task create commands expose explicit failure events.
- The saving state has no cancel event; the user must observe success/failure before leaving.
- Terminal size below 60×20 is outside the accepted scope of this example, not an invented responsive layout.
