# Settings

[← Specification index](../README.md) · [Previous](07-installation.md) · [Next](09-architecture-foundation.md)

## 11. Settings design

### Storage

- User: `$XDG_CONFIG_HOME/nosey/config.json` (fallback to platform-appropriate config directory).
- Optional workspace: `<workspace>/.nosey/config.json`, values override user preferences for the current workspace.
- Cache: `$XDG_CACHE_HOME/nosey/` (or platform equivalent); do not check it into a workspace.
- Untrusted project registries are inert until approved with `nosey trust` or the equivalent Settings → Registries trust review; both write to the same user-owned trust store. A changed canonical project-registry fingerprint automatically revokes that approval. Only user-level config can activate detector adapters. Registry content is read-only from the TUI; edits to repository content happen in the user's editor/Git workflow.
- Persist config by writing a temporary validated file then atomic rename. Show unsaved changes and confirmation before destructive removal.

`config get/set` is implemented in M1 with typed JSON values, dotted allowed keys, atomic chosen-layer writes and project adapter/trust restrictions; see [D4](17-approved-decisions.md#d4--complete-cli-machine-contracts). Installation source payloads use the separate persistent application-data store in [D2](17-approved-decisions.md#d2--persistent-installation-sources), not the refresh cache.

### Fields

| Settings section | Fields | Defaults |
|---|---|---|
| Registries | ID, Git URL/local path, enabled, priority, pinned ref | Built-in default registry at priority 0 |
| Refresh | TTL hours, refresh now, cache status | 24 hours globally, per-registry override (`ttlHours`) |
| Installation | Agent IDs, method, default mode, verbose | Agent detection used only for confirmation defaults; symlink, execute, off; pinned CLI version and upstream telemetry inherit |
| Detection | File exclusions, max content bytes, MCP package inspection | Standard generated/vendor excludes, 1 MiB, MCP inspection off (explicit opt-in) |

Validate when adding/editing a registry; indicate offline/pinned/incompatible sources. Changing registry order/priority triggers a merge and Scan/Explore refresh, without automatically installing anything. Settings → Registries MUST list workspace registries pending approval with their canonical workspace root, origin URI/path, registry IDs and full canonical fingerprint; provide explicit **Approve** and **Revoke approval** actions. There is no implicit trust on first use, and no project-config flag may approve its own registry. `registry add/remove` are user-scoped; project-scoped registries are edited through the project settings file or Settings workspace controls with an approval step.

### Settings merge (normative)

User config is the base and workspace config overrides it. Scalar leaves replace, `registries` merge by stable ID, and `installation.agents` / `detection.exclude` arrays replace atomically. `detection.enableMcpPackage` is an ordinary boolean scalar, defaulting to false; temporary `--enable-mcp` applies only to that invocation. Local `source.path` in workspace settings is resolved from the workspace root; a user config local registry path must be absolute or `~/`-prefixed. The default priority for a workspace-local registry is 30. A registry-specific `ttlHours` overrides `updates.ttlHours`, otherwise default TTL is 24h. Official registry starts at priority 0 and is first for equal-priority comparison. Project settings may not activate adapters. Trust-state fingerprints are maintained in the user-controlled trust store.
