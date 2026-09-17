#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ICONSET="$(mktemp -d)/Kit.iconset"
mkdir -p "$ICONSET"
trap 'rm -rf "${ICONSET%/Kit.iconset}"' EXIT
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$ROOT/app-icon.png" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  retina=$((size * 2))
  sips -z "$retina" "$retina" "$ROOT/app-icon.png" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$ROOT/Kit.icns"
