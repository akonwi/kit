# Ard entry point

`apps/cli` is an Ard project that builds the `kit` executable. `main.ard` is
the process entry point: it applies build metadata, turns interrupt and
termination signals into command cancellation and exit codes, and runs the
interactive commands. The command line in `ffi/cli` parses commands and runs
the headless ones and the daemon role (`cli.Parse`); for an interactive
command it connects to the server and loads settings (`cli.OpenEnvironment`)
and prepares the terminal client's options (`Environment.Prepare`), while
`main.ard` runs the terminal apps in between: `kit sessions` runs the inline
session picker (`tui::pick_session`) and then the terminal client
(`tui::run`). Go calls whose result shapes Ard cannot import use small
bridges such as `ffi/contextbridge`.

Releases build `kit` from this entry point (`.github/workflows/release.yml`).
The terminal client is written in Ard with Cooper (`tui.ard`, `tui/`).

The directory is a separate Go module (`github.com/akonwi/kit/apps/cli`) that
replaces `github.com/akonwi/kit` with the repository root, so root
`go build ./...` and `go test ./...` do not include it. Building requires Ard
0.43.0 or newer (CI uses the version in `.github/actions/setup-ard/action.yml`)
and Go 1.27 or newer.

## Cooper

The terminal client depends on [Cooper](https://github.com/akonwi/cooper) as
a Git dependency pinned to a commit in `ard.toml` and locked in `ard.lock`.
Ard builds from its dependency cache and never fetches on its own, so restore
the locked commit after cloning or after the lock changes:

```sh
ard deps fetch
```

To move to a newer Cooper commit that is on GitHub, run
`ard add github.com/akonwi/cooper@<commit>` (or `ard update cooper` for the
latest `main`) and commit `ard.toml` and `ard.lock` together. To develop
against an unpushed local Cooper checkout, temporarily set
`cooper = { path = "../../../cooper" }` in `ard.toml` and move `ard.lock`
aside: Ard builds the locked commit while the lock pins one, ignoring the path.
Push the Cooper change and pin its commit with `ard add`, which writes a new
lock, before committing Kit.

## Develop

Run commands from this directory. A build bound to a channel uses
`~/.local/state/kit/<channel>/home` as its default Kit home instead of `~/.kit`.
See `.agents/skills/worktree-development/SKILL.md` before starting a daemon.

```sh
ard deps fetch
ard format --check .
ard check main.ard
ard test
gofmt -l ffi
go vet ./...
KIT_HOME="$(mktemp -d)" go test ./...
ard build main.ard --out /tmp/kit-dev/kit
KIT_HOME=/tmp/kit-dev/home /tmp/kit-dev/kit version
```

The last line prints the isolated home a channel-bound build resolves, because
that home is not the default one.

`ard build` regenerates `ard-out/`, which is ignored and not formatted.

## Mermaid fixtures

The Mermaid renderer in `tui/mermaid` is the reference the macOS app's Swift
port in `apps/macos/Sources/KitMermaid` follows
([ADR 0046](../../docs/adrs/0046-draw-mermaid-diagrams-with-kits-own-renderer.md)).
`test/mermaid_fixtures_test.ard` checks it against the shared fixtures in
`testdata/mermaid`: each `.mmd` source and its drawing in the matching `.txt`.
After changing the renderer, or adding a source, review the drawings it makes
and write them:

```sh
KIT_UPDATE_MERMAID_FIXTURES=1 ard test test/mermaid_fixtures_test.ard
```

Then update the Swift port in the same change until the macOS client's
`MermaidFixtureTests` pass.

## Diff fixtures

`FencedDiff` in `ffi/toolpresentation` decides how each line of a ```` ```diff ````
or ```` ```patch ```` fence reads, and the macOS app's `UnifiedDiff` follows it.
The shared fixtures in `testdata/diffs` pair each `.diff` source with the kind
and text of its lines in the matching `.txt`. After changing how lines are read,
or adding a source, review the records it makes and write them:

```sh
KIT_UPDATE_DIFF_FIXTURES=1 go test ./ffi/toolpresentation
```

Then update `UnifiedDiff` in the same change until the macOS client's
`DiffFenceTests` pass.

## Release build

Release builds must define the version and commit; `--release` rejects
defaults. Build values replace `-ldflags -X` version stamping. `ard build`
inherits the environment, so Go toolchain flags pass through `GOFLAGS`; append
to an existing `GOFLAGS` rather than replacing it. With Go 1.22 and newer,
`-s` also implies `-w`.

```sh
CGO_ENABLED=1 GOFLAGS="${GOFLAGS:+$GOFLAGS }-trimpath -ldflags=-s" \
  ard build main.ard --release \
  --define version="$version" --define commit="$commit" --out "$out/kit"
```

Stamping a real version is what makes this a release build. A release names no
channel and resolves `~/.kit` exactly as it always has; `--define channel=NAME`
is the only thing that binds a build to an isolated
`~/.local/state/kit/NAME/home` (ADR 0047).

## Figure fixtures

The figures in `tui/figure` are likewise the reference for the macOS app's
port in `apps/macos/Sources/KitFigures`
([ADR 0054](../../docs/adrs/0054-draw-data-figures-from-fenced-markdown.md),
grammar in [`docs/figures.md`](../../docs/figures.md)).
`test/figure_fixtures_test.ard` checks it against the shared fixtures in
`testdata/figures`: each `.md` fence and, in the matching `.txt`, the parsed
figure and its drawing, or the reason it isn't drawn. After changing a figure,
or adding a fence, review what it makes and write it:

```sh
KIT_UPDATE_FIGURE_FIXTURES=1 ard test test/figure_fixtures_test.ard
```
