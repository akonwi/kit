#!/usr/bin/env bash
# Builds GhosttyKit.xcframework (libghostty for macOS arm64) at a pinned
# Ghostty commit and copies it into Vendor/. Requires the Zig version named by
# Ghostty's build.zig.zon and Xcode's Metal Toolchain
# (`xcodebuild -downloadComponent MetalToolchain`).
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GHOSTTY_COMMIT="${GHOSTTY_COMMIT:-83edd491e3024ae5e50393d62877b8897da1cccd}"
GHOSTTY_SRC="${GHOSTTY_SRC:-$APP_ROOT/.build/ghostty}"

if [[ ! -d "$GHOSTTY_SRC/.git" ]]; then
  git clone https://github.com/ghostty-org/ghostty.git "$GHOSTTY_SRC"
fi
git -C "$GHOSTTY_SRC" fetch --depth 1 origin "$GHOSTTY_COMMIT"
git -C "$GHOSTTY_SRC" checkout --detach "$GHOSTTY_COMMIT"

(cd "$GHOSTTY_SRC" && zig build \
  -Demit-macos-app=false \
  -Demit-xcframework=true \
  -Dxcframework-target=native \
  -Doptimize=ReleaseFast)

rm -rf "$APP_ROOT/Vendor/GhosttyKit.xcframework"
mkdir -p "$APP_ROOT/Vendor"
ditto "$GHOSTTY_SRC/macos/GhosttyKit.xcframework" "$APP_ROOT/Vendor/GhosttyKit.xcframework"
echo "GhosttyKit.xcframework @ $GHOSTTY_COMMIT -> $APP_ROOT/Vendor"
