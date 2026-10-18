#!/usr/bin/env bash
# Builds libghostty at a pinned Ghostty commit into Vendor/Ghostty (not
# committed):
#
#   GhosttyKit.xcframework   static libghostty for macOS arm64
#   Resources/terminfo       xterm-ghostty terminfo, bundled into Kit.app
#   Resources/ghostty        shell integration, bundled into Kit.app
#   Resources/Licenses       Ghostty-Acknowledgements.txt, bundled into Kit.app
#
# The build is skipped when Vendor/Ghostty already matches the pin, flags, and
# notices. Requires Zig $ZIG_VERSION and Xcode's Metal Toolchain
# (`xcodebuild -downloadComponent MetalToolchain`).
#
#   build_ghosttykit.sh            build if missing or out of date
#   build_ghosttykit.sh --check    exit 1 if missing or out of date
#   build_ghosttykit.sh --force    rebuild
set -euo pipefail
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-build}"
case "$MODE" in build|--check|--force) ;; *) echo "usage: $0 [--check|--force]" >&2; exit 2 ;; esac

GHOSTTY_COMMIT=83edd491e3024ae5e50393d62877b8897da1cccd
ZIG_VERSION=0.16.0
# Gettext (LGPL) and Sentry crash reporting stay out of the linked app.
BUILD_FLAGS=(-Demit-macos-app=false -Demit-xcframework=true -Dxcframework-target=native
  -Doptimize=ReleaseFast -Di18n=false -Dsentry=false -Demit-terminfo=true)

