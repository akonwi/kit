#!/usr/bin/env bash
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-run}"
case "$MODE" in run|--verify|--build) ;; *) echo "usage: $0 [--verify|--build]" >&2; exit 2 ;; esac
APP_BUNDLE="${KIT_MACOS_APP_BUNDLE:-$APP_ROOT/dist/Kit.app}"
CONFIGURATION="${KIT_MACOS_CONFIGURATION:-Debug}"
APP_VERSION="${KIT_MACOS_APP_VERSION:-0.0.0}"
APP_BUILD="${KIT_MACOS_APP_BUILD:-1}"
CLIENT_RELEASE="${KIT_MACOS_CLIENT_RELEASE:-dev}"
APP_COMMIT="${KIT_MACOS_SOURCE_COMMIT:-}"
DERIVED_DATA="${KIT_MACOS_DERIVED_DATA:-$APP_ROOT/.build/xcode}"
[[ -z "$APP_COMMIT" || "$APP_COMMIT" =~ ^[a-f0-9]{40}$ ]] || { echo "Invalid source commit" >&2; exit 2; }
[[ "$CONFIGURATION" == Debug || "$CONFIGURATION" == Release ]] || { echo "Invalid build configuration" >&2; exit 2; }
[[ "$APP_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$APP_BUILD" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] || { echo "Invalid app version or build" >&2; exit 2; }
[[ "$CLIENT_RELEASE" == dev || "$CLIENT_RELEASE" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Invalid Kit client release" >&2; exit 2; }
if [[ "${KIT_MACOS_RELEASE_BUILD:-0}" == 1 ]]; then
  # The protocol-45 stable-release parser must agree with Swift/Go, not just
  # accept a numeric-looking label that would fail compatibility at runtime.
  python3 - "$CLIENT_RELEASE" <<'PYCLIENT'
import sys
v = sys.argv[1]
parts = v.split(".")
if (len(v) > 64 or len(parts) != 3 or
    any(not p or (len(p) > 1 and p[0] == "0") or not p.isascii() or
        not p.isdecimal() or int(p) > 18446744073709551615 for p in parts)):
    sys.exit("Release build requires a canonical Kit client release")
release = tuple(map(int, parts))
if release < (0, 42, 0):
    sys.exit("Protocol 45 requires Kit client release >= 0.42.0")
PYCLIENT
fi
if [[ "$MODE" != --build ]]; then pkill -x Kit 2>/dev/null || true; fi
cd "$APP_ROOT"
# SwiftLintPlugin declares these output directories but does not create them.
PLUGIN_OUTPUT="$DERIVED_DATA/Build/Intermediates.noindex/BuildToolPluginIntermediates"
mkdir -p "$PLUGIN_OUTPUT/codeeditsourceeditor.output/CodeEditSourceEditor/SwiftLint/Output" \
  "$PLUGIN_OUTPUT/codeedittextview.output/CodeEditTextView/SwiftLint/Output"
build_args=(-scheme Kit -destination 'platform=macOS' -configuration "$CONFIGURATION"
  -derivedDataPath "$DERIVED_DATA")
if [[ "${KIT_MACOS_RELEASE_BUILD:-0}" == 1 ]]; then
  build_args+=(-disableAutomaticPackageResolution)
else
  build_args+=(-skipPackagePluginValidation)
fi
if [[ -n "${KIT_MACOS_ARCH:-}" ]]; then build_args+=("ARCHS=$KIT_MACOS_ARCH" ONLY_ACTIVE_ARCH=NO); fi
xcodebuild "${build_args[@]}" build
BIN_DIR="$DERIVED_DATA/Build/Products/$CONFIGURATION"
mkdir -p "$APP_BUNDLE/Contents/MacOS" "$APP_BUNDLE/Contents/Resources"
mkdir -p "$APP_BUNDLE/Contents/Resources/Fonts"
cp "$APP_ROOT/../../assets/kit/fonts/"*.ttf "$APP_BUNDLE/Contents/Resources/Fonts/"
cp "$APP_ROOT/../../assets/kit/fonts/OFL.txt" "$APP_BUNDLE/Contents/Resources/Fonts/"
# Grammar queries are SwiftPM resource bundles, needed by the packaged app too.
for resource in "$BIN_DIR"/*.bundle; do
  [[ -d "$resource" ]] || continue
  ditto "$resource" "$APP_BUNDLE/Contents/Resources/$(basename "$resource")"
  # Remove the old unsealed root-level copy from earlier development builds.
  rm -rf "$APP_BUNDLE/$(basename "$resource")"
done
cp "$BIN_DIR/Kit" "$APP_BUNDLE/Contents/MacOS/Kit"
# Compile appearance-aware Icon Composer layers for Tahoe and later.
ICON_OUTPUT="$APP_ROOT/.build/icon"
mkdir -p "$ICON_OUTPUT"
xcrun actool "$APP_ROOT/../../assets/kit/Kit.icon" \
  --compile "$ICON_OUTPUT" --platform macosx --minimum-deployment-target 15.0 \
  --app-icon Kit --output-partial-info-plist "$ICON_OUTPUT/icon.plist"
cp "$ICON_OUTPUT/Assets.car" "$APP_BUNDLE/Contents/Resources/Assets.car"
# Preserve the approved graphite fallback for older macOS releases.
cp "$APP_ROOT/../../assets/kit/Kit.icns" "$APP_BUNDLE/Contents/Resources/Kit.icns"
# Private recordings are never part of the normal app bundle.
rm -f "$APP_BUNDLE/Contents/Resources/fixture.json" "$APP_BUNDLE/Contents/Resources/workspace.json"
commit_entry=""
if [[ -n "$APP_COMMIT" ]]; then commit_entry="<key>KitSourceCommit</key><string>$APP_COMMIT</string>"; fi
cat > "$APP_BUNDLE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Kit</string>
<key>CFBundleIdentifier</key><string>com.akonwi.kit</string>
<key>CFBundleName</key><string>Kit</string>
<key>CFBundleShortVersionString</key><string>$APP_VERSION</string>
<key>CFBundleVersion</key><string>$APP_BUILD</string>
<key>KitClientRelease</key><string>$CLIENT_RELEASE</string>
$commit_entry
<key>CFBundleIconFile</key><string>Kit</string>
<key>CFBundleIconName</key><string>Kit</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSMinimumSystemVersion</key><string>15.0</string>
<key>NSPrincipalClass</key><string>KitApplication</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
# Seal the assembled development bundle after replacing its executable/resources.
codesign --force --deep --sign - "$APP_BUNDLE"
if [[ "$MODE" == --build ]]; then echo "$APP_BUNDLE"; exit; fi
/usr/bin/open -n "$APP_BUNDLE"
if [[ "$MODE" == --verify ]]; then
  sleep 1
  pgrep -x Kit >/dev/null
  echo "Kit is running."
fi
