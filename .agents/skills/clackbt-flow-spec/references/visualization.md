# clackbt visualization and reproducible generation

The skill produces two complementary, **offline and non-executable** visualizations from canonical JSON:

- `contracts-viewer.html` from `scripts/generate-html.mjs` shows UI/state/event/command/data/validation contracts, screen/state links and evidence.
- `storyboard.html` and `mockups/*` from `scripts/mockup.mjs` show deterministic per-state semantic previews (TXT/ANSI/SVG), with multiple viewport/scenario fixtures. `clack-prompt`, `clack-message`, `clack-note`, `clack-spinner` are clackbt-specific render adapters, not actual Bubble Tea components.
- `clackbt-preview.html` and `clackbt-transcript.txt` from `scripts/clackbt-adapter.mjs` illustrate the ordered Clack ModelStep chain in static snapshots, with completed/active/cancelled stages. Secret inputs are never allowed in sample previews; runtime fidelity is not claimed.

Run `node scripts/clackbt.mjs build --repo-root <root> --feature <slug> --strict`. For only the Clack transcript, use `clack-preview`; for semantic state mockups, `mockup --cols 48 --outputs txt,svg,ansi,html`. `design.json` can set viewports and styling, `scenarios.json` can populate sample text, options and focus. Neither can add new events/transitions/side effects. All output is written beneath `.work/flow-spec/<slug>/`.

**Security:** no target app is executed, no dynamic imports of Go/termcn/Ink registry items, no third-party assets, no network use. Do not include passwords/tokens in scenarios or `clackbt.json` previews. HTML viewers are standalone, not authoritative source evidence, not byte-exact ANSI captures. Confirm live rendering and event semantics through authorized Go/PTY tests separately.
