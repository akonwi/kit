# 0035: Build the terminal client with Ard and Cooper

## Status

Accepted

## Context

Kit's terminal client is its largest presentation surface. It combines a
streaming transcript, a composer, overlays and pickers, workspace panes,
and keyboard-first navigation with deterministic focus and overlay
precedence. Building it at high fidelity requires expressive modeling of UI
state and a UI library that makes layout, styling, and key handling cheap to
change.

[Ard](https://ard.run) is a language that compiles to Go. It exists to provide
language features Go lacks while retaining Go's runtime, tooling, and
ecosystem: tagged unions with exhaustive `match`, `Maybe` and `Result` with
`try` in place of nil checks and error plumbing, immutable bindings with
explicit `mut`, traits, and named arguments. Ard calls Go packages directly,
so it can use Kit's Go packages without a protocol boundary of its own.

[Cooper](https://github.com/akonwi/cooper) is a terminal UI library written in
Ard on top of Vaxis. It provides:

- a retained-mode control tree with an imperative, DOM-like API and an optional
  declarative component layer;
- Yoga-compatible flexbox layout with intrinsic sizing, alignment, and
  constraints;
- flexible per-control styling;
- a keymap system with typed commands, focus-scoped bindings with explicit
  priority, remappable control actions, and discoverable shortcuts for menus
  and help;
- animation, images, and terminal services such as clipboard, notifications,
  progress, and title.

## Decision

Kit's terminal client is written in Ard using Cooper.

The `kit` executable is built from the Ard project in `apps/cli`:

- `apps/cli/main.ard` is the process entry point. It supplies build metadata
  and the terminal client to the command line.
- The command line is Go in `apps/cli/ffi/cli`. It owns command parsing,
  headless commands, the daemon role, and interactive setup, and it invokes the
  terminal client through functions supplied by the entry point.
- `apps/cli` is a separate Go module, `github.com/akonwi/kit/apps/cli`, that
  replaces `github.com/akonwi/kit` with the repository root. Its module path
  lets Ard and `ffi` code import Kit's `internal/` packages.
- The terminal client lives in `apps/cli` as Ard modules. Like every client, it
  consumes the session-client and protocol contracts and does not import
  server or persistence internals. Renderer-neutral Go packages such as
  markdown, highlighting, and themes are called directly.
- Go remains the implementation language for the server, sessions, agent core,
  persistence, plugins, and every other non-client package.

Kit still ships as one executable. Ard lowers to Go, and `ard build` invokes the
Go toolchain with the inherited environment, so CGO and Go toolchain flags
continue to apply through `CGO_ENABLED` and `GOFLAGS`. Release builds supply the
version and commit as Ard build values with `--define`; `--release` requires
both.

## Consequences

- Building the executable requires the Ard compiler (0.42.0 or newer) and Go
  1.27 or newer, the same Go version Kit's root module requires.
- Validation spans two modules. Root `go build`, `go vet`, and `go test` do not
  include `apps/cli`; its Go tests run from `apps/cli`, and client tests run
  with `ard test` against Cooper's headless test support.
- The shipped executable resolves dependency versions from `apps/cli/go.mod`,
  which combines Kit's, Cooper's, and Vaxis's requirements. Kit's root
  `go.mod` is kept aligned so tests exercise the versions that ship.
- `ard build` writes ignored, generated Go under `apps/cli/ard-out/`, which is
  not gofmt-formatted.
- Go calls that Ard interop does not express, such as functions returning more
  than two values, use small Go bridges under `apps/cli/ffi`.
- Kit's client depends on the evolution of Ard and Cooper, which are owned by
  the same maintainer; gaps are fixed in those projects rather than worked
  around in Kit.
- Installing with `go install` is not supported; Kit is distributed as release
  executables.
