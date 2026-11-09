# skillkit

`skillkit` is Nosey's engine for installing, listing and removing agent skills in a project.
It is an in-tree fork of [ka2n/skills-go](https://github.com/ka2n/skills-go), trimmed to what
Nosey uses and corrected to behave like [vercel-labs/skills](https://github.com/vercel-labs/skills)
`skills@1.7.6` on disk. The design and migration are described in
`docs/prd/SKILLKIT_MIGRATION_PLAN.md`.

## Origin

- Forked from skills-go commit `56d1a362d6228a543092da8ad6e763de6414d269`.
- skills-go is MIT licensed: see [`LICENSE`](LICENSE).
- The agent table, `copyDirectory` rules, `computeSkillFolderHash` ordering and the
  `projectLockEntry` / `getProjectLockSource` logic are ported from `skills@1.7.6`
  (`dist/cli.mjs`), also MIT licensed: see [`NOTICE-vercel-labs-skills`](NOTICE-vercel-labs-skills).

## Provenance of each file

"Copied" means the skills-go file was the starting point and only mechanical changes were made
(package name, `encoding/json`, `gopkg.in/yaml.v3`). "Rewritten" means the file was replaced; the
skills-go file is named for its role only.

| File | skills-go origin | Treatment |
|---|---|---|
| `skill.go` | `skill.go` | Parser kept in spirit (frontmatter split, name and description required); YAML via `gopkg.in/yaml.v3`; `Files`, `Marshal`, provider fields dropped; `SanitizeName` ported from `skills@1.7.6`. Substantially rewritten. |
| `plugin.go` | `plugin.go` | Copied (`fs.FS` variants only, `encoding/json`). One fix: containment against a `.` base. |
| `discover.go` | `discover.go` | `fs.FS` path kept. Rewritten: sorted search directories, parse errors and duplicate names are errors, no environment reads, `os.Root` reads. |
| `hash.go` | `hash.go` | Shape kept (SHA-256 over relative path then content). Rewritten: regular files only, `x/text/collate` ordering to match `localeCompare`. |
| `lock.go` | `lock.go` | Project lock only. Rewritten: every field and unknown field preserved, key order preserved, strict validation and `LockError` moved from `internal/installation/inventory.go`, relative local sources, atomic write via `internal/storage`. |
| `agents.go`, `agents_table.go` | `agent.go` | Replaced by the 79-entry `skills@1.7.6` table plus custom agents. |
| `install.go`, `installset.go`, `copy.go`, `dest.go`, `uninstall.go`, `list.go` | `installer.go` | Rewritten: `os.Root` confinement, `skills@1.7.6` copy rules, structured outcomes, context handling, validated uninstall. |
| `fetch.go`, `source.go` | `provider/git`, `source.go` | Not copied. New: hardened `init`, credentialed shallow `fetch`, hardened `checkout`, exact SHA verification, source classification for lock entries. |
| `oplock*.go` | - | New: kernel advisory workspace lock. |

Not carried over: `sk/`, `provider/*`, `cmd/skills`, the global lock and `Skill.Files`.

## Behaviour that differs from `skills@1.7.6`

- Eve subagent skill directories and Eve front-matter stripping are not implemented.
- Discovery does not read `INSTALL_INTERNAL_SKILLS`; use `DiscoverOptions.IncludeInternal`.
- Custom agents always get their directory created; built-in agents whose project directory is
  missing are skipped in symlink mode unless `Target.CreateMissingAgentRoot` is set (as upstream).
- `FolderHash` uses `golang.org/x/text/collate`. It matches every golden except CJK Extension B
  ideographs, which ICU orders by radical-stroke and `x/text` after the core block. See
  `testdata/hash/cases.json` (`han-extension-b`, marked `knownMismatch`). Canonically equivalent
  file names (precomposed and decomposed) tie in `localeCompare`; they break bytewise here.

## Goldens

Everything under `testdata/` comes from the real `skills@1.7.6` CLI, run once with a temporary
`HOME`, `DISABLE_TELEMETRY=1` and `DO_NOT_TRACK=1`, and contains no machine-specific paths. Normal
`go test` needs neither Node nor the network.

| File | Captured from |
|---|---|
| `testdata/hash/cases.json` | `computedHash` written by `skills add <dir> -a claude-code -y` for each fixture tree (also identical under `LANG=C`). |
| `testdata/copy/cases.json` | The tree `skills add <repo>/skill --copy -a claude-code -y` installed for each symlink fixture. |
| `testdata/lock/skills@1.7.6.json` | A `skills-lock.json` with a git entry and a relative local entry; the temporary repository URL was replaced by `https://git.example.test/team/skills.git`. |
| `testdata/agents.1.7.6.tsv` | The `agents` table extracted from `dist/cli.mjs`. |

## Regression tests for "Why a fork"

| skills-go problem | Test |
|---|---|
| Pinned SHAs fail | `TestFetchPinnedSHAIsExact`, `TestFetchDefaultBranchBranchTagAndPinnedSHA`, `TestFetchFailsWhenHeadDiffersFromRequestedSHA` |
| Lock fields dropped | `TestLockPreservesEveryFieldAndUnknownFields`, `TestLockRoundTripsSkills176Output` |
| Escaping symlinks copied | `TestCopyMatchesSkills176/escaping-*`, `TestCopyWarnsOnSkippedLinks` |
| Directory symlinks fail | `TestCopyMatchesSkills176/internal-dir-link` |
| Hash ordering | `TestFolderHashMatchesSkills176` |
| Agent table | `TestAgentTableMatches176` |
| Codex-only inventory empty | `TestListCodexOnlyInstallListsCodex` |
| Missing skills silent | `TestInstallSkillsRequestedSkillMissingIsAnError` |
| Update check hides fetch errors, ignores refs | `TestFetchCleansUpOnEveryFailure` (typed `FetchError`), `TestFetchGitInvocation` (ref honoured); the update check itself arrives with PR 3 |
| Local lock source absolute | `TestLockLocalSourceIsRelative` |

## Testing notes

Git-backed tests build local repositories. The `file` transport is enabled through an unexported
`Fetcher` field only the tests can set, so production code is limited to `https` and `ssh`.
`TestOperationLockReleasedWhenHolderIsKilled` re-executes the test binary and sends it `SIGKILL`.
