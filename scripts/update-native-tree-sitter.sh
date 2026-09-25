#!/bin/sh
# Reproduce the pinned native Tree-sitter queries, notices, and vendored grammars.
# Updating a version requires reviewing queries/tokens and deliberately refreshing
# internal/highlight/ASSET_SHA256SUMS and licenses/MANIFEST.tsv.
set -eu

root=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/kit-native-tree-sitter.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM
stage=$tmp/stage
mkdir -p "$stage/internal/highlight/queries" "$stage/internal/highlight/licenses"

module_dir() {
  go mod download -json "$1@$2" | python3 -c 'import json,sys; print(json.load(sys.stdin)["Dir"])'
}

copy_module() {
  key=$1 module=$2 version=$3
  dir=$(module_dir "$module" "$version")
  eval "${key}_dir=\$dir"
}

copy_module core github.com/tree-sitter/go-tree-sitter v0.25.0
copy_module ard github.com/akonwi/tree-sitter-ard v0.0.0-20260825025050-98a6e2e447c9
copy_module go github.com/tree-sitter/tree-sitter-go v0.25.0
copy_module typescript github.com/tree-sitter/tree-sitter-typescript v0.23.2
copy_module javascript github.com/tree-sitter/tree-sitter-javascript v0.25.0
copy_module python github.com/tree-sitter/tree-sitter-python v0.25.0
copy_module rust github.com/tree-sitter/tree-sitter-rust v0.24.2
copy_module bash github.com/tree-sitter/tree-sitter-bash v0.25.1
copy_module json github.com/tree-sitter/tree-sitter-json v0.24.8
copy_module yaml github.com/tree-sitter-grammars/tree-sitter-yaml v0.7.2
copy_module toml github.com/tree-sitter-grammars/tree-sitter-toml v0.7.0
copy_module markdown github.com/tree-sitter-grammars/tree-sitter-markdown v0.5.3
copy_module html github.com/tree-sitter/tree-sitter-html v0.23.2
copy_module css github.com/tree-sitter/tree-sitter-css v0.25.0
copy_module sql github.com/DerekStride/tree-sitter-sql v0.3.11

grep -q '"license": "MIT"' "$ard_dir/tree-sitter.json"
grep -q '"name": "Akonwi Ngoh"' "$ard_dir/tree-sitter.json"

q=$stage/internal/highlight/queries
cp "$ard_dir/queries/highlights.scm" "$q/ard.scm"
cp "$go_dir/queries/highlights.scm" "$q/go.scm"
cat "$javascript_dir/queries/highlights.scm" "$javascript_dir/queries/highlights-jsx.scm" >"$q/javascript.scm"
cat "$typescript_dir/queries/highlights.scm" "$javascript_dir/queries/highlights.scm" >"$q/typescript.scm"
cat "$typescript_dir/queries/highlights.scm" "$javascript_dir/queries/highlights.scm" "$javascript_dir/queries/highlights-jsx.scm" >"$q/tsx.scm"
for name in python rust bash json html css; do
  eval "dir=\$${name}_dir"
  cp "$dir/queries/highlights.scm" "$q/$name.scm"
done
cp "$html_dir/queries/injections.scm" "$q/html-injections.scm"
cp "$yaml_dir/queries/highlights.scm" "$q/yaml.scm"
cp "$toml_dir/queries/highlights.scm" "$q/toml.scm"
cp "$sql_dir/queries/highlights.scm" "$q/sql.scm"
cp "$markdown_dir/tree-sitter-markdown/queries/highlights.scm" "$q/markdown.scm"
cp "$markdown_dir/tree-sitter-markdown/queries/injections.scm" "$q/markdown-injections.scm"
cp "$markdown_dir/tree-sitter-markdown-inline/queries/highlights.scm" "$q/markdown_inline.scm"
cp "$markdown_dir/tree-sitter-markdown-inline/queries/injections.scm" "$q/markdown_inline-injections.scm"

for name in go typescript javascript python rust bash json yaml toml markdown html css sql; do
  eval "dir=\$${name}_dir"
  cp "$dir/LICENSE" "$stage/internal/highlight/licenses/$name.txt"