OUT="$APP_ROOT/Vendor/Ghostty"
SRC="${GHOSTTY_SRC:-$APP_ROOT/.build/ghostty-src}"
NOTICES="$APP_ROOT/ThirdParty/Ghostty"
stamp="$(
  {
    echo "$GHOSTTY_COMMIT $ZIG_VERSION ${BUILD_FLAGS[*]}"
    shasum -a 256 "${BASH_SOURCE[0]}" "$NOTICES"/* | awk '{print $1}'
  } | shasum -a 256 | awk '{print $1}'
)"

if [[ "$MODE" != --force && -f "$OUT/STAMP" && "$(cat "$OUT/STAMP")" == "$stamp" ]]; then exit 0; fi
if [[ "$MODE" == --check ]]; then
  echo "Vendor/Ghostty is missing or out of date; run $0" >&2
  exit 1
fi

command -v zig >/dev/null || { echo "Zig $ZIG_VERSION is required to build libghostty" >&2; exit 1; }
[[ "$(zig version)" == "$ZIG_VERSION" ]] || { echo "Zig $ZIG_VERSION is required, found $(zig version)" >&2; exit 1; }
xcrun -f metal >/dev/null 2>&1 || {
  echo "Xcode's Metal Toolchain is required: xcodebuild -downloadComponent MetalToolchain" >&2; exit 1; }

if [[ ! -d "$SRC/.git" ]]; then
  rm -rf "$SRC"
  git init -q "$SRC"
  git -C "$SRC" remote add origin https://github.com/ghostty-org/ghostty.git
fi
if [[ "$(git -C "$SRC" rev-parse -q --verify HEAD 2>/dev/null)" != "$GHOSTTY_COMMIT" ]]; then
  git -C "$SRC" fetch -q --depth 1 origin "$GHOSTTY_COMMIT"
  git -C "$SRC" checkout -q --detach "$GHOSTTY_COMMIT"
fi
git -C "$SRC" clean -qfdx -e zig-pkg -e .zig-cache
(cd "$SRC" && zig build "${BUILD_FLAGS[@]}")

stage="$(mktemp -d "$APP_ROOT/Vendor/.ghostty.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/Resources/ghostty" "$stage/Resources/Licenses"
ditto "$SRC/macos/GhosttyKit.xcframework" "$stage/GhosttyKit.xcframework"
ditto "$SRC/zig-out/share/terminfo" "$stage/Resources/terminfo"
ditto "$SRC/zig-out/share/ghostty/shell-integration" "$stage/Resources/ghostty/shell-integration"
[[ -f "$stage/Resources/terminfo/78/xterm-ghostty" ]] || { echo "xterm-ghostty terminfo was not built" >&2; exit 1; }

# Directory of a Zig dependency named `key` in `zon`, resolved by its hash.
dep() {
  local zon="$SRC/$1" key="$2" hash
  hash="$(awk -v k=".$key" '$1 == k && $2 == "=" { found = 1 }
    found && /\.hash *=/ { match($0, /"[^"]+"/); print substr($0, RSTART + 1, RLENGTH - 2); exit }' "$zon")"
  [[ -n "$hash" && -d "$SRC/zig-pkg/$hash" ]] || { echo "Dependency $key not found in $1" >&2; exit 1; }
  echo "$SRC/zig-pkg/$hash"
}

# Upstream URL of the Zig dependency named `key` in `zon`.
dep_url() {
  awk -v k=".$2" '$1 == k && $2 == "=" { found = 1 }
    found && /\.url *=/ { match($0, /"[^"]+"/); print substr($0, RSTART + 1, RLENGTH - 2); exit }' "$SRC/$1"
}

notice="$stage/Resources/Licenses/Ghostty-Acknowledgements.txt"
section() {
  local name="$1" detail="$2"; shift 2
  {
    printf '\n================================================================================\n'
    printf '%s\n%s\n' "$name" "$detail"
    printf '================================================================================\n\n'
    for file in "$@"; do
      [[ -s "$file" ]] || { echo "Missing license text $file" >&2; exit 1; }
      cat "$file"; printf '\n'
    done
  } >> "$notice"
}

freetype="$(dep pkg/freetype/build.zig.zon freetype)"
extract="$stage/extracted"
mkdir -p "$extract"
sed -n '/^License$/,$p' "$freetype/src/bdf/README" > "$extract/freetype-bdf.txt"
sed -n '/^License$/,$p' "$freetype/src/pcf/README" > "$extract/freetype-pcf.txt"
sed -n '/This software is available under 2 licenses/,/^\*\//p' "$SRC/src/stb/stb_image.h" > "$extract/stb.txt"
cat > "$notice" <<EOF
Kit's terminal is built on libghostty from Ghostty
(https://github.com/ghostty-org/ghostty), commit $GHOSTTY_COMMIT,
built with Zig $ZIG_VERSION. The software below is included in Kit.app with
its terminal, under the licenses that follow.

Portions of this software are copyright © The FreeType Project
(https://freetype.org). All rights reserved.

z2d is used under the Mozilla Public License 2.0. Its source code is available
at $(dep_url build.zig.zon z2d).
EOF
section "Ghostty" "https://github.com/ghostty-org/ghostty · MIT License" "$SRC/LICENSE"
section "FreeType" "https://freetype.org · FreeType License" \
  "$freetype/docs/FTL.TXT" "$extract/freetype-bdf.txt" "$extract/freetype-pcf.txt"
section "libpng" "http://www.libpng.org · PNG Reference Library License" "$(dep pkg/libpng/build.zig.zon libpng)/LICENSE"
section "zlib" "https://zlib.net · zlib License" "$(dep pkg/zlib/build.zig.zon zlib)/LICENSE"
section "Oniguruma" "https://github.com/kkos/oniguruma · BSD 2-Clause License" "$(dep pkg/oniguruma/build.zig.zon oniguruma)/COPYING"
section "glslang" "https://github.com/KhronosGroup/glslang · BSD, MIT, and Apache 2.0 Licenses" "$(dep pkg/glslang/build.zig.zon glslang)/LICENSE.txt"
section "SPIRV-Cross" "https://github.com/KhronosGroup/SPIRV-Cross · Apache License 2.0" "$(dep pkg/spirv-cross/build.zig.zon spirv_cross)/LICENSE"
section "Highway" "https://github.com/google/highway · Apache License 2.0" "$(dep pkg/highway/build.zig.zon highway)/LICENSE"
section "simdutf" "https://github.com/simdutf/simdutf · MIT License" "$NOTICES/simdutf-LICENSE-MIT.txt"
section "Wuffs" "https://github.com/google/wuffs · MIT License" "$(dep pkg/wuffs/build.zig.zon wuffs)/LICENSE-MIT"
section "Dear ImGui" "https://github.com/ocornut/imgui · MIT License" "$(dep pkg/dcimgui/build.zig.zon imgui)/LICENSE.txt"
section "Dear Bindings" "https://github.com/dearimgui/dear_bindings · MIT License" "$NOTICES/dear_bindings-LICENSE.txt"
section "stb" "https://github.com/nothings/stb · MIT License or public domain" "$extract/stb.txt"
section "libxev" "https://github.com/mitchellh/libxev · MIT License" "$(dep build.zig.zon libxev)/LICENSE"
section "zig-objc" "https://github.com/mitchellh/zig-objc · MIT License" "$(dep build.zig.zon zig_objc)/LICENSE"
section "uucode" "https://github.com/jacobsandlund/uucode · MIT License" "$(dep build.zig.zon uucode)/LICENSE.md"
section "z2d" "https://github.com/vancluever/z2d · Mozilla Public License 2.0" "$NOTICES/z2d-LICENSE.txt"
section "Zig standard library and compiler-rt" "https://ziglang.org · MIT License" "$NOTICES/zig-LICENSE.txt"
section "JetBrains Mono" "https://github.com/JetBrains/JetBrainsMono · SIL Open Font License 1.1" "$(dep build.zig.zon jetbrains_mono)/OFL.txt"
section "Symbols Nerd Font" "https://github.com/ryanoasis/nerd-fonts · MIT License" "$(dep build.zig.zon nerd_fonts_symbols_only)/LICENSE"
section "bash-preexec" "https://github.com/rcaloras/bash-preexec · MIT License" "$NOTICES/bash-preexec-LICENSE.txt"

rm -rf "$extract"
echo "$stamp" > "$stage/STAMP"
rm -rf "$OUT"
mv "$stage" "$OUT"
trap - EXIT
echo "libghostty @ $GHOSTTY_COMMIT -> $OUT" >&2
