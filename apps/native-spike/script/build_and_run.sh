#!/usr/bin/env bash
# Builds and ad-hoc signs dist/KitNativeSpike.app, then launches it.
#   script/build_and_run.sh [--build]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LAUNCH=1
[[ "${1:-}" == "--build" ]] && LAUNCH=0
if [[ ! -d "$ROOT/Vendor/GhosttyKit.xcframework" ]]; then
  "$ROOT/script/build_ghosttykit.sh"
fi
cd "$ROOT"
swift build -c "${CONFIG:-debug}" --product KitNativeSpike
BIN="$(swift build -c "${CONFIG:-debug}" --show-bin-path)/KitNativeSpike"
APP="$ROOT/dist/KitNativeSpike.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/KitNativeSpike"
cp "$ROOT/Resources/Info.plist" "$APP/Contents/Info.plist"
cp -R "$ROOT/Resources/Fonts" "$APP/Contents/Resources/Fonts"
codesign --force --deep --sign - "$APP" >/dev/null
echo "Built $APP"
if [[ "$LAUNCH" == 1 ]]; then
  pkill -x KitNativeSpike 2>/dev/null || true
  open "$APP"
fi