done
cp "$root/internal/highlight/licenses/ard.txt" "$stage/internal/highlight/licenses/ard.txt"
cp "$core_dir/LICENSE" "$stage/internal/highlight/licenses/tree-sitter-core.txt"
cp "$root/internal/highlight/licenses/MANIFEST.tsv" "$stage/internal/highlight/licenses/MANIFEST.tsv"

for grammar in grammarmarkdown grammarmarkdowninline; do
  mkdir -p "$stage/internal/highlight/$grammar/src/tree_sitter"
done
cp "$markdown_dir/tree-sitter-markdown/src/parser.c" "$stage/internal/highlight/grammarmarkdown/src/"
cp "$markdown_dir/tree-sitter-markdown/src/scanner.c" "$stage/internal/highlight/grammarmarkdown/src/"
cp "$markdown_dir/tree-sitter-markdown/src/tree_sitter/parser.h" "$stage/internal/highlight/grammarmarkdown/src/tree_sitter/"
cp "$markdown_dir/tree-sitter-markdown-inline/src/parser.c" "$stage/internal/highlight/grammarmarkdowninline/src/"
cp "$markdown_dir/tree-sitter-markdown-inline/src/scanner.c" "$stage/internal/highlight/grammarmarkdowninline/src/"
cp "$markdown_dir/tree-sitter-markdown-inline/src/tree_sitter/parser.h" "$stage/internal/highlight/grammarmarkdowninline/src/tree_sitter/"

mkdir -p "$stage/internal/highlight/grammarsql/src/tree_sitter"
curl --fail --location --silent --show-error \
  https://registry.npmjs.org/@derekstride/tree-sitter-sql/-/tree-sitter-sql-0.3.11.tgz |
  tar -xz -C "$tmp"
cp "$tmp/package/src/parser.c" "$tmp/package/src/scanner.c" "$stage/internal/highlight/grammarsql/src/"
cp "$tmp/package/src/tree_sitter/parser.h" "$stage/internal/highlight/grammarsql/src/tree_sitter/"

cd "$stage"
shasum -a 256 -c "$root/internal/highlight/ASSET_SHA256SUMS"
cd "$root"
backup=$tmp/backup
mkdir -p "$backup/grammarmarkdown" "$backup/grammarmarkdowninline" "$backup/grammarsql"
mv internal/highlight/queries "$backup/queries"
mv internal/highlight/licenses "$backup/licenses"
mv internal/highlight/grammarmarkdown/src "$backup/grammarmarkdown/src"
mv internal/highlight/grammarmarkdowninline/src "$backup/grammarmarkdowninline/src"
mv internal/highlight/grammarsql/src "$backup/grammarsql/src"
rollback() {
  set +e
  rm -rf internal/highlight/queries internal/highlight/licenses \
    internal/highlight/grammarmarkdown/src internal/highlight/grammarmarkdowninline/src \
    internal/highlight/grammarsql/src
  mv "$backup/queries" internal/highlight/queries
  mv "$backup/licenses" internal/highlight/licenses
  mv "$backup/grammarmarkdown/src" internal/highlight/grammarmarkdown/src
  mv "$backup/grammarmarkdowninline/src" internal/highlight/grammarmarkdowninline/src
  mv "$backup/grammarsql/src" internal/highlight/grammarsql/src
}
trap 'rollback; exit 1' INT TERM HUP
set +e
mv "$stage/internal/highlight/queries" internal/highlight/queries &&
mv "$stage/internal/highlight/licenses" internal/highlight/licenses &&
mv "$stage/internal/highlight/grammarmarkdown/src" internal/highlight/grammarmarkdown/src &&
mv "$stage/internal/highlight/grammarmarkdowninline/src" internal/highlight/grammarmarkdowninline/src &&
mv "$stage/internal/highlight/grammarsql/src" internal/highlight/grammarsql/src &&
go test ./internal/highlight
status=$?
set -e
if [ "$status" -ne 0 ]; then
  rollback
  exit "$status"
fi
rm -rf "$backup"
trap 'rm -rf "$tmp"' EXIT INT TERM HUP
