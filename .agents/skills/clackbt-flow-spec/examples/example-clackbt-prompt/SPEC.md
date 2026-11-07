# Clackbt greeting workflow — illustrative brief

- **Command:** `go-cli-template prompt`, interactive TTY only; inline, not alternate-screen.
- **Runtime:** Go, Cobra, native Bubble Tea v2 via `internal/clack/prompts`, one `RunNative` parent.
- **Step 1:** required `TextModel`, exported result field `Name string`.
- **Step 2:** `ConfirmModel`, exported result field `Confirm bool`; `false` is a business decision, not `ErrCancelPrompt`.
- **No:** prints `Cancelled.` and returns normal success per example command.
- **Yes:** prints `Hello, <Name>!` to stdout.
- **Ctrl+C:** illustrative status 130; confirm actual Cobra process handling before shipping.
- **Non-TTY:** do not launch prompts; return an explanatory error.
- **No external effects** beyond stdout/stderr. Illustrative model state snapshots only.
