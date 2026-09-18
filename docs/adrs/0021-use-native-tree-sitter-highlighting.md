# 0021: Use native Tree-sitter for syntax highlighting

## Status

Accepted

## Context

Kit needs one semantic syntax-highlighting service for retained files and diffs.
Transcript rendering will use the same service for Markdown fences under
`TUI-TRANSCRIPT-002`. The service must preserve readable plain text under parser
or query failure, remain independent of renderer themes, and support structural
query predicates and bounded language injections.

A broad regular-expression lexer does not provide structural injections. A
Tree-sitter WASM host adds a second runtime, grammar ABI adaptation, and query
orchestration while retaining substantial startup and memory overhead. Native
Tree-sitter provides the complete parser/query lifecycle and allows generated
grammars to be linked directly into the application.

Native Tree-sitter requires CGO and a C11 compiler for source builds. The linked
runtime and grammars remain part of the single executable.

## Decision

Kit uses the official native Tree-sitter Go binding and statically compiles a
curated, pinned grammar set into the executable. Kit does not load system
Tree-sitter libraries or runtime grammar shared objects.

The highlighting package exposes sanitized source and theme-independent
semantic spans. It owns bounded workers, queues, parser/query timeouts,
generation cancellation, match/capture/injection limits, semantic caching, and
native resource closure. Renderers resolve semantic roles at paint time.
Unsupported languages and all parser, query, timeout, limit, or validation
failures produce sanitized plain text. File and Diff surfaces consume this
service directly. Transcript Markdown fences use the same semantic service;
that renderer integration is owned by `TUI-TRANSCRIPT-002` rather than this
ADR.

Grammar and query revisions, generated sources that cannot be consumed from Go
modules, checksums, and complete license notices are reproducible and pinned.
Grammar updates must pass capture snapshots, malformed-input tests, injection
tests, cancellation and lifecycle tests, and benchmarks.

## Consequences

Kit remains a single executable with no separately installed Tree-sitter runtime
dependency, but it is no longer a CGO-free executable.

Native parsing avoids WASM startup and linear-memory overhead and supports
Tree-sitter text predicates and recursive HTML/Markdown injections. Native C
parsers and external scanners are not sandboxed; defects can crash the Kit
process. Inputs, execution time, recursion, captures, and concurrency therefore
remain bounded, and grammar changes receive the same scrutiny as native code.

Executable size grows with the curated grammar set. Grammar footprint is
tracked as that set evolves, and unusually large grammars may be replaced or
narrowed without changing the renderer-neutral contract.
