#!/usr/bin/env bash
set -euo pipefail
scope_script="$(cd "$(dirname "$0")" && pwd)/ci-scope.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export RUNNER_TEMP="$fixture" GITHUB_OUTPUT="$fixture/output"
export GITHUB_EVENT_NAME=pull_request
cd "$fixture"
git init -q
git -c user.name=Test -c user.email=test@example.com commit --allow-empty -qm base
BASE_SHA="$(git rev-parse HEAD)"
export BASE_SHA HEAD_SHA
check() {
  local path="$1" expected="$2"
  git read-tree --empty
  local blob
  blob="$(printf 'fixture\n' | git hash-object -w --stdin)"
  git update-index --add --cacheinfo "100644,$blob,$path"
  HEAD_SHA="$(git -c user.name=Test -c user.email=test@example.com commit-tree "$(git write-tree)" -p "$BASE_SHA" -m change)"
  : > "$GITHUB_OUTPUT"
  bash "$scope_script"
  local actual
  actual="$(sed -n 's/=true$//p' "$GITHUB_OUTPUT" | paste -sd ',' -)"
  [[ "$actual" == "$expected" ]] || { echo "$path: expected $expected, got $actual"; exit 1; }
}
check docs/guide.md ''
check site/src/app.js ''
check internal/ui/view.go go
check internal/installation/launcher.go go,upstream
check internal/detection/detect.go go,upstream
check registries/default go
check go.mod go,upstream,dependencies,go_dependencies
check tools/skills/package-lock.json go,upstream,dependencies
check .github/workflows/ci.yml go,upstream,dependencies,workflows
check .github/scripts/ci-scope.sh go,upstream,workflows
check .golangci.yml go
check .gitmodules go
# A large diff must not be misclassified by grep closing a pipe early.
blob="$(printf 'fixture\n' | git hash-object -w --stdin)"
for ((i=0; i<3000; i++)); do printf '100644 %s\tinternal/ui/%s.go\n' "$blob" "$i"; done | git update-index --index-info
HEAD_SHA="$(git -c user.name=Test -c user.email=test@example.com commit-tree "$(git write-tree)" -p "$BASE_SHA" -m large)"
: > "$GITHUB_OUTPUT"
bash "$scope_script"
grep -qx 'go=true' "$GITHUB_OUTPUT"
BASE_SHA=invalid
: > "$GITHUB_OUTPUT"
if bash "$scope_script" 2>/dev/null; then echo 'Invalid diff unexpectedly succeeded'; exit 1; fi
[[ ! -s "$GITHUB_OUTPUT" ]]
for GITHUB_EVENT_NAME in push workflow_dispatch schedule; do
  : > "$GITHUB_OUTPUT"
  bash "$scope_script"
  grep -qx 'workflows=true' "$GITHUB_OUTPUT"
  grep -qx 'go_dependencies=true' "$GITHUB_OUTPUT"
  if [[ "$GITHUB_EVENT_NAME" == schedule ]]; then
    grep -qx 'go=false' "$GITHUB_OUTPUT"
    grep -qx 'upstream=false' "$GITHUB_OUTPUT"
  else
    grep -qx 'go=true' "$GITHUB_OUTPUT"
    grep -qx 'upstream=true' "$GITHUB_OUTPUT"
  fi
done
echo 'CI scope tests passed'
