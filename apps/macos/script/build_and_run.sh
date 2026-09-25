#!/usr/bin/env bash
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-run}"
case "$MODE" in run|--verify|--build) ;; *) echo "usage: $0 [--verify|--build]" >&2; exit 2 ;; esac
APP_BUNDLE="$APP_ROOT/dist/Kit.app"
if [[ "$MODE" != --build ]]; then pkill -x Kit 2>/dev/null || true; fi
cd "$APP_ROOT"
# SwiftLintPlugin declares these output directories but does not create them.
PLUGIN_OUTPUT="$APP_ROOT/.build/xcode/Build/Intermediates.noindex/BuildToolPluginIntermediates"
mkdir -p "$PLUGIN_OUTPUT/codeeditsourceeditor.output/CodeEditSourceEditor/SwiftLint/Output" \
  "$PLUGIN_OUTPUT/codeedittextview.output/CodeEditTextView/SwiftLint/Output"
xcodebuild -scheme Kit -destination 'platform=macOS' \
  -derivedDataPath .build/xcode -skipPackagePluginValidation build
BIN_DIR="$APP_ROOT/.build/xcode/Build/Products/Debug"
python3 "$APP_ROOT/script/generate_wire.py" --check
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
cat > "$APP_BUNDLE/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Kit</string>
<key>CFBundleIdentifier</key><string>com.akonwi.kit</string>
<key>CFBundleName</key><string>Kit</string>
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
