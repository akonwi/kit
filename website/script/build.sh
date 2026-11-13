#!/bin/sh
# Assemble a standalone site with the canonical branding assets.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${1:?Usage: website/script/build.sh OUTPUT_DIRECTORY}
mkdir -p "$out/assets/kit/fonts" "$out/images"
for file in index.html theme.css kit-window.css style.css site.js backdrop.js demo.js chapters.js; do
  cp "$root/website/$file" "$out/"
done
cp "$root/website/images/"*.png "$root/website/images/"*.webp "$out/images/"
cp "$root/assets/kit/app-icon.svg" "$out/assets/kit/"
cp "$root/assets/kit/fonts/"*.ttf "$root/assets/kit/fonts/OFL.txt" "$out/assets/kit/fonts/"
