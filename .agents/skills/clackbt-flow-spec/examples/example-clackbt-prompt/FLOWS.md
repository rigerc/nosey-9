# Flow

`name.ready` → `confirm.ready` → (`greet.running` → `greet.done` or `greet.failed`) / `greet.declined`.

Name validation failure → `name.invalid` → retry. Ctrl+C at any active prompt → `greet.cancelled` (proposed exit policy). Native model steps share a single Bubble Tea event loop.
