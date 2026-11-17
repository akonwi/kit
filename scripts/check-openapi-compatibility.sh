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

def load(path):
    with open(path, encoding="utf-8") as source:
        return json.load(source)

released_document, current_document = load(sys.argv[2]), load(sys.argv[3])
released = released_document.get("components", {}).get("schemas", {})
current = current_document.get("components", {}).get("schemas", {})
schema_prefix = "#/components/schemas/"

def resolve(schema):
    while isinstance(schema, dict) and isinstance(schema.get("$ref"), str) and schema["$ref"].startswith(schema_prefix):
        schema = current.get(schema["$ref"][len(schema_prefix):])
    return schema if isinstance(schema, dict) else None

def union_at(schema, segments):
    """Names the component schema at the response property path, or None."""
    for segment in segments:
        schema = resolve(schema)
        if schema is None:
            return None
        if segment in schema.get("properties", {}):
            schema = schema["properties"][segment]
        elif segment == "items" and "items" in schema:
            schema = schema["items"]
        else:
            return None
    reference = schema.get("$ref") if isinstance(schema, dict) else None
    if not isinstance(reference, str) or not reference.startswith(schema_prefix):
        return None
    return reference[len(schema_prefix):]

def variants(schema):
    return {variant.get("$ref") for variant in schema.get("oneOf", [])}

def open_union_addition(change):
    """Whether change only adds variants to a union both contracts declare open."""
    if change.get("id") != "response-property-one-of-added":
        return False
    # oasdiff lists every variant one change added, separated by commas.
    match = re.match(r"added `([^`]+)` to the `([^`]*)` response property `oneOf` list for the response status `(\d+)`$",
                     change.get("text", ""))
    if match is None:
        return False
    added = {reference.strip() for reference in match.group(1).split(",")}
    segments = [segment for segment in match.group(2).split("/") if segment]
    operation = current_document.get("paths", {}).get(change.get("path"), {}).get(str(change.get("operation", "")).lower(), {})
    content = operation.get("responses", {}).get(match.group(3), {}).get("content", {})
    if not content:
        return False
    for media in content.values():
        name = union_at(media.get("schema"), segments)
        if name is None or released.get(name, {}).get("x-kit-open-union") is not True or current[name].get("x-kit-open-union") is not True:
            return False
        if not added <= variants(current[name]) - variants(released[name]):
            return False
    return True

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
