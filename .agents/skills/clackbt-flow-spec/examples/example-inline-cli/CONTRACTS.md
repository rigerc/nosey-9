# CLI implementation contracts

The canonical IDs are in `contracts.json`, `screens.json`, and `machine.json`. The **invocation and input mode** live on `screens[*].cli`; the **process exit code** lives on each terminal inline state.

- UI: `confirm-prompt` emits `deploy.confirm` / `deploy.cancel`; `deployment-stdout` and `deployment-stderr` distinguish output channels, not merely colors; `deploy-progress` is a transient TTY presentation.
- Events: user decisions originate from UI; `deploy.succeeded` and `deploy.failed` originate from command outcomes.
- Command: `deploy.start` is non-idempotent and publishes to staging; do not retry it automatically.
- State: `deploy_progress` 0–100; `error_text` is human-readable.
- Output: document shell completion and stderr behavior separately; exit codes `0`, `1`, `130` are process statuses and must **not** be printed as if application output.
