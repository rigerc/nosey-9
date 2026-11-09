#!/usr/bin/env bash
# Shared step selection for the two CI jobs. Diff errors must fail closed.
set -euo pipefail

if [[ "$GITHUB_EVENT_NAME" != pull_request ]]; then
  go=true
  upstream=true
  if [[ "$GITHUB_EVENT_NAME" == schedule ]]; then
    go=false
    upstream=false
  fi
  printf 'go=%s\nupstream=%s\ndependencies=false\ngo_dependencies=true\nworkflows=true\n' "$go" "$upstream" >> "$GITHUB_OUTPUT"
  exit 0
fi

git diff --name-only "$BASE_SHA...$HEAD_SHA" > "$RUNNER_TEMP/changed-files"
scope() {
  if grep -Eq "$2" "$RUNNER_TEMP/changed-files"; then
    echo "$1=true" >> "$GITHUB_OUTPUT"
  else
    echo "$1=false" >> "$GITHUB_OUTPUT"
  fi
}
build='^go\.(mod|sum)$|^Makefile$|^\.github/workflows/(ci|release)\.yml$|^\.github/scripts/'
scope go "^(cmd|internal|tests|third_party|registries)/|^tools/skills/|^\.gitmodules$|^\.(golangci|custom-gcl)\.yml$|$build"
scope upstream "^internal/(installation|process|settings|storage|contracts|domain|catalog|detection|registry|registrygen|gitpolicy)/|^tools/skills/|^third_party/|$build"
scope dependencies '^go\.(mod|sum)$|(^|/)(package(-lock)?\.json|npm-shrinkwrap\.json)$|^\.github/workflows/'
scope go_dependencies '^go\.(mod|sum)$'
scope workflows '^\.github/|(^|/)action\.ya?ml$'
