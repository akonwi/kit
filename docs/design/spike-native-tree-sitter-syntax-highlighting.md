# Native Tree-sitter TUI syntax-highlighting implementation

Status: implementation evidence for accepted [ADR 0021](../adrs/0021-use-native-tree-sitter-highlighting.md). The release toolchain and libc/deployment-target policy remain release work.

## Architecture

`internal/highlight` remains renderer- and theme-neutral: callers submit language/path, source, and revision; results contain sanitized source and semantic byte-range roles. The retained file viewer renders plain selectable text immediately, highlights on a bounded worker pool, accepts only the current generation, cancels hidden/disposed work, and resolves roles through `SemanticTheme` while painting.

The backend now uses `github.com/tree-sitter/go-tree-sitter` v0.25.0 and statically compiles selected generated C grammars into the Kit executable. Parsers and compiled queries are owned per worker and reused serially. Trees and query cursors are closed after every operation; parsers and queries close when workers shut down. Query text predicates (`#eq?`, `#match?`, `#any-of?`, and variants) are evaluated by the official binding. Patterns requiring unsupported local-scope property predicates or custom predicates are disabled rather than misclassified. Curated HTML and Markdown injection queries are recursively parsed, and deeper-language captures override parent captures.

Bounds are two workers, an eight-job queue, a 750 ms job deadline, a 100 ms per-query cap, 512 KiB primary source, 1 MiB cumulative injected source, four injection levels, 16,384 in-progress native matches, and 100,000 matches/captures/output spans. The semantic LRU accounts for at most 8 MiB. A per-worker native cancellation flag observes parse cancellation without retaining callback handles; query execution uses the remaining request deadline capped at 100 ms because the binding's callback iterator is unsafe to retain between calls. Any unsupported grammar, timeout, query failure, limit, or invalid span falls back to sanitized plain text.

## Languages and injections

Implemented and tested:

- Ard
- Go
- TypeScript and TSX
- JavaScript and JSX
- Python
- Rust
- Bash
- JSON
- YAML
- TOML
- Markdown and Markdown inline
- HTML
- CSS
- SQL

HTML `<script>` and `<style>` content is injected as JavaScript and CSS. Markdown inline content, fenced code, HTML blocks, YAML front matter, and TOML front matter use the pinned Markdown injection queries. JavaScript/TypeScript tagged-template and Rust macro injection queries are deliberately not registered because they require combined-range/include-children semantics not implemented by this host. Regex, JSDoc, Glimmer, and other unregistered targets remain plain. Injection recursion and byte use are bounded.

Ard is imported as Go module `github.com/akonwi/tree-sitter-ard` pinned to commit `98a6e2e447c9b95cc677d5b37b74117f5d3f47a5`; its matching highlights query is copied under `internal/highlight/queries` by the update script. Markdown and SQL modules do not publish usable generated Go bindings in the selected releases, so their pinned generated parsers/scanners are also vendored under `internal/highlight/grammarmarkdown*` and `internal/highlight/grammarsql`. Other grammars come from pinned Go modules. `scripts/update-native-tree-sitter.sh` reconstructs copied queries, notices, and generated sources and verifies `internal/highlight/ASSET_SHA256SUMS`. Adding a grammar means pinning its module/revision, adding its constructor and queries to the registry, recording its license, extending the update script, and adding capture, malformed-input, cancellation, and injection tests.

## Build and portability impact

This implementation requires `CGO_ENABLED=1` and a C compiler. `CGO_ENABLED=0 go build ./...` intentionally fails. Prebuilt users do not need a compiler or external Tree-sitter installation: runtime and grammars are linked into one executable.

Release builds must be produced and tested per target (`darwin/arm64`, `darwin/amd64`, `linux/arm64`, and `linux/amd64`). Local cross-compilation succeeded for all four using Clang for macOS and Zig for Linux; this is compile evidence, not native runtime verification. Linux still needs an explicit oldest-glibc or static-musl policy, and macOS needs a pinned SDK/deployment target. The measured macOS binary links only normal system frameworks/libraries (`libSystem`, `libresolv`, CoreFoundation, and Security), not a separately installed Tree-sitter library. Native parsers and external scanners are not sandboxed and can crash the Kit process, so grammar updates require native smoke and race testing.

All components declare MIT licensing. Exact modules, versions, and notices are recorded in `internal/highlight/licenses/MANIFEST.tsv` and adjacent files. The pinned Ard revision has no standalone `LICENSE`; `ard.txt` explicitly records the MIT identifier and author from its `tree-sitter.json` metadata and reproduces the standard terms without inventing a copyright year.

## Measurements

Apple M3 Pro, darwin/arm64, Go 1.27.1 against the module's Go 1.26 target, one benchmark iteration:

```sh
go test ./internal/highlight -run '^$' -bench 'BenchmarkNativeTreeSitter$' -benchtime=1x -count=1 -benchmem
go test ./internal/highlight -run '^$' -bench 'BenchmarkNativeTreeSitterRetainedHeap$' -benchtime=1x -count=1
```

| Scenario | Time | Allocated bytes / allocations |
|---|---:|---:|
| cold parser + Go query, 10 KiB | 2.88 ms | 429 KB / 4,511 |
| warm Go, 10 KiB | 1.45 ms | 382 KB / 4,117 |
| warm Go, 100 KiB | 14.03 ms | 4.40 MB / 45,758 |
| near 1 MiB | 4.09 ms | 952 B / 9 (intentional 512 KiB fallback) |
| malformed multiline, about 10 KiB | 20.57 ms | 2.97 MB / 4,819 |
| JSX sample | 5.25 ms | 59 KB / 613 |
| Markdown fenced injection | 3.92 ms | 59 KB / 570 |
| HTML script/style injection | 5.54 ms | 95 KB / 899 |

Four 100 KiB parses retained no measurable additional Go heap after GC in the sample run (`-80 KB`, measurement noise). This benchmark cannot measure C heap. The implementation explicitly closes every tree/cursor and worker-owned parser/query and runs a repeated grammar/injection lifecycle test; process-RSS soak measurement remains a release verification item.

At base revision `0b40a72bb2312d988891ad2346a0fd92f43ba9f8`, a stripped build was 35,714,658 bytes. The native implementation built with:

```sh
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /tmp/kit-native-tree-sitter ./cmd/kit
```

was 45,751,890 bytes: **+10,037,232 bytes (+28.10%)**. Ard adds 165,328 bytes to the stripped executable. The SQL generated parser remains the largest source/asset contributor. Vendored generated source occupies about 4.3 MiB for Markdown/inline and 17 MiB for SQL; module-cache grammar sources are build-time dependencies.

## Comparison and recommendation

Compared with the Chroma spike, native Tree-sitter is approximately 2.5× faster at 10 KiB and 100 KiB, allocates substantially less, supports structural predicates and real HTML/Markdown injections, and covers the full requested language set. Compared with the WASM candidate, it removes runtime compilation, linear-memory instances, incomplete predicates, whole-instance recycling, and ABI traps.

Costs are the 28.10% executable increase, a native release matrix, libc policy on Linux, compiler requirements for source builds, and loss of memory sandboxing. If Kit accepts CGO, this native backend is the strongest production architecture of the evaluated candidates. Productionization should next establish the release toolchain/compatibility policy, reduce the SQL footprint if possible, and share this semantic service with diffs, transcript fences, and tool activity.
