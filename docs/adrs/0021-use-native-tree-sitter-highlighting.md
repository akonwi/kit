# 0021: Use native Tree-sitter for syntax highlighting

## Status

Accepted

## Context

Kit needs one semantic syntax-highlighting service for retained files, diffs,
Markdown fences, and tool activity. The service must preserve readable plain
text under parser or query failure, remain independent of renderer themes, and
support structural query predicates and bounded language injections.

A broad regular-expression lexer does not provide structural injections. A
Tree-sitter WASM host adds a second runtime, grammar ABI adaptation, and query
orchestration while retaining substantial startup and memory overhead. Native
Tree-sitter provides the complete parser/query lifecycle and allows generated
grammars to be linked directly into the application.

Native Tree-sitter requires CGO. Kit's supported release platforms are macOS
and Linux on arm64 and amd64, so releases can be produced through a controlled
native toolchain matrix rather than arbitrary cross-compilation.

## Decision

Kit uses the official native Tree-sitter Go binding and statically compiles a
curated, pinned grammar set into the executable. Kit does not load system
Tree-sitter libraries or runtime grammar shared objects.

The release build permits CGO for this native dependency. Release automation
produces and tests distinct artifacts for:

- macOS arm64;
- macOS amd64;
- Linux arm64; and
- Linux amd64.

The Linux release pipeline owns an explicit libc compatibility policy. It uses
either an oldest-supported glibc build environment or verified static-musl
artifacts; the selected policy and minimum versions are release metadata. The
macOS pipeline pins its SDK and deployment target. Source builds require a C11
compiler, while users of published artifacts do not require a compiler or a
separate Tree-sitter installation.

The highlighting package exposes sanitized source and theme-independent
semantic spans. It owns bounded workers, queues, parser/query timeouts,
generation cancellation, match/capture/injection limits, semantic caching, and
native resource closure. Renderers resolve semantic roles at paint time.
Unsupported languages and all parser, query, timeout, limit, or validation
failures produce sanitized plain text.

Grammar and query revisions, generated sources that cannot be consumed from Go
modules, checksums, and complete license notices are reproducible and pinned.
Grammar updates must pass capture snapshots, malformed-input tests, injection
tests, cancellation and lifecycle tests, benchmarks, and every supported
release build.

## Consequences

Kit remains a single executable with no Tree-sitter runtime dependency, but it
is no longer a CGO-free executable. The release matrix and libc/SDK policy
become required release infrastructure.

Native parsing avoids WASM startup and linear-memory overhead and supports
Tree-sitter text predicates and recursive HTML/Markdown injections. Native C
parsers and external scanners are not sandboxed; defects can crash the Kit
process. Inputs, execution time, recursion, captures, and concurrency therefore
remain bounded, and grammar changes receive the same scrutiny as native code.

Executable size grows with the curated grammar set. Grammar footprint is
measured during release work, and unusually large grammars may be replaced or
narrowed without changing the renderer-neutral contract.
