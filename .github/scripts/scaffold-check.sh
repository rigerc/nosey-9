#!/usr/bin/env bash
# A registry made by this CLI must pass the generator its CI workflow pins, and
# that generator must have every command the workflow calls. Set
# PINNED_NOSEY_BIN to test a local build instead of installing the pin.
set -euo pipefail

: "${NOSEY_BIN:?NOSEY_BIN must point at a nosey binary}"
template=registries/default/templates/registry/empty/files/ci/github/registry.yml
ref="$(sed -n 's/^  GENERATOR_REF: *//p' "$template")"
[[ "$ref" =~ ^[0-9a-f]{40}$ ]] || { echo "scaffold workflow has no GENERATOR_REF commit" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
official="$(realpath registries/default)"
export XDG_CONFIG_HOME="$work/config" XDG_CACHE_HOME="$work/cache" XDG_DATA_HOME="$work/data" XDG_STATE_HOME="$work/state"
cat > "$work/settings.json" <<JSON
{"schemaVersion":2,"registries":[{"id":"nosey:official","source":{"kind":"local","path":"$official"},"enabled":true,"priority":0}]}
JSON

pinned="${PINNED_NOSEY_BIN:-}"
if [[ -z "$pinned" ]]; then
  GOBIN="$work/bin" go install "github.com/rigerc/nosey-9/cmd/nosey@$ref"
  pinned="$work/bin/nosey"
fi

"$NOSEY_BIN" -c "$work/settings.json" --cwd "$work" registry init "$work/registry" --ci github --yes
"$pinned" registry generate "$work/registry" --check
"$pinned" registry validate "$work/registry" --standalone

available="$("$pinned" registry --help)"
grep -oE 'GENERATOR_REF\}"? registry [a-z]+' "$template" | awk '{print $NF}' | sort -u | while read -r command; do
  if ! grep -qE "^  $command( |\$)" <<<"$available"; then
    echo "generator ${ref:0:7} has no 'registry $command'; bump GENERATOR_REF in $template" >&2
    exit 1
  fi
done
echo "Scaffold passes generator ${ref:0:7}"
