# clackbt: validate and build prompt-flow artifacts

Read `SKILL.md` and `references/clackbt-workflow.md`. Once the user-approved spec is authored and reviewed, run:

```bash
node <skill-root>/scripts/clackbt.mjs build --repo-root <repo-root> --feature <slug> --strict
```

Correct canonical contract and clackbt backend binding errors. Inspect the offline `contracts-viewer.html`, `storyboard.html`, `mockups/index.json`, `clackbt-preview.html`, and `clackbt-transcript.txt` if present. Check secret redaction, native `RunNative` only, ModelStep→exported field correctness, TTY and stdout/stderr, cancel versus No, and terminal exit codes. Label visuals *illustrative*, separate Go/runtime testing from JSON spec validation, and report commands actually run.
