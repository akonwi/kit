#!/usr/bin/env bash
set -euo pipefail

release_ref=${1:?usage: check-openapi-compatibility.sh RELEASE_REF [CURRENT_SPEC]}
current_spec=${2:-api/kit-session.openapi.json}
release_spec=$(mktemp)
report=$(mktemp)
trap 'rm -f "$release_spec" "$report"' EXIT

# RELEASE_SPEC replaces the released contract, for testing this script.
if [[ -n ${RELEASE_SPEC:-} ]]; then
  cp "$RELEASE_SPEC" "$release_spec"
elif ! git show "$release_ref:api/kit-session.openapi.json" >"$release_spec"; then
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

# Each line excuses one exact finding; see the file's header.
exceptions="$(git rev-parse --show-toplevel)/scripts/openapi-compatibility-exceptions.txt"

run_oasdiff() {
  "$oasdiff_bin" breaking --fail-on ERR --format json --err-ignore "$exceptions" "$release_spec" "$current_spec"
}

if run_oasdiff >"$report" 2>&1; then
  cat "$report"
  exit 0
fi

# A variant added to a union the released contract declares open
# (x-kit-open-union, ADR 0072) is compatible: clients skip kinds they don't
# know. Every other error-level finding is breaking. The classifier exits 3
# when a breaking finding remains, 2 when only open-union additions do, and
# otherwise when oasdiff's report is unusable.
classification=0
python3 - "$report" "$release_spec" "$current_spec" <<'PY' || classification=$?
import json
import re
import sys

try:
    changes = json.load(open(sys.argv[1], encoding="utf-8"))
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"oasdiff failed without a valid breaking-change report: {error}")
if not isinstance(changes, list) or not any(change.get("level") == 3 for change in changes):
    raise SystemExit("oasdiff failed without reporting an error-level breaking change")

def schemas(path):
    with open(path, encoding="utf-8") as source:
        return json.load(source).get("components", {}).get("schemas", {})

released, current = schemas(sys.argv[2]), schemas(sys.argv[3])
open_variants = set()
for name, schema in current.items():
    if schema.get("x-kit-open-union") is True and released.get(name, {}).get("x-kit-open-union") is True:
        open_variants.update(variant.get("$ref") for variant in schema.get("oneOf", []))

def open_union_addition(change):
    if change.get("id") != "response-property-one-of-added":
        return False
    # oasdiff lists every variant one change added, separated by commas.
    match = re.match(r"added `([^`]+)` to the ", change.get("text", ""))
    return match is not None and all(ref.strip() in open_variants for ref in match.group(1).split(","))

breaking = False
for change in changes:
    if change.get("level") != 3:
        continue
    location = " ".join(filter(None, [change.get("operation"), change.get("path")]))
    text = change.get("text", change.get("id", "unknown change"))
    if open_union_addition(change):
        print(f"compatible: {location}: {text}")
    else:
        breaking = True
        print(f"breaking: {location}: {text}", file=sys.stderr)
sys.exit(3 if breaking else 2)
PY
case $classification in
  2)
    echo "Only variants added to open unions differ from release $release_ref; they are compatible."
    exit 0
    ;;
  3) ;;
  *)
    cat "$report" >&2
    exit 1
    ;;
esac
if (( current_version <= release_version )); then
  echo "breaking OpenAPI changes require SessionProtocolVersion greater than released protocol $release_version" >&2
  exit 1
fi

echo "Breaking OpenAPI changes are acknowledged by protocol version $current_version (release $release_ref publishes $release_version)."
