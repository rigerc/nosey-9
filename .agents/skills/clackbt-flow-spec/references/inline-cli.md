> clackbt specialization: an interactive prompt uses `clackbt.json`, the bundled Go model catalog, native `RunNative` composition and a TTY gate. Use this file for generic inline stream/exit contracts only.

# Inline CLI contracts and render semantics

Inline CLI means **no alternate screen / fullscreen ownership**. A command writes output into the caller’s existing terminal scrollback and may ask short interactive questions on a TTY. This is a different interaction model from a fullscreen TUI with persistent navigation, panels, and help footer. Fullscreen and inline surfaces may coexist in the same state machine.

## Initial plan and questions

In the provisional plan identify which commands run as **batch** operations and which may pause for an **in-place prompt**. Clarify:

1. Invocation and argument grammar: executable, positional args, options, defaults, env/config precedence, invalid values, `--help`/`--version`, noninteractive/`--yes` flags, completion and predictable exit codes.
2. I/O channels: whether stdout is machine-readable (JSON/JSONL/table/plain text), which logs belong on stderr, output ordering and newline discipline, treatment of secrets and sensitive input, and pipe/redirection behavior.
3. TTY: how interaction is detected; whether stdin is a TTY, pipe, or absent; question defaults, confirmation safety, cancellation, EOF, Ctrl+C/termination, color/Unicode fallback, no-color and `TERM=dumb`.
4. Async and transient display: whether spinner/progress rewrites the same line, which stream is used, and behavior when stdout/stderr are redirected or progress output is disabled. Long lines, wrapping, terminal width, final cleanup of transient prompts.
5. Output and completion: exact success/failure/retry semantics, exit status for each terminal state, useful error diagnostics, idempotency, partial failure and no-auto-retry boundaries.

Follow the normal **provisional plan → interview → revised plan → explicit approval** workflow. Do not treat examples as approved product decisions.

## Canonical modeling

Keep `schema_version: "1.0.0"` for `screens.json`, `machine.json`, and `contracts.json`. The following fields extend the canonical schemas additively:

```json
{
  "id": "deploy-output",
  "title": "Deployment output",
  "purpose": "Stream the output without taking over the terminal",
  "presentation": "inline",
  "cli": {
    "invocation": "shipctl deploy --env staging",
    "interaction": "batch",
    "stdin": "none"
  },
  "min_size": {"cols": 40, "rows": 1},
  "regions": [
    {"id": "invocation", "kind": "command"},
    {"id": "message", "kind": "output", "stream": "stdout"},
    {"id": "diagnostic", "kind": "output", "stream": "stderr"}
  ],
  "focus_order": [],
  "keybindings": [],
  "states": ["deploy.done", "deploy.failed"]
}
```

`screen.presentation` defaults to `fullscreen` for backward compatibility. `cli` is **mandatory for inline** and disallowed for fullscreen. `interaction` is `batch` or `prompted`; prompted requires `stdin: "tty"`. `stdin` accepts `none`, `pipe`, or `tty` — it describes expected standard-input behavior, not whether stdout is a terminal. Use separate views when interaction mode changes (e.g. a confirmation prompt before a batch output stage). A command echo is sample text only; actual shells may already display the command.

Reusable inline node kinds are `command`, `output` (`stream: "stdout"|"stderr"`), `prompt`, `spinner`, and `progress`. `output.stream` defaults to stdout if omitted. `prompt` may have `focusable` and `on` event mappings, just like other interactive nodes. Do not add a persistent help footer to inline views. A `spinner` or `progress` node expresses a **single sampled state**, not an animation. Order nodes in the normal vertical scrollback order and use scenario fixtures to hide nodes that do not appear in a given state. `screens.json` is the canonical structural layout; examples must not invent control events in `scenarios.json`.

Inline terminal states require `exit_code` (integer 0–255), for example:

```json
{"id":"deploy.done","screen":"deploy-output","terminal":true,"exit_code":0,"description":"Completed successfully"}
```

Exit status is **process metadata**, not stdout or stderr text. `machine.json` owns state transitions; `contracts.json` owns events, commands, payloads, effects and validation. For output channels and argument/option contracts not represented by a fixed canonical schema field, use descriptive component props, `SPEC.md` and `CONTRACTS.md`. Do not assert real-byte-level fidelity from illustrative previews.

## Preview behavior

Inline mockups never draw a fullscreen frame/title bar or fill to a fixed terminal height. Their row count is content-derived. Width (`--cols` or configured preview viewports) controls wrapping/clipping; terminal rows are not a transcript bound. The `min_size.rows` entry is kept for schema compatibility but should normally be `1` for inline views and is not enforced as a height floor by the inline renderer. Inline nodes can use the same optional `design.preview.styles` and `scenarios` fixture overrides as fullscreen views. In particular, `components.output.text` may contain newlines and `components.progress.value` may contain percent values. State snapshots are point-in-time renderings, **not cumulative logs**; they never execute a CLI or simulate in-terminal cursor movements.

The offline contract explorer and storyboard display inline views alongside fullscreen ones. They are visual documentation, not a terminal emulator. To test actual stream separation, signals, pipes, cursor control or exit statuses, write runtime integration tests against the real executable outside this skill.

See `examples/example-inline-cli/` and the `scaffold --surface inline` option in the README.
