#!/bin/sh
set -eu

package_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
repo_dir=$(CDPATH= cd -- "$package_dir/../.." && pwd)
config="$package_dir/Sources/Kit/openapi-generator-config.yaml"
document="$repo_dir/api/kit-session.openapi.json"
output="$package_dir/Sources/Kit/GeneratedSources"

cd "$package_dir"
swift build --product swift-openapi-generator >/dev/null
binary="$(swift build --show-bin-path)/swift-openapi-generator"
temporary=$(mktemp -d "$package_dir/Sources/Kit/.GeneratedSources.XXXXXX")
trap 'rm -rf "$temporary"' EXIT INT TERM
"$binary" generate "$document" --config "$config" --output-directory "$temporary"

if [ "${1:-}" = "--check" ]; then
    if ! diff -ru "$output" "$temporary"; then
        echo "Generated Swift OpenAPI client is stale; run apps/macos/script/generate_openapi.sh" >&2
        exit 1
    fi
else
    rm -rf "$output"
    mv "$temporary" "$output"
    trap - EXIT INT TERM
fi
