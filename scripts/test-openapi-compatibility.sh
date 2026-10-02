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
case ${OASDIFF_RESULT:?} in
  clean) echo '[]'; exit 0 ;;
  breaking) echo '[{"id":"test-break","text":"removed test field","level":3,"operation":"GET","path":"/test"}]'; exit 1 ;;
  crash) echo 'unexpected tool failure' >&2; exit 1 ;;
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
