#!/usr/bin/env bash
set -euo pipefail

release_ref=${1:?usage: check-openapi-compatibility.sh RELEASE_REF [CURRENT_SPEC]}
current_spec=${2:-api/kit-session.openapi.json}
release_spec=$(mktemp)
report=$(mktemp)
trap 'rm -f "$release_spec" "$report"' EXIT

if ! git show "$release_ref:api/kit-session.openapi.json" >"$release_spec"; then
  echo "release $release_ref does not publish api/kit-session.openapi.json" >&2
  exit 1
fi

read_version() {
  python3 - "$1" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    value = json.load(source)["info"]["version"]
try:
    version = int(value)
except (TypeError, ValueError):
    raise SystemExit(f"OpenAPI info.version must be an integer, got {value!r}")
if version < 1:
    raise SystemExit(f"OpenAPI info.version must be positive, got {version}")
print(version)
PY
}

release_version=$(read_version "$release_spec")
current_version=$(read_version "$current_spec")
if (( current_version < release_version )); then
  echo "session protocol version regressed: release $release_ref publishes $release_version, current contract publishes $current_version" >&2
  exit 1
fi

oasdiff_bin=${OASDIFF_BIN:-$(command -v oasdiff || true)}
if [[ -z $oasdiff_bin ]]; then
  echo "oasdiff is required; install github.com/oasdiff/oasdiff@v1.33.0 or set OASDIFF_BIN" >&2
  exit 1
fi

run_oasdiff() {
  "$oasdiff_bin" breaking --fail-on ERR --format json "$release_spec" "$current_spec"
}

if run_oasdiff >"$report" 2>&1; then
  cat "$report"
  exit 0
fi

if ! python3 - "$report" <<'PY'
import json
import sys

try:
    changes = json.load(open(sys.argv[1], encoding="utf-8"))
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"oasdiff failed without a valid breaking-change report: {error}")
if not isinstance(changes, list) or not any(change.get("level") == 3 for change in changes):
    raise SystemExit("oasdiff failed without reporting an error-level breaking change")
for change in changes:
    if change.get("level") == 3:
        location = " ".join(filter(None, [change.get("operation"), change.get("path")]))
        print(f"breaking: {location}: {change.get('text', change.get('id', 'unknown change'))}", file=sys.stderr)
PY
then
  cat "$report" >&2
  exit 1
fi
if (( current_version <= release_version )); then
  echo "breaking OpenAPI changes require SessionProtocolVersion greater than released protocol $release_version" >&2
  exit 1
fi

echo "Breaking OpenAPI changes are acknowledged by protocol version $current_version (release $release_ref publishes $release_version)."
