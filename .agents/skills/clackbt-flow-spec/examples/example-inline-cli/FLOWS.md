# Inline CLI flow

| State | Surface | Interaction | Event / next state | Output |
|---|---|---|---|---|
| `confirm.ready` | inline, prompted | `y` or `n` on TTY | `deploy.confirm` → `deploy.running`; `deploy.cancel` → `deploy.cancelled` | Command echo + in-place prompt |
| `deploy.running` | inline, batch | none | `deploy.succeeded` → `deploy.done`; `deploy.failed` → `deploy.failed` | One transient spinner + progress snapshot |
| `deploy.done` | inline, batch | terminal | exit `0` | stdout: deployment ID and URL |
| `deploy.failed` | inline, batch | terminal | exit `1` | stderr: diagnostic |
| `deploy.cancelled` | inline, batch | terminal | exit `130` | stderr: cancellation |

The different screens describe regions of a single scrollback interaction. The mockup for each state represents the output visible at that point; it does not concatenate previous machine-state previews.
