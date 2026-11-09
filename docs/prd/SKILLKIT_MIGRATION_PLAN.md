# Skillkit migration plan

Status: implemented, pending review and release. Product decisions were approved on 2026-11-09.

Nosey stops delegating installation to the external `skills` CLI and owns the whole skill lifecycle through `internal/skillkit`, an in-tree fork of [ka2n/skills-go](https://github.com/ka2n/skills-go) at commit `56d1a36`.

## Goals

- Install, list, check, update and uninstall skills with no Node.js, npm or `skills` CLI at runtime. Git remains a prerequisite.
- Keep existing projects working, including `skills-lock.json` interoperability with `npx skills`.
- Replace CLI argument building and output parsing with typed, per-skill and per-agent results.
- Fix the safety and correctness gaps in skills-go at the source.

## Non-goals

- Global (user-scoped) installs. All installs remain project-scoped.
- Installing sources outside a registry through Nosey. Well-known endpoints and direct URLs are not added.
- Rollback of a partially failed operation.
- A fetch cache. Each run uses temporary checkouts.
- A separate fork repository or a `skills-go` module dependency.

## Decisions

| Topic | Decision |
|---|---|
| Ownership | Nosey owns installation. A new decision, D9, supersedes D8 (thin installer delegation). |
| Library | In-tree fork at `internal/skillkit`, trimmed to what Nosey uses. |
| `--print` | Removed, along with `installation.defaultMode` and the TUI copy action. |
| Private repos | Only `git fetch` sees the user's git configuration, for credentials. `init` and `checkout` run with nulled configuration. Hooks, `ext`, submodules, LFS smudge and terminal prompts stay disabled. |
| Partial failure | Stop at the first failed source, keep completed work, and report exactly what did and did not install. |
| Symlinks | Skip and warn on links that escape the skill or are broken. Follow internal links. |
| Lock file | Full interoperability with `npx skills`, including `computedHash` ordering. |
| Retired settings | Migrate to settings v3, removing `defaultMode`, `cliVersion` and `upstreamTelemetry`. |
| Agents | All 79 agents from `skills@1.7.6`, plus custom agents. The table is maintained by hand. |
| Custom agents | User settings only, with workspace-contained relative directories. |
| Lifecycle | Add `nosey update` and `nosey uninstall`, in both CLI and TUI. |
| `update` target | Follow the catalog: current pin for pinned skills, latest of the ref otherwise. |
| `uninstall` scope | Skills tracked in `skills-lock.json`. Block when an installed skill requires the target, unless `--ignore-requirements` is passed. |
| Fetching | One temporary checkout per source and ref, removed after use. |

## Why a fork

These results come from running skills-go at `56d1a36` against real `skills@1.7.6` in disposable projects on 2026-11-09.

| Area | skills-go behaviour | Consequence if used unchanged |
|---|---|---|
| Pinned SHAs | `provider/git` runs `clone --branch <sha>` and fails with "Remote branch not found". `provider/go-git` only tries branches and tags. | Official catalog pins cannot install. |
| Lock fields | Reading and writing a lock drops `ref`, `sourceUrl`, `skillPath` and `version`. | `installedStatus` marks every pinned skill `outdated`. |
| Escaping symlinks | The target's contents are copied into the skill. A fixture secret was copied; the real CLI skipped it. | Untrusted repositories can pull local files into a project. |
| Directory symlinks | The install fails with "is a directory". | Valid skills cannot install. |
| Hash | Files are sorted bytewise; upstream uses `localeCompare`. Hashes differ for a plain `SKILL.md` plus `scripts/` skill. | False updates on existing locks. |
| Agent table | 44 agents against 79. 36 are missing, `kimi-cli` is named differently, and `droid`, `kilo` and `pi` use different directories. | Rejected agent IDs or wrong install paths. |
| Inventory | A Codex-only install lists no agents. | Status becomes `unknown`. |
| Missing skills | `sk.Install` returns a nil error and no results. | Silent failure. |
| Update check | `CheckProjectUpdates` skips fetch errors and ignores refs. | A failed check can read as "no updates". |
| Local lock source | Writes absolute paths; upstream writes relative ones. | Machine-specific paths in a committed file. |
| Project activity | No releases or tags, 0 stars, 13 commits, last pushed 2026-03-31. | No upstream to rely on for fixes. |

## Target design

### `internal/skillkit`

| Source file | Treatment |
|---|---|
| `skill.go` | Keep the `SKILL.md` parser. Replace `goccy/go-yaml` with `gopkg.in/yaml.v3`. |
| `discover.go` | Keep the `fs.FS` path only. Sort search directories. Report parse errors and treat duplicate names as errors. |
| `plugin.go` | Keep plugin manifest discovery only. Use `encoding/json`. |
| `installer.go` | Keep project install and listing. Replace copy, symlink handling and uninstall as described below. |
| `hash.go` | Keep the shape and replace ordering and file selection. |
| `lock.go` | Project lock only, rewritten for full field preservation. |
| `agent.go` | Replace the table and constants with the 79-entry table plus custom agents. |
| `sk/`, `provider/*`, `cmd/skills`, `source.go`, `provider.go`, global lock, `Skill.Files` | Not carried over. |

Indicative API (final names may change):

```go
type Source struct { URI, Ref, LocalPath string }
type Checkout struct { Dir, Commit string; Close func() error }

func NewAgentTable(home string, custom []CustomAgent) (AgentTable, error)
func (f Fetcher) Fetch(ctx context.Context, src Source) (Checkout, error)
func Discover(root string, opts DiscoverOptions) ([]Skill, error)
func Install(ctx context.Context, skill Skill, agents []string, target Target) (InstallResult, error)
func Uninstall(ctx context.Context, name string, target Target) (UninstallResult, error)
func ListInstalled(target Target) ([]InstalledSkill, error)
func ReadLock(workspace string) (ProjectLock, error)
func (l ProjectLock) Write(workspace string) error
func FolderHash(dir string) (string, error)
```

Required behaviour:

- **Fetch:** resolve `owner/repo`, HTTPS and SSH URIs, validate them with `gitpolicy.Validate`, then run a hardened `init`, a credentialed `fetch --depth=1 origin <ref>` and a hardened `checkout --detach FETCH_HEAD`. For a full-SHA ref, fail unless `HEAD` equals it, with no fallback to another revision. Record the resolved commit separately from the requested ref, and keep only the requested ref in the lock `ref` field. Return a typed error, and always remove the temporary directory.
- **Git environment:** add a `gitpolicy` helper used only for the credentialed `fetch`. It keeps the user's global git configuration, sets `GIT_TERMINAL_PROMPT=0`, `GIT_ALLOW_PROTOCOL=https:ssh` and `GIT_LFS_SKIP_SMUDGE=1`, and uses `HardenedArgs` to disable hooks, `ext` and submodule recursion. Every other git command uses `HardenedEnv` and `HardenedArgs`, so a repository's `.gitattributes` cannot select a filter driver from the user's configuration.
- **Copy:** port the rules from `copyDirectory` in `skills@1.7.6`. Skip broken links and links that resolve outside the skill root, with a warning on the result. Follow internal file and directory links. Guard against directory cycles. Keep atomic replacement of the destination.
- **Hash:** match upstream `computeSkillFolderHash`: regular files only, excluding `.git` and `node_modules`, ordered with `golang.org/x/text/collate`, hashing relative path then content. The contract is equality with `skills@1.7.6` output, not the collation library. Update checks compare the fetched and installed trees using this same function, so a collation gap cannot report a false update. A locally edited skill therefore shows as outdated.
- **Lock:** preserve `source`, `sourceUrl`, `ref`, `sourceType`, `skillPath`, `computedHash`, `version` and unknown fields. Write relative local sources. Derive `source` and `sourceType` as upstream does. Write atomically through `storage`, while holding the operation lock. Move the strict validation and `LockError` from `installation/inventory.go`.
- **Destination safety:** perform all workspace writes and removals through `os.Root`, for built-in and custom agents. A symlinked ancestor that leaves the workspace is refused with a policy error.
- **Agents:** generate the table once from `skills@1.7.6`, including skill directories and detection. Pass the same table to discovery, install, listing and uninstall.
- **Inventory:** attribute agent-specific directories directly. Attribute `.agents/skills` to universal agents detected on the host, as upstream does, and to custom agents using that directory.
- **Install:** take `context.Context`, check it between skills and agents, sanitize names, and return per-skill, per-agent results and warnings. A requested skill absent from the source is an error.
- **Uninstall:** validate names read from the lock, keep every path inside the workspace, remove symlinks without following them, remove the canonical directory after agent directories, and return every error.

### `internal/installation`

`Service` stays the entry point for the CLI and TUI, so Cobra stays thin. `AddCommand` becomes `SourcePlan`:

```go
type SourcePlan struct {
    Source, Ref string
    Skills      []AddSkill
    Agents      []string
    Method      string
}
```

`Build` keeps dependency ordering, conflict checks and duplicate-name checks. `Prepare` becomes project preflight plus a Git check and no longer returns a CLI. `RunSource` calls skillkit.

### Failure semantics

- Sources run in dependency order and stop at the first failed source. The error states how many later sources did not start.
- Running a source returns structured results even when it also returns an error. Each agent outcome is `installed`, `failed`, `skipped` or `not-started`.
- The lock is written after every attempted source, including failure and cancellation. A skill that succeeded for at least one agent is recorded.
- If installation succeeds but writing the lock fails, the operation is reported as incomplete and exits 5.
- Cancellation stops between skills and agents, keeps completed work, and reports the remainder as not started.
- Exit codes are unchanged: 2 for policy and validation, 5 for install failure or partial success, 6 for a missing or incompatible prerequisite.

### Operation lock

- Install, update and uninstall each hold one operation lock from final preflight and inventory through lock persistence and final reconciliation.
- The lock is a kernel advisory lock (`flock`), so the kernel releases it if the process crashes. Non-Unix builds need an equivalent or a build-tagged fallback.
- It lives in Nosey's data directory, keyed by the canonical workspace path, because a project must not gain a `.nosey` directory from an install.
- Acquisition is bounded and cancellable. A busy lock reports that another Nosey operation is running.
- The TUI holds it through an operation handle that spans its prepare and per-source steps.

### Settings v3

```json
{
  "schemaVersion": 3,
  "installation": {
    "agents": ["claude-code"],
    "method": "symlink",
    "verbose": false,
    "customAgents": [
      { "id": "my-agent", "name": "My Agent", "skillsDir": ".my-agent/skills" }
    ]
  }
}
```

- Follow the existing migration path: upgrade in memory on load, and persist on the next `Store.Set`.
- Custom agent IDs must be safe, unique and distinct from built-in IDs. `skillsDir` must satisfy `filepath.IsLocal`.
- `customAgents` is rejected in project settings.
- A saved `defaultMode: print` no longer prevents execution, but `install` still confirms unless `--yes` is passed.

## Delivery

Do not cut a release between PR 1 and PR 3. Each PR must pass `go test ./...` with new success and failure tests.

### PR 1: Settings v3 and print removal

Changes:

- Add `internal/contracts/schemas/settings.v3.schema.json` and migrate v1 and v2 documents in `internal/settings/settings.go`.
- Remove `--print` and print-mode branches from `cmd/nosey/install.go`.
- Remove `printOnly`, the `c` copy action and `installCopied` from `internal/ui/install_run.go` and `internal/ui/nosey.go`, and the print state from `internal/ui/nosey_view.go`.
- Remove retired rows and forms from `nosey_rows.go`, `nosey_forms.go` and `settings_editor.go`.
- Replace `AddCommand.Text()` with a confirmation summary of source, ref, skills, agents and method.
- Remove `atotto/clipboard` if nothing else uses it.
- Interim state until PR 3: remove the version gate in `Build` and always pass `DISABLE_TELEMETRY=1` and `DO_NOT_TRACK=1` to the CLI, so telemetry cannot be enabled after its setting is gone.

Tests: v1→v3 and v2→v3 migration; retired keys removed on write; invalid and project-level `customAgents` rejected; `--print` rejected; confirmation summary; the TUI flow without print mode.

Done when: no code path can print commands, and old settings files load.

### PR 2: Add `internal/skillkit`

Changes:

- Add the package with the design above and no callers.
- Include the skills-go MIT `LICENSE` and a README recording the origin commit `56d1a362d6228a543092da8ad6e763de6414d269` and which files were copied or substantially rewritten, following `internal/clack`.
- Add the operation-lock primitive.
- Include the vercel-labs/skills MIT notice for the ported agent table, copy rules and hash ordering.
- Add `internal/skillkit/testdata` goldens captured once from real `skills@1.7.6`.

Tests:

- Pinned SHA, branch, tag and default-branch fetches; invalid URI and ref rejection; temporary directory cleanup on failure; a fetch whose `HEAD` differs from the requested SHA fails.
- A `.gitattributes` filter defined in user git configuration never runs during a fetch and checkout, while credentials configuration still reaches `fetch`.
- Operation lock exclusion, release after the holding process is killed, and busy and cancelled acquisition.
- Escaping, broken, internal-file, internal-directory and cyclic symlinks. Assert that no outside content is copied.
- Destination paths with a symlinked ancestor that leaves the workspace, for built-in and custom agents.
- Hash goldens with mixed-case, nested, accented, numeric, combining and non-BMP names.
- Lock round trip with all fields and unknown fields; merge markers; unsupported versions.
- All built-in agent paths against the 1.7.6 table; custom agents; universal attribution.
- Missing skills, duplicate names, parse errors and `ctx` cancellation.
- Uninstall with path traversal names, symlink targets left untouched, shared canonical directories and removal errors.

Done when: goldens match and every row in "Why a fork" has a regression test.

### PR 3: Cut over to the native engine

Changes:

- Move `Build`, `Prepare`, `RunSource`, `Install`, `Inventory` and `CheckUpdates` onto skillkit and `SourcePlan`.
- Replace `UpdateCheck` parsing with a native check: group lock entries by source and ref, fetch once per group, and compare hashes. Report failures through `Failures`, never as "no updates". Keep `Summary()` and `Warning()`.
- Hold the operation lock across each lifecycle operation, with a handle for the TUI's stepped flow, and reconcile inventory after failures.
- Replace agent display-name mapping and make `AgentNames()` include custom agents. Replace the `settings.BuiltinAgentIDs` registration from PR 1 with a direct lookup in the skillkit agent table.
- Remove `CLI` from `internal/ui/install_run.go`, and report progress for each source in the CLI.
- Delete `launcher.go`, the Node and `skills` prerequisite checks, the `skills check` parser, `AddCommand.Args`, `Service.Attach`, and `process.RunAttached` with its test.
- Delete `tools/skills`, `.github/workflows/sync-skills-pin.yml` and the `/tools/skills` Dependabot entry.
- In `ci.yml`, replace "Test both upstream CLI backends" with a native integration step. Keep Node setup because `internal/studio/testjs` still uses it. Update `ci-scope.sh` and its test.
- Update Cobra text in `install.go`, `list.go` and `doctor.go`, plus the README prerequisites and install section.
- Update the PRD: add D9, mark D8 superseded, and revise `03-cli-contract.md` (print flag, execution rules, exit 6), `07-installation.md`, AC-41, the core principle in `01-product-overview.md`, and the architecture chapters.

Tests:

- Rewrite `command_test.go`, `installation_test.go`, `updates_test.go` and `integration_test.go`, and delete `launcher_test.go`.
- Update fakes in `cmd/nosey/nosey_test.go`, `internal/app/nosey_test.go`, `internal/ui/install_run_test.go` and `internal/ui/nosey_test.go`.
- Keep an environment-gated real test using the current official pin. Keep the "workspace code is never executed" test.
- Test partial failure with structured results, lock persistence after failure and cancellation, a failed lock write, later sources not starting, concurrent lifecycle operations, crash recovery, existing 1.7.6 locks, and private-repo credentials through a gated manual check.

Before merge, run a one-off comparison against real `skills@1.7.6` for trees, locks and inventory, including discovery depth and internal skills.

Done when: a clean machine without Node can run `install`, `list --check-updates` and `doctor`, and `npx skills list` reads a Nosey-written project.

### PR 4: `nosey update` and `nosey uninstall`

Shared service methods live in `internal/installation`; Cobra files only parse flags and render results.

`nosey update [SKILL_ID...] [--ignore-conflicts] [--yes]`:

- With no IDs, select every outdated catalog-managed skill.
- Build the target from the catalog, so requirements and conflicts still apply.
- Use the actual filesystem inventory per agent. Refresh each install location found on disk and preserve its symlink or copy mode, including mixed installs, so an undetected universal agent is not lost.
- Install a newly required skill to the union of the locations of all skills that require it.
- Keep the reason for each update (catalog pin changed or upstream content changed) in the update output. `list --json` status values do not change.
- Report skills outside the catalog as skipped. Never update them.
- Failed update checks stop the command and report the failed source.

`nosey uninstall NAME_OR_ID... [--ignore-requirements] [--yes]`:

- Accept a tracked skill name or a catalog ID. Untracked or unknown targets exit 2.
- Remove the skill across all agents and remove its lock entry.
- Evaluate the selection as a whole. Compute dependents from installed skills that remain after the operation, using catalog `requires` and mapping requirements, and block unless `--ignore-requirements` is passed. Remove dependents before the skills they require.
- Confirm once. Non-interactive use requires `--yes`.
- Stop at the first failure and report what was removed.

TUI:

- Keep `u` for checking updates. Add an apply-update action on outdated skills and a remove action on tracked skills, using keys that do not collide with existing bindings.
- Reuse the install confirmation, cancellation, progress and error handling, and refresh the inventory afterwards.

Neither command gets JSON output, matching `install`. Update the README and `03-cli-contract.md` for both commands, including `--help` examples.

Tests: all selection, requirement, conflict and confirmation paths; skipped non-catalog skills; non-interactive refusal; preserved install mode; dependent blocking and override; partial failure; TUI keys and refresh; exit codes.

Done when: every lifecycle path works in both CLI and TUI, and all failure paths exit with the documented codes.

## Release blockers

- Untrusted Git content cannot trigger Git checkout filters or hooks.
- Concurrent lifecycle operations cannot corrupt inventory or lock state.
- Partial source failures persist all confirmed successful installs.
- A process crash does not leave the project permanently locked.
- Legacy locks round-trip without losing provenance or unknown fields.
- Custom agent directories cannot escape the workspace.
- A multi-target uninstall preserves everything outside the requested selection.
- Native results distinguish failed, skipped and not-started agents.

## Risks

| Risk | Mitigation |
|---|---|
| `localeCompare` depends on ICU and the system locale. | Use a fixed collation matching the goldens, with extra mixed-case and non-ASCII fixtures, and compare like-for-like hashes for updates. Document any remaining difference. |
| Keeping user git configuration widens what git reads. | Expose it only to `fetch`, override hooks and transports on the command line, disable prompts, and restrict protocols to HTTPS and SSH. |
| Upstream discovery or lock details differ from the fork. | Goldens plus the one-off comparison before PR 3 merges. |
| The agent table goes stale. | Custom agents cover urgent gaps. Update the table by hand. |
| Uninstall deletes the wrong path. | Workspace containment, name validation, no symlink following, and adversarial tests. |
| Files written by a future `npx skills` version change format. | Preserve unknown fields and keep version validation strict. |
| Replacing a skill from an update removes local edits. | Show the replacement in the confirmation, matching today's install behaviour. |

## Attribution

Preserve the MIT notices for [ka2n/skills-go](https://github.com/ka2n/skills-go) and [vercel-labs/skills](https://github.com/vercel-labs/skills) in `internal/skillkit/`.
