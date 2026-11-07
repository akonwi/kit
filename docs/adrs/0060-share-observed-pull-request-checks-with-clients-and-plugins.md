# 0060: Share observed pull request checks with clients and plugins

## Status

Accepted. Extends the VCS observation described in
[`docs/features/vcs-status.md`](../features/vcs-status.md) and the stream
rules of [ADR 0035](0035-describe-server-push-streams-in-the-session-contract.md).

## Context

The server observes the pull request of each loaded session's named branch,
including its CI checks and the repository's required-check state, and tells
the agent about changes. Clients and plugins receive only the pull request's
number and URL. A plugin that shows CI status has to query GitHub again on its
own schedule, and in the macOS app its footer item appears far from the pull
request it describes.

Session protocol 50 is released, and its clients decode responses strictly:
generated Swift types and the Go client reject unknown members. A new member
in `GitHubPullRequest` would make the VCS stream of every released client fail,
which hides the branch and pull request, unless the protocol is bumped, and
the server accepts only its own protocol version.

## Decision

### One bounded summary

The server summarizes a pull request's checks once, in `internal/pullrequest`,
and projects the summary for each consumer:

- `state`: `failing` when any check failed or was canceled, otherwise
  `pending` when any is running, otherwise `passing` when any passed,
  otherwise `none`.
- `required`: whether the repository's required checks are `satisfied`,
  `unsatisfied`, `pending`, or `unknown`.
- `counts`: every observed check by state (`passed`, `failed`, `pending`,
  `skipped`, `canceled`, `unknown`).
- `items`: at most 50 checks, failing, canceled, running, unknown, passed, and
  skipped in that order, each with a name, an optional workflow, a state, and
  whether it is required. `omitted` counts the checks not listed.

Names and workflows are at most 256 UTF-8 bytes without control or format
characters. The session contract validates the summary's consistency, and
clients validate it again at their boundary. Reviews and merge readiness
remain model context.

### Clients read a separate stream

`streamSessionPullRequest` (`GET /v1/sessions/{sessionID}/pull-request/events`)
pushes `pullRequest.status` records: the session ID, cwd, named branch, and the
branch's pull request with its number, URL, state, draft flag, and checks. It
sends the current state first, then deduplicated latest-only changes, with
heartbeats, a 64 KiB record bound, at most 32 subscriptions per session, and
no cursor or replay.

The operation is additive, so the session protocol stays 50. Released clients
never request it, and their VCS records don't change. A client that requests
it from a server released before it receives an unrouted 404, which the Go
client reports as an `UnsupportedError` and the Swift client as
`PullRequestStreamNotServed`; clients then show no check status and don't
retry.

The stream is separate from the VCS stream rather than a variant of it, so a
client that doesn't show checks keeps one connection per session. The macOS
app opens it only while the VCS stream reports a pull request.

### Plugins read their Git context

The plugin Git context's `pullRequest` gains an additive `checks` member with
the same summary, in initialization and in `kit/events/git.changed`. Plugin
protocol v1 allows additive context members, as it did for `pullRequest`. A
change in a check's state sends `git.changed`, and the observer wakes the
plugin host without waiting for its next poll.

### Observation is unchanged

Clients and plugins read the session's existing observation, which runs while
the session is loaded because the agent consumes it. Their reads add no GitHub
requests. A branch is looked up once every 20 seconds or so, with two `gh`
calls, shared by every session that observes it.

### The macOS app shows checks beside the pull request

A checks chip beside the PR chip shows a state glyph and passed/total, skipped
checks excluded, in the state's color. It is hidden when there are no checks.
Clicking it lists the checks in a popover; clicking the PR chip still opens the
pull request.

## Consequences

- A plugin can show CI status from the context Kit already sends, without
  calling GitHub.
- Clients need a server released with the stream to show checks. Older servers
  keep working with newer clients; they show no checks.
- Each consumer that wants checks holds one more stream per session that has a
  pull request.
- Strict plugin schema validators must use the updated schema.
