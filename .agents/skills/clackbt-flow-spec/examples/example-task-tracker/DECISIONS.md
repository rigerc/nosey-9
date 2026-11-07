# Example task tracker — questions and decisions

This is a **synthetic demonstration**, not a record of questions answered by the current user.

| ID | Clarification | Example decision | Source | Impact |
| --- | --- | --- | --- | --- |
| Q-1 | Entry view? | Task list | Illustrative brief | `entry_screen=task-list` |
| Q-2 | Minimum terminal size? | 60×20 | Delegated illustrative assumption | All screen `min_size` |
| Q-3 | Persistence? | Local task repository | Illustrative brief | Data sources and commands |
| Q-4 | Empty title? | Reject with inline error | Illustrative brief | `title-required`, guarded submit |
| Q-5 | Cancel while saving? | Disabled until command settles | Delegated illustrative assumption | No `nav.back` from `create.saving` |

## Assumptions

The example uses keyboard-first interaction and a minimum 60×20 terminal. These are **sample decisions for demonstration** only. In an actual project the agent asks the user about material gaps before writing.

## Unresolved questions

None for this illustrative example. Future product briefs must run the clarification gate anew.
