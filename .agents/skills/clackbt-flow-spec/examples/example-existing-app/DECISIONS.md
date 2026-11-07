# Decisions — existing-app example

- Scope: contracts-only, existing implementation, illustrative fixture.
- Inspection: static source inspection only; no application execution.
- Clarification gate: complete for this fixed illustrative fixture; no questions required to document its code.
- `task.delete.request` is a logical event name for the D-key path, even though Python dispatch directly calls `delete_current`.
- No assumptions about external app behavior or extra UI controls.
