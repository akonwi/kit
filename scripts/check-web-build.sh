#!/usr/bin/env bash
# Refuses a browser client build the kit executable can't serve: it needs
# index.html, its third-party notices, every asset index.html names, and every
# file those assets load, including chunks a script imports only when needed
# (ADR 0059).
set -euo pipefail

build="${1:-internal/webui/build}"
index="$build/index.html"
if [[ ! -s "$index" ]]; then
  echo "No browser client build at $index. Run 'bun run build' in apps/web." >&2
  exit 1
fi
# The build ships its third-party notices and links them from the page.
if ! grep -qE '<link rel="license" href="/assets/licenses-[A-Za-z0-9]+\.txt">' "$index"; then
  echo "$index links no third-party notices (rel=\"license\")." >&2
  exit 1
fi
assets=$(grep -oE '/assets/[^"]+' "$index" | sort -u)
if [[ -z "$assets" ]]; then
  echo "$index names no assets." >&2
  exit 1
fi
while read -r asset; do
  if [[ ! -s "$build$asset" ]]; then
    echo "$index names $asset, which the build doesn't have." >&2
    exit 1
  fi
done <<< "$assets"
# Scripts import their chunks relative to /assets/; stylesheets name files
# under /assets/.
loaded=0
for file in "$build"/assets/*.js "$build"/assets/*.css; do
  [[ -e "$file" ]] || continue
  refs=$( { grep -oE '"\./[A-Za-z0-9._-]+\.(js|css)"' "$file" | tr -d '"' | sed 's|^\./|/assets/|'; \
    grep -oE '/assets/[A-Za-z0-9._-]+' "$file"; } | sort -u || true)
  [[ -z "$refs" ]] && continue
  while read -r ref; do
    if [[ ! -s "$build$ref" ]]; then
      echo "${file#"$build"} loads $ref, which the build doesn't have." >&2
      exit 1
    fi
    loaded=$((loaded + 1))
  done <<< "$refs"
done
echo "Browser client build: $index, $(wc -l <<< "$assets" | tr -d ' ') assets, and $loaded files they load."
