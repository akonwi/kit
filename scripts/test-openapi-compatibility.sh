#!/usr/bin/env bash
set -euo pipefail

root=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
release_spec="$tmp/release.json"
git show HEAD:api/kit-session.openapi.json >"$release_spec"
release_version=$(python3 - "$release_spec" <<'PY'
import json, sys
print(int(json.load(open(sys.argv[1]))["info"]["version"]))
PY
)

make_spec() {
  python3 - "$release_spec" "$1" "$2" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    document = json.load(source)
document["info"]["version"] = sys.argv[3]
with open(sys.argv[2], "w", encoding="utf-8") as destination:
    json.dump(document, destination)
PY
}

cat >"$tmp/oasdiff" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" >"${OASDIFF_ARGS:-/dev/null}"
case ${OASDIFF_RESULT:?} in
  clean) echo '[]'; exit 0 ;;
  breaking) echo '[{"id":"test-break","text":"removed test field","level":3,"operation":"GET","path":"/test"}]'; exit 1 ;;
  crash) echo 'unexpected tool failure' >&2; exit 1 ;;
  variant-added) echo '[{"id":"response-property-one-of-added","text":"added `#/components/schemas/WidgetSpunEvent, #/components/schemas/WidgetStoppedEvent` to the `events/items/` response property `oneOf` list for the response status `200`","level":3,"operation":"GET","path":"/v1/sessions/{sessionID}/events"}]'; exit 1 ;;
  variant-added-elsewhere) echo '[{"id":"response-property-one-of-added","text":"added `#/components/schemas/WidgetSpunEvent` to the `widgets/items/` response property `oneOf` list for the response status `200`","level":3,"operation":"GET","path":"/test/widgets"}]'; exit 1 ;;
  variant-removed) echo '[{"id":"response-property-one-of-removed","text":"removed `#/components/schemas/WidgetSpunEvent` from the `events/items/` response property `oneOf` list for the response status `200`","level":3,"operation":"GET","path":"/v1/sessions/{sessionID}/events"}]'; exit 1 ;;
esac
SH
chmod +x "$tmp/oasdiff"

same="$tmp/same.json"
bumped="$tmp/bumped.json"
regressed="$tmp/regressed.json"
make_spec "$same" "$release_version"
make_spec "$bumped" "$((release_version + 1))"
make_spec "$regressed" "$((release_version - 1))"

check="$root/scripts/check-openapi-compatibility.sh"
OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=clean "$check" HEAD "$same" >/dev/null
OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=breaking "$check" HEAD "$bumped" >/dev/null
if OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=breaking "$check" HEAD "$same" >/dev/null 2>&1; then
  echo "breaking change without a version bump was accepted" >&2
  exit 1
fi
if OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=crash "$check" HEAD "$bumped" >/dev/null 2>&1; then
  echo "oasdiff crash was accepted as a versioned break" >&2
  exit 1
fi
if OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=clean "$check" HEAD "$regressed" >/dev/null 2>&1; then
  echo "protocol version regression was accepted" >&2
  exit 1
fi
OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=clean OASDIFF_ARGS="$tmp/args" "$check" HEAD "$same" >/dev/null
if ! grep -qxF -- "--err-ignore" "$tmp/args" || ! grep -qxF -- "$root/scripts/openapi-compatibility-exceptions.txt" "$tmp/args"; then
  echo "the compatibility exceptions file was not passed to oasdiff" >&2
  exit 1
fi

# A variant added to a union the released contract declares open is
# compatible; the same addition to a closed union, or a removed variant, is not.
open_union() {
  python3 - "$release_spec" "$1" "$2" "$3" "$4" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    document = json.load(source)
document["info"]["version"] = sys.argv[3]
schemas = document["components"]["schemas"]
union = schemas["SessionEvent"]
union.pop("x-kit-open-union", None)
if sys.argv[4] == "open":
    union["x-kit-open-union"] = True
# A closed union in another response shares a variant with SessionEvent.
schemas["ClosedWidgets"] = {"oneOf": [{"$ref": "#/components/schemas/WidgetStoppedEvent"}]}
document["paths"]["/test/widgets"] = {"get": {"responses": {"200": {"description": "Success", "content": {"application/json": {"schema": {
    "type": "object", "properties": {"widgets": {"type": "array", "items": {"$ref": "#/components/schemas/ClosedWidgets"}}}}}}}}}}
if sys.argv[5] == "added":
    union["oneOf"] += [{"$ref": "#/components/schemas/WidgetSpunEvent"}, {"$ref": "#/components/schemas/WidgetStoppedEvent"}]
    schemas["ClosedWidgets"]["oneOf"].append({"$ref": "#/components/schemas/WidgetSpunEvent"})
with open(sys.argv[2], "w", encoding="utf-8") as destination:
    json.dump(document, destination)
PY
}
open_release="$tmp/open-release.json"
closed_release="$tmp/closed-release.json"
open_current="$tmp/open-current.json"
open_union "$open_release" "$release_version" open released
open_union "$closed_release" "$release_version" closed released
open_union "$open_current" "$release_version" open added
RELEASE_SPEC="$open_release" OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=variant-added "$check" HEAD "$open_current" >/dev/null
if RELEASE_SPEC="$closed_release" OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=variant-added "$check" HEAD "$open_current" >/dev/null 2>&1; then
  echo "a variant added to a union the release declares closed was accepted" >&2
  exit 1
fi
if RELEASE_SPEC="$open_release" OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=variant-added-elsewhere "$check" HEAD "$open_current" >/dev/null 2>&1; then
  echo "a variant of an open union added to a closed union was accepted" >&2
  exit 1
fi
if RELEASE_SPEC="$open_release" OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=variant-added "$check" HEAD "$open_release" >/dev/null 2>&1; then
  echo "an addition the current contract doesn't make was accepted" >&2
  exit 1
fi
if RELEASE_SPEC="$open_release" OASDIFF_BIN="$tmp/oasdiff" OASDIFF_RESULT=variant-removed "$check" HEAD "$open_current" >/dev/null 2>&1; then
  echo "a variant removed from an open union was accepted" >&2
  exit 1
fi
