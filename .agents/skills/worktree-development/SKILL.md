---
name: worktree-development
description: Safely run, test, and debug Kit from a development worktree without replacing another worktree's daemon or migrating production state. Use before launching a development Kit binary, working with a local daemon, testing persistence/migrations, or connecting clients built from different worktrees.
---

# Worktree development

Kit's default `~/.kit` contains configuration **and** mutable runtime state: `kit.db`, credentials, attachments, daemon discovery, locks, and logs. `KIT_HOME` selects the entire home, including the daemon. A normal `kit` invocation can discover, start, or replace a daemon; `kit server start` is not a read-only probe. A newer-protocol development client can replace an older daemon, while equal-protocol development builds may still differ. Treat the default home as production unless the user explicitly says otherwise.

## Preflight before launching Kit

1. Identify the worktree (`git rev-parse --show-toplevel`), intended executable (`kit version` does **not** start the daemon), and effective `KIT_HOME`. An unset `KIT_HOME` means `~/.kit`. Check the actual launch environment, not just the current shell; a script, `go run`, or GUI launch may differ.
2. Determine the intended server: existing production daemon, isolated development daemon, or no daemon (build/test only). Check status through a known installed binary's `kit server status` or inspect the non-secret `<home>/run/server.json`; status is read-only, but its CLI output does not show protocol or commit. Never print `<home>/run/server.token` or credential files.
3. Compare protocol, build identity, and intended database state before connecting a development client. The registry contains `protocolVersion`, `kitVersion`, and `commit`; `dev`/`unknown` build metadata is **not** evidence of compatibility. If compatibility is uncertain, stop and explain rather than trying a normal invocation to find out.
4. If a command could start/replace a daemon or write/migrate a database, choose the home explicitly **before** launching it. Never use an implicit default for a risky command.

## Choose the narrowest mode

- **Build, vet, unit tests:** No live daemon is needed. Run tests with an explicit disposable `KIT_HOME`, for example `KIT_HOME="$(mktemp -d)" go test ./...`. Preserve the isolated directory when investigating failures.
- **Client UI against existing sessions:** Keep the existing server and home. Connect only when the client/server protocol and behavior are known to be compatible. Do not restart the daemon to make a client work; if incompatible, use a matching client or ask which server the user wants to keep.
- **Development server, migrations, persistence, or unknown compatibility:** Use a disposable or stable worktree-specific `KIT_HOME` outside `~/.kit`. It owns its own daemon, database, attachments, and logs. This mode intentionally does not share live sessions with production.
- **Explicitly authorized production-home changes:** State which executable, daemon, and database will be affected. Preserve a recoverable backup before a migration and coordinate shutdown/snapshotting with the owning daemon; never copy a live SQLite database by copying `kit.db` alone (WAL may hold committed data).

## Guardrails

- `kit server stop`, `kit server restart`, ordinary `kit`, `kit web`, `kit -p`, and `kit server start` are not harmless ways to check compatibility. Do not run them against a shared home without confirming the intended effect.
- Never point two independently evolving daemon versions at the same `KIT_HOME`/database or symlink a development home to production runtime files. Do not copy or link managed auth files as a shortcut; reauthentication or explicit credential handling is preferable.
- Do not infer compatibility from protocol number alone: same-protocol worktrees can have unversioned behavioral differences. Conversely, don't force all ordinary development into isolated homes when an existing matching server safely supports the task.
- If you encounter an unexpected daemon, protocol mismatch, schema change, or migration failure, stop. Report the worktree, binary identity, selected home, and non-secret daemon identity before proposing an action; never auto-restart or retry against `~/.kit`.

When reporting a run or test, include the home used, which daemon (if any) was contacted, and whether production state was untouched.
