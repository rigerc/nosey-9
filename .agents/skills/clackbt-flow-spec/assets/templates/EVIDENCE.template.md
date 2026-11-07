# Existing-application evidence — {{FEATURE_NAME}}

## Source context

- Repository root: {{REPO_ROOT}} (referenced paths are relative to root)
- Source revision / branch: {{REVISION}}
- Inspected application/feature boundary: {{BOUNDARY}}
- Inspection approach: read-only static analysis; tests/runtime only with user authorization

## Inspected surfaces and paths

| Surface | File:lines | Code symbol | Notes |
| --- | --- | --- | --- |
| {{SCREEN_OR_SERVICE}} | `{{PATH}}:{{LINE_START}}-{{LINE_END}}` | `{{SYMBOL}}` | {{NOTES}} |

## Contract provenance

Summarize `evidence.json` claims. Explicitly distinguish observed, user-confirmed, inferred, and proposed items.

## Unresolved gaps and scope limits

List unverified dynamic/plugin branches, missing implementations, and answers needed. If none, state none. Describe uninspected surfaces explicitly.

## Drift and verification

Document which source files were hashed; validator will reject stale supplied hashes. Validation is not runtime proof.
