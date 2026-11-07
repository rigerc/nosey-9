# Example inline deployment CLI — specification

This is an **illustrative, not executable** inline CLI. Invocation: `shipctl deploy --env staging`. It is rendered in the regular terminal scrollback, not the alternate screen buffer. There is no fixed-height frame, status bar, or persistent help footer. The command line shown in the snapshots is an illustrative echo; an actual application may not reprint the shell's prompt.

## Inputs and terminal contract

- Required option: `--env staging`; the example only documents staging. Unknown/missing environment handling is outside this small sample.
- Confirmation is attached to **`/dev/tty`/TTY stdin**, single response `y` or `n` (`n` is the conservative default). This flow is **not pipe-safe** as designed; a production spec must define a `--yes` flag or a noninteractive failure behavior.
- While running, a spinner and percentage progress are **transient** and would normally rewrite one terminal line with carriage returns when stderr is a TTY; neither should be emitted as an unbounded stream. Mockups show one frame at 37% and do not emulate cursor movement.
- On success, write a human-readable deployment ID and URL to **stdout** and exit `0`. On error, write diagnostics to **stderr** and exit `1`. On an explicit decline, write `Cancelled` to **stderr** and exit `130`. No stdout output is guaranteed for failures. The exact remote URL is sample content.
- `TERM=dumb`, color-disabled, and non-TTY progress behavior should disable animations and ANSI control codes. This sample specifies snapshots rather than a working terminal runtime.

## Accessibility, width, and acceptance

The transcript flows to arbitrary height and must preserve log order. Layout adapts at 35+ columns; the `min_size.rows` field is documentary only for inline flows. Output should wrap without deleting earlier lines. Each terminal state has an explicit exit code and no outgoing transition. Prompts accept keyboard input without requiring fullscreen focus navigation. Generated semantic previews can never validate a real subprocess's stdout/stderr bytes or terminal escape behavior.
