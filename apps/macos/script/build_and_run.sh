#!/usr/bin/env bash
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-run}"
case "$MODE" in run|--verify|--build) ;; *) echo "usage: $0 [--verify|--build]" >&2; exit 2 ;; esac
APP_BUNDLE="${KIT_MACOS_APP_BUNDLE:-$APP_ROOT/dist/Kit.app}"
CONFIGURATION="${KIT_MACOS_CONFIGURATION:-Debug}"
APP_VERSION="${KIT_MACOS_APP_VERSION:-0.0.0}"
APP_BUILD="${KIT_MACOS_APP_BUILD:-1}"
APP_COMMIT="${KIT_MACOS_SOURCE_COMMIT:-}"
DERIVED_DATA="${KIT_MACOS_DERIVED_DATA:-$APP_ROOT/.build/xcode}"
# Development bundles get their own identity so their preferences, window
# restoration, and process never touch an installed Kit.app. Like the CLI, a
# bundle uses ~/.kit unless it is bound to a channel: KIT_MACOS_CHANNEL=NAME
# binds it to the isolated home ~/.local/state/kit/NAME/home (ADR 0047). A
# launched GUI app does not inherit its launching shell's environment, so the
# home is written into Info.plist. `KIT_HOME` still overrides it when set,
# matching the CLI's precedence. Release bundles name no channel.
CHANNEL="${KIT_MACOS_CHANNEL:-}"
DEV_HOME=""
if [[ "${KIT_MACOS_RELEASE_BUILD:-0}" == 1 ]]; then
  BUNDLE_ID=com.akonwi.kit; BUNDLE_NAME=Kit
  [[ -z "$CHANNEL" ]] || { echo "A release build names no channel" >&2; exit 2; }
else
  BUNDLE_ID=com.akonwi.kit.dev; BUNDLE_NAME="Kit Dev"
fi
if [[ -n "$CHANNEL" ]]; then
  # One directory name, so a channel can't escape the state directory.
  [[ "$CHANNEL" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || { echo "Invalid channel '$CHANNEL': want a single directory name" >&2; exit 2; }
  DEV_HOME="$HOME/.local/state/kit/$CHANNEL/home"
fi
[[ -z "$APP_COMMIT" || "$APP_COMMIT" =~ ^[a-f0-9]{40}$ ]] || { echo "Invalid source commit" >&2; exit 2; }
[[ "$CONFIGURATION" == Debug || "$CONFIGURATION" == Release ]] || { echo "Invalid build configuration" >&2; exit 2; }
[[ "$APP_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$APP_BUILD" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] || { echo "Invalid app version or build" >&2; exit 2; }
# Stop only a previous launch of this bundle, never an installed Kit.app.
if [[ "$MODE" != --build ]]; then pkill -f "^$APP_BUNDLE/Contents/MacOS/Kit\$" 2>/dev/null || true; fi
cd "$APP_ROOT"
# libghostty is built from source at a pinned commit; this is a no-op when current.
"$APP_ROOT/script/build_ghosttykit.sh"
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
# libghostty finds xterm-ghostty and its shell integration in Contents/Resources.
rm -rf "$APP_BUNDLE/Contents/Resources/terminfo" "$APP_BUNDLE/Contents/Resources/ghostty" "$APP_BUNDLE/Contents/Resources/Licenses"
ditto "$APP_ROOT/Vendor/Ghostty/Resources" "$APP_BUNDLE/Contents/Resources"
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
dev_home_entry=""
if [[ -n "$DEV_HOME" ]]; then dev_home_entry="<key>KitDevHome</key><string>$DEV_HOME</string>"; fi
cat > "$APP_BUNDLE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Kit</string>
<key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
<key>CFBundleName</key><string>$BUNDLE_NAME</string>
<key>CFBundleShortVersionString</key><string>$APP_VERSION</string>
<key>CFBundleVersion</key><string>$APP_BUILD</string>
$commit_entry
$dev_home_entry
<key>CFBundleIconFile</key><string>Kit</string>
<key>CFBundleIconName</key><string>Kit</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSMinimumSystemVersion</key><string>15.0</string>
<key>NSPrincipalClass</key><string>KitApplication</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSAppleEventsUsageDescription</key><string>A program running in a Kit terminal wants to use AppleScript to control other apps.</string>
<key>NSBluetoothAlwaysUsageDescription</key><string>A program running in a Kit terminal wants to use Bluetooth.</string>
<key>NSCalendarsUsageDescription</key><string>A program running in a Kit terminal wants to access your calendars.</string>
<key>NSCalendarsFullAccessUsageDescription</key><string>A program running in a Kit terminal wants to access your calendars.</string>
<key>NSCameraUsageDescription</key><string>A program running in a Kit terminal wants to use the camera.</string>
<key>NSContactsUsageDescription</key><string>A program running in a Kit terminal wants to access your contacts.</string>
<key>NSLocalNetworkUsageDescription</key><string>A program running in a Kit terminal wants to access the local network.</string>
<key>NSLocationUsageDescription</key><string>A program running in a Kit terminal wants to access your location.</string>
<key>NSMicrophoneUsageDescription</key><string>A program running in a Kit terminal wants to use the microphone.</string>
<key>NSAudioCaptureUsageDescription</key><string>A program running in a Kit terminal wants to record your computer's audio.</string>
<key>NSPhotoLibraryUsageDescription</key><string>A program running in a Kit terminal wants to access your photo library.</string>
<key>NSRemindersUsageDescription</key><string>A program running in a Kit terminal wants to access your reminders.</string>
<key>NSSpeechRecognitionUsageDescription</key><string>A program running in a Kit terminal wants to use speech recognition.</string>
</dict></plist>
PLIST
# Seal the assembled development bundle after replacing its executable/resources.
codesign --force --deep --sign - "$APP_BUNDLE"
if [[ -n "$DEV_HOME" ]]; then echo "Kit home: $DEV_HOME (channel $CHANNEL)" >&2; fi
if [[ "$MODE" == --build ]]; then echo "$APP_BUNDLE"; exit; fi
/usr/bin/open -n "$APP_BUNDLE"
if [[ "$MODE" == --verify ]]; then
  sleep 1
  pgrep -f "^$APP_BUNDLE/Contents/MacOS/Kit\$" >/dev/null
  echo "Kit is running."
fi
