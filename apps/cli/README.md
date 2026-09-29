# Ard entry point

`apps/cli` is an Ard project that builds the `kit` executable. `main.ard` is
the process entry point; it supplies build metadata and the terminal client to
the command line in `ffi/cli`, which owns command parsing, headless commands,
the daemon role, and interactive setup.

This entry point is being developed alongside `cmd/kit`. Until `cmd/kit` is
removed, `ffi/cli` is a copy of `internal/cli`; mirror command-line changes in
both. The terminal client is currently the existing vaxis/ui client
(`ffi/cli/vaxis_tui.go`); a Cooper client will replace it.

The directory is a separate Go module (`github.com/akonwi/kit/apps/cli`) that
replaces `github.com/akonwi/kit` with the repository root, so root
`go build ./...` and `go test ./...` do not include it. Building requires Ard
0.42.0 or newer and Go 1.27 or newer.

## Develop

Run commands from this directory. Use an isolated `KIT_HOME`; see
`.agents/skills/worktree-development/SKILL.md` before starting a daemon.

```sh
ard format --check main.ard
ard check main.ard
gofmt -l ffi
go vet ./...
KIT_HOME="$(mktemp -d)" go test ./...
ard build main.ard --out /tmp/kit-dev/kit
KIT_HOME=/tmp/kit-dev/home /tmp/kit-dev/kit version
```

`ard build` regenerates `ard-out/`, which is ignored and not formatted.

## Release build

Release builds must define the version and commit; `--release` rejects
defaults. Build values replace `-ldflags -X` version stamping.

```sh
CGO_ENABLED=1 GOFLAGS=-trimpath ard build main.ard --release \
  --define version="$version" --define commit="$commit" --out "$out/kit"
```
