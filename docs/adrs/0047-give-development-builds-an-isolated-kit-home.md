# 0047: Give development builds an isolated Kit home

## Status

Accepted

## Context

`~/.kit` is production state: the database, credentials, attachments, settings,
daemon discovery, locks, and logs. AGENTS.md and the worktree-development skill
require every development invocation to set `KIT_HOME` explicitly, but that rule
depends on a person remembering it every time — in a test run, a manual build, a
shell from yesterday, or a new CI job.

A plain `kit` invocation can discover, start, or replace the daemon that owns
that state. A development build that forgets `KIT_HOME` therefore does not fail
loudly; it reads and migrates a live database it was never meant to see, and two
independently evolving builds can end up sharing one database. The failure is
silent and unrecoverable.

Isolation must therefore not depend on discipline. The binary should know whether
it is a development build and, when it is, choose state that cannot collide with
production — while leaving every explicit choice to the operator.

## Decision

A build is bound to a channel, and the channel is the whole decision.

`internal/version` carries `Channel`. `apphome.Resolve` keeps `KIT_HOME` and an
explicit home as unconditional overrides — they are how tests, containers, and
multi-home machines choose state, and their precedence never changes. A build
with **no channel** that receives neither resolves `~/.kit`, exactly as before. A
build bound to a channel receives an isolated home instead:

```
~/.local/state/kit/<channel>/home
```

The layout is identical on macOS and Linux; Kit's supported systems are
single-user developer machines, and one path keeps the home predictable when
moving between them. A channel is the namespace, so a worktree that needs its own
state names one at build time (`--define channel=NAME`); `dev` is the usual name
for everyday isolated development. A build that names no channel is unbound.
Channels are validated as a single path segment so a channel can never escape
the state directory.

The resolved home is a derived path, never created by resolution: `Resolve`
selects, and a directory appears only when a command needs it, exactly as `~/.kit`
does today.

Because a channel-bound home is not the familiar one, it is always visible.
`kit version` and `kit server status` report the home such a build resolves, so
nobody has to guess which state a binary owns.

The macOS client follows the same rule. `SharedSettingsStore.home` and daemon
discovery resolve through one `DevHomeResolver`, whose precedence is `KIT_HOME`,
then a development bundle's home, then `~/.kit`. A launched GUI app does not
inherit its launching shell's environment, so a development bundle is bound to a
channel at build time (`KIT_MACOS_CHANNEL=NAME build_and_run.sh`), which writes
the channel's home into the bundle's `Info.plist`. A bundle built without a
channel carries no home and resolves `~/.kit`, like an unbound CLI build; a
release bundle never names one. Development bundles keep their own bundle
identifier, so development preferences and window restoration also stay
separate.

## Consequences

- A build bound to a channel cannot read or migrate production state by
  forgetting one variable: `~/.kit` is not its default at all.
- Two builds that need separate state take different channels instead of racing
  for `~/.kit`.
- Credentials and themes saved in a channel's home persist across rebuilds, so
  isolation does not mean reauthenticating every time.
- Naming a channel is the only act that opts a build into isolated state, and it
  is a build-time choice made deliberately. A production release names nothing
  and behaves exactly as it always has: its build command is unchanged, and the
  release smoke test asserts that a published binary reports no channel home.
- An unbound build still resolves `~/.kit`, so an ordinary `go test ./...` or a
  locally built binary without a channel is unchanged.
- Tests and CI keep explicit `KIT_HOME`. Those call sites are unaffected; only
  the default a *bound* build gets changes.
- Consoles, containers, and service managers that run a development build in a
  different `HOME` get a different isolated home, which is the intended answer.
