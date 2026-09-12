# VCS footer status

The native TUI's bottom-right footer presents volatile Git status for the
attached session's authoritative working directory:

```text
~/project (main)
~/project (main*)
~/project (detached@a1b2c3d)
~/project
```

`*` means `git status --porcelain=2 --branch` reported at least one staged,
unstaged, conflicted, or untracked entry. Ignored files do not make the
worktree dirty. An unborn branch keeps its branch name; a detached checkout
uses the first seven hexadecimal characters of its object ID. Outside a usable
Git worktree, or when Git cannot be inspected safely, the footer shows only the
working directory.

## Ownership and transport

Git inspection runs in the authoritative daemon against the session's current
cwd. Native and future remote clients consume a renderer-neutral status result;
they do not execute workspace Git commands themselves. The result carries the
session ID and cwd used for the probe so a client can reject a response for a
session or workspace that is no longer attached.

VCS status is transient and is not persisted. Git subprocesses run without a
shell, discard inherited `GIT_*` repository/configuration overrides, disable
repository fsmonitor hooks and optional Git locks, have bounded output, and use
a bounded process-group lifetime. Missing Git, non-repository directories,
malformed output, and probe failures degrade silently to unavailable status.

## Refresh behavior

The native client requests status immediately after initial attachment, session
switches, and cwd changes. It also requests a refresh after tool completion,
agent settlement, and direct bash completion, then polls every five seconds as
a fallback for external changes.

Refresh requests may overlap. Results carry no request sequence or revision.
Any successful response whose session ID and cwd still match the active binding
may update the footer; completion order wins for the same binding and later
refreshes converge on current state. Transport failures retain the last known
status, while a successful response with unavailable status clears the Git
suffix.

Production's TypeScript client also uses filesystem watchers for lower-latency
external worktree, index, and ref changes. That event-driven refresh remains a
roadmap follow-up unless polling latency is accepted as an intentional
difference.

GitHub pull-request metadata through `gh` is a separate feature and is not part
of local VCS status.
