#!/usr/bin/env bash
# Build, sign, notarize, staple, and package an Apple Silicon desktop app.
# Does not tag, upload, publish, launch the app, or start a daemon.
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tag="${1:-}"
if [[ ! "$tag" =~ ^macos-v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "usage: $0 macos-vX.Y.Z" >&2
  exit 2
fi
version="${BASH_REMATCH[1]}"
# The app is versioned independently; this source implements Kit's stable
# protocol-42 client contract beginning with Kit release 0.39.0.
client_release="0.39.0"
# Require a canonical app version before stamping a bundle.
python3 - "$version" <<'PYVERSION'
import sys
v = sys.argv[1]
if len(v) > 64:
    sys.exit("App version exceeds the canonical release limit")
parts = v.split(".")
if (len(parts) != 3 or any(not p or (len(p) > 1 and p[0] == "0") or
                           not p.isascii() or not p.isdecimal() or
                           int(p) > 18446744073709551615 for p in parts)):
    sys.exit("A canonical stable app version is required")
PYVERSION
repo="$(cd "$APP_ROOT/../.." && pwd)"
if [[ "$(git -C "$repo" rev-parse --is-shallow-repository)" != false ]]; then
  echo "A full checkout is required to derive a reliable build number" >&2
  exit 2
fi
commit="$(git -C "$repo" rev-parse HEAD)"
# A positive build number derived from the reviewed commit, increasing on
# ancestry-based releases; verify monotonicity against the previous app release.
build="$(git -C "$repo" rev-list --count "$commit")"
[[ "$build" =~ ^[1-9][0-9]*$ ]] || { echo "Invalid source-derived build number" >&2; exit 2; }
if [[ -n "$(git -C "$repo" status --porcelain)" ||
      "$(git -C "$repo" rev-parse -q --verify "refs/tags/$tag^{commit}" 2>/dev/null)" != "$commit" ]]; then
  echo "A clean checkout at the reviewed app release tag is required" >&2
  exit 2
fi
identity="${KIT_DEVELOPER_ID_APPLICATION:-}"
profile="${KIT_NOTARY_KEYCHAIN_PROFILE:-kit}"
expected_team='M7B73F53MK'
if [[ "$identity" != "Developer ID Application:"*"($expected_team)" || -z "$profile" ]]; then
  echo "A Developer ID Application identity and notarytool keychain profile are required" >&2
  exit 2
fi
identities="$(security find-identity -v -p codesigning)"
if [[ "$identities" != *"\"$identity\""* ]]; then
  echo "The Developer ID Application identity is not available in the local keychain" >&2
  exit 2
fi

mkdir -p "$APP_ROOT/dist/releases"
artifact="kit_${tag}_darwin_arm64.zip"
if [[ -e "$APP_ROOT/dist/releases/$artifact" || -e "$APP_ROOT/dist/releases/$artifact.sha256" ]]; then
  echo "Release artifact already exists; refusing to overwrite it" >&2
  exit 2
fi
staging="$(mktemp -d "$APP_ROOT/dist/releases/.staging.XXXXXX")"
trap 'rm -rf "$staging"' EXIT
app="$staging/Kit.app"
KIT_MACOS_APP_BUNDLE="$app" KIT_MACOS_CONFIGURATION=Release KIT_MACOS_ARCH=arm64 \
  KIT_MACOS_RELEASE_BUILD=1 KIT_MACOS_DERIVED_DATA="$staging/DerivedData" \
  KIT_MACOS_APP_VERSION="$version" KIT_MACOS_APP_BUILD="$build" KIT_MACOS_SOURCE_COMMIT="$commit" \
  KIT_MACOS_CLIENT_RELEASE="$client_release" \
  "$APP_ROOT/script/build_and_run.sh" --build

[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")" == "$version" ]]
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$app/Contents/Info.plist")" == "$build" ]]
[[ "$(/usr/libexec/PlistBuddy -c 'Print :KitClientRelease' "$app/Contents/Info.plist")" == "$client_release" ]]
[[ "$(/usr/libexec/PlistBuddy -c 'Print :KitSourceCommit' "$app/Contents/Info.plist")" == "$commit" ]]
[[ "$(lipo -archs "$app/Contents/MacOS/Kit")" == arm64 ]] || { echo "Expected an arm64 app" >&2; exit 1; }
[[ ! -e "$app/Contents/Resources/fixture.json" && ! -e "$app/Contents/Resources/workspace.json" ]]
if [[ -n "$(find "$app/Contents" -type d \( -name '*.framework' -o -name '*.appex' -o -name '*.xpc' -o -name '*.app' -o -name '*.plugin' \) -print -quit)" ]]; then
  echo "New nested code bundles require an explicit signing review" >&2
  exit 1
fi
while IFS= read -r -d '' entry; do
  if [[ "$entry" != "$app/Contents/MacOS/Kit" && "$(file -b "$entry")" == *Mach-O* ]]; then
    echo "Unexpected nested Mach-O executable: ${entry#"$app"/}" >&2
    exit 1
  fi
done < <(find "$app/Contents" -type f -print0)
# Resource bundles can carry signatures of their own. Sign inside-out; never
# rely on --deep to repair nested code at verification time.
while IFS= read -r -d '' bundle; do
  codesign --force --timestamp --sign "$identity" "$bundle"
  codesign --verify --strict "$bundle"
  bundle_signature="$(codesign --display --verbose=4 "$bundle" 2>&1)"
  [[ "$bundle_signature" == *"TeamIdentifier=$expected_team"* && "$bundle_signature" == *"Authority=$identity"* ]] || {
    echo "Unexpected embedded bundle signing identity" >&2
    exit 1
  }
done < <(find "$app/Contents/Resources" -type d -name '*.bundle' -print0)
codesign --force --options runtime --timestamp --sign "$identity" "$app"
codesign --verify --deep --strict --verbose=2 "$app"
signature="$(codesign --display --verbose=4 "$app" 2>&1)"
[[ "$signature" == *"TeamIdentifier=$expected_team"* && "$signature" == *"Authority=$identity"* && "$signature" == *"flags=0x10000(runtime)"* ]] || {
  echo "Unexpected Developer ID authority, team, or hardened runtime" >&2
  exit 1
}

submission="$staging/notarization.zip"
ditto -c -k --keepParent "$app" "$submission"
xcrun notarytool submit "$submission" --keychain-profile "$profile" --wait
xcrun stapler staple "$app"
xcrun stapler validate "$app"
spctl --assess --type execute --verbose=2 "$app"

ditto -c -k --keepParent "$app" "$staging/$artifact"
(cd "$staging" && shasum -a 256 "$artifact" > "$artifact.sha256" && shasum -c "$artifact.sha256")
mv "$staging/$artifact" "$staging/$artifact.sha256" "$APP_ROOT/dist/releases/"
printf 'Ready for downloaded-artifact verification (not published): %s\n' "$APP_ROOT/dist/releases/$artifact"
