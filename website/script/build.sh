#!/bin/sh
# Assemble a standalone site with the canonical branding assets.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${1:?Usage: website/script/build.sh OUTPUT_DIRECTORY}
mkdir -p "$out/assets/kit/fonts"
cp "$root/website/index.html" "$root/website/style.css" "$out/"
for asset in logo-light.svg logo-dark.svg app-icon.svg; do
  cp "$root/assets/kit/$asset" "$out/assets/kit/"
done
cp "$root/assets/kit/fonts/"*.ttf "$root/assets/kit/fonts/OFL.txt" "$out/assets/kit/fonts/"
