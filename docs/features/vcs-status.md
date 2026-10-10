# VCS footer status

The native clients' bottom-right footer presents volatile Git status for the
attached session's authoritative working directory:

```text
~/project (main)
~/project (main*)
~/project (feature* · PR #123)
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
a bounded process-group lifetime. The probe retains only an opaque digest of
repository-local configuration so remote and default-repository changes fence
in-flight GitHub work without exposing remote URLs or credentials. Missing Git,
non-repository directories, malformed output, and probe failures degrade
silently to unavailable status.

## Live observation and subscriptions

Each loaded session runtime owns one server-side observer. It probes local Git
immediately and every ten seconds, independently of clients and plugin startup.
All native clients and subprocess plugins consume that observation; opening more
clients does not create more Git polling loops. Observation is canceled and joined
when the runtime closes. A cwd change immediately clears the old state, cancels an
in-flight probe, and starts observation of the new workspace.

Native session protocol version 37 requires the VCS stream; update the daemon
and clients together. The external subprocess plugin protocol remains v1.

Native clients subscribe to authenticated `GET /v1/sessions/{sessionID}/vcs/events`
(`streamSessionVCS` in the published contract). The Server-Sent Events stream
follows [ADR 0035](../adrs/0035-describe-server-push-streams-in-the-session-contract.md):
it sends a current snapshot first (possibly unavailable while the initial probe
runs), then deduplicated updates, each as a `vcs.status` record whose single
`data` line is a `SessionVCSStatus` (session ID, cwd, local status and optional
PR). Comment heartbeats arrive every 15 seconds. Records are bounded to 64 KiB;
the stream has no `id` field, cursor, or replay. There are at most 32
subscriptions per session and each retains only its latest snapshot; slow
clients coalesce intermediate states. Failures before the stream opens use the
common error body: 429 `capacity_exceeded` when the subscriber limit is reached
and 503 `unavailable` when observation is unavailable. The one-shot `GET /vcs`
endpoint (`getSessionVCS`) remains available and reads the same observer,
waiting only for initial local Git state, never GitHub.

Both clients use attachment-owned streams rather than polling or activity-triggered
status requests. Transient disconnects reconnect with bounded backoff and a fresh
snapshot; authentication, missing-session and malformed-protocol failures stop
retries. Clients fence callbacks by attachment/workspace identity, including
A→B→A switches, and restart the stream when authoritative cwd metadata changes.
Unavailable Git or absent PR metadata clears its corresponding footer content;
transient transport failures retain the last accepted state until reconnection.

Kit intentionally uses the shared ten-second daemon observer instead of
filesystem watchers for external worktree, index, and ref changes. This keeps
observation portable and bounded while accepting up to one polling interval of
latency for changes made outside Kit.

## GitHub pull requests

Optional PR metadata is a separate built-in Go adapter, not part of the local Git
probe or an external subprocess plugin. The daemon enriches the VCS response with
an optional `pullRequest` containing a positive `number` and validated HTTP(S)
`url`. Both clients show `PR #123` beside the branch. Only that PR label
is underlined and clickable; the cwd, branch, and punctuation remain plain.
Activation opens the URL
on the client machine, never on the daemon. Plugin claims
that hide `kit.footer.location` also hide its PR text and click target.

The server runs a bounded `gh pr view` from the captured repository root for
PR identity, head revision, checks, review decision, and GitHub merge state. It
also runs `gh pr checks --required` for the required-check projection; its
pending and failed exit statuses still carry validated JSON results. Lookups use
an explicit named branch or the PR number discovered for that branch; detached
and unborn heads do not query GitHub. Missing `gh`, absent authentication,
missing PRs, and malformed or failed results silently omit the PR while
preserving local status. No credentials are copied into Kit's provider-auth
store.

Requests never wait for GitHub. A cold cache returns local status immediately and
schedules background work. Completion wakes the observer immediately; PR
identity changes update the footer stream, while rich-only transitions use the
separate model-context stream. Positive and negative results expire after 20
seconds, keyed by cwd, repository root, opaque repository generation, and
branch. Refreshes coalesce per key; stale same-key metadata remains visible
during refresh and a failed refresh clears it. There are at most 128 entries and four
in-flight lookups. Expiry is revisited on the next ten-second observation tick.
Loaded runtimes continue observing while clients are detached; unloading the
runtime stops that work.
Each lookup sequence has a five-second deadline; each command has a 64 KiB
stdout limit, runs without a shell or interactive prompts, and is canceled and
joined on daemon shutdown. URLs are
bounded to 4096 UTF-8 bytes and restricted to absolute HTTP(S) without credentials,
whitespace, backslashes, or control/format text. Check and reviewer identifiers
are bounded and reject control or format text; check and review collections are
bounded and sorted into canonical order.

## Model context

Rich pull-request status is a renderer-neutral observation separate from the
footer projection. It records PR and head identity, canonical check states,
required-check satisfaction, GitHub's review decision, required-approval
satisfaction, mergeability, and conservative ready, blocked, or unknown merge
readiness. The type is not Droid-specific so future clients or plugins can gain
an explicit projection without scraping model-facing text.

A loaded session subscribes to distinct rich-status transitions. Equal
consecutive canonical states are deduplicated; a later return to an earlier
state remains a new transition. Each transition is delivered through
`Droid.Inform` as a provenance-labeled `pull_request_status` boundary from
`github`, with structured details and a concise check/review/readiness summary.
Workspace, repository, branch, PR number, and head-revision identity are
revalidated immediately before delivery so queued stale results cannot cross a
workspace or revision change. Delivery retries reuse the transition's boundary
ID, while separate transitions receive separate IDs.

Information boundaries never start an idle turn. During active work they become
available at the next safe model boundary; while idle they remain pending until
a later user turn or an explicit harness reaction. Rich-only changes do not
produce duplicate native VCS records or `kit/events/git.changed` events. The
observer and Droid both impose hard bounds; a rich-status subscription that
cannot retain every transition terminates explicitly instead of silently
coalescing or replacing an observed state.

Subprocess plugin initialization and `kit/events/git.changed` now include an
optional nullable `git.pullRequest` with the same number and URL. Native v2 sends
that member explicitly, using null while PR information is loading or unavailable.
PR-only changes (including clearing) trigger plugin notifications too. The public
plugin schema accepts the additive field; plugins may ignore it, while strict
consumers must use the updated schema. This does not add plugin subscriptions or
alter the `git: null` representation for unavailable local Git metadata.
