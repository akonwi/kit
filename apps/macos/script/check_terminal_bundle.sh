#!/usr/bin/env bash
# Checks that an assembled Kit.app carries libghostty's resources and notices,
# and that no gettext (LGPL) or Sentry code was linked with libghostty.
#
# Release builds strip the app binary, so pass the build's Kit.dSYM to read the
# symbol table from it instead; it must match the binary's UUID.
set -euo pipefail
app="${1:?usage: $0 path/to/Kit.app [path/to/Kit.dSYM]}"
dsym="${2:-}"
for path in terminfo/78/xterm-ghostty ghostty/shell-integration/zsh/ghostty-integration \
  Licenses/Ghostty-Acknowledgements.txt; do
  [[ -s "$app/Contents/Resources/$path" ]] || { echo "Missing terminal resource $path" >&2; exit 1; }
done
binary="$app/Contents/MacOS/Kit"
symbol_file="$binary"
if [[ -n "$dsym" ]]; then
  symbol_file="$dsym/Contents/Resources/DWARF/Kit"
  [[ -s "$symbol_file" ]] || { echo "Missing symbols in $dsym" >&2; exit 1; }
  uuids() { dwarfdump --uuid "$1" | awk '{print $2}' | sort; }
  [[ "$(uuids "$binary")" == "$(uuids "$symbol_file")" ]] || {
    echo "$dsym does not match $binary" >&2
    exit 1
  }
fi
symbols="$(nm "$symbol_file")"
# The absence check below means nothing if the symbol table was stripped.
grep -q ' T _ghostty_app_new$' <<<"$symbols" || { echo "libghostty symbols not found in Kit" >&2; exit 1; }
if grep -E ' _(libintl_[A-Za-z_]+|bindtextdomain|dcgettext|sentry_[A-Za-z_]+)$' <<<"$symbols"; then
  echo "Kit links gettext or Sentry code from libghostty" >&2
  exit 1
fi
