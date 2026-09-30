# Ard entry point

`apps/cli` is an Ard project that builds the `kit` executable. `main.ard` is
the process entry point: it applies build metadata, turns interrupt and
termination signals into command cancellation and exit codes, and supplies the
terminal client to the command line in `ffi/cli`, which owns command parsing,
headless commands, the daemon role, and interactive setup. Go calls whose
result shapes Ard cannot import use small bridges such as `ffi/contextbridge`.

This entry point is being developed alongside `cmd/kit`. Until `cmd/kit` is
removed, `ffi/cli` is a copy of `internal/cli`; mirror command-line changes in
both. The terminal client is written in Ard with Cooper (`tui.ard`, `tui/`) and
is working toward parity with the vaxis/ui client that `cmd/kit` ships; see the
Cooper client parity section of `backlog/tui.md`.

The directory is a separate Go module (`github.com/akonwi/kit/apps/cli`) that
replaces `github.com/akonwi/kit` with the repository root, so root
`go build ./...` and `go test ./...` do not include it. Building requires Ard
0.42.0 or newer and Go 1.27 or newer.

## Develop

Run commands from this directory. Use an isolated `KIT_HOME`; see
`.agents/skills/worktree-development/SKILL.md` before starting a daemon.

```sh
ard format --check .
ard check main.ard
ard test
gofmt -l ffi
go vet ./...
KIT_HOME="$(mktemp -d)" go test ./...
ard build main.ard --out /tmp/kit-dev/kit
KIT_HOME=/tmp/kit-dev/home /tmp/kit-dev/kit version
```

`ard build` regenerates `ard-out/`, which is ignored and not formatted.

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
