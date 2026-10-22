---
name: go-concurrency
description: Write, review, and debug concurrent Go in Kit (internal/, droids/, apps/cli/ffi). Use when adding or changing goroutines, mutexes, channels, WaitGroups, subscriptions, context cancellation, Close/Shutdown paths, or timeouts, when reviewing such changes, or when triaging -race reports, flaky timing tests, goroutine leaks, hangs, or deadlocks.
---

# Go concurrency in Kit

AGENTS.md "Go conventions" and "Validation" are the baseline: thread context,
own goroutines explicitly, bound queues, and run `go test -race` for
concurrency-sensitive changes. CI runs `go vet` and `go test -race -count=1
./...` for the root module on every PR. It does not run `-race` on the
`apps/cli` module, so run that yourself when changing concurrent code there.
This skill covers Kit's own idioms and the failures those tools miss.

## What the toolchain already handles

- Per-iteration loop variables (Go 1.22+): don't add `v := v` before
  launching a goroutine.
- `go vet` copylocks rejects copied mutexes and WaitGroups.
- `-race` reports unsynchronized memory access, but only on interleavings a
  test actually executes. It cannot see logical races, deadlocks, leaks,
  lock-order inversions, or ignored deadlines. Most concurrency defects found
  in Kit so far have been of those kinds.
- `sync.WaitGroup.Go`, `context.AfterFunc`, `context.WithCancelCause`,
  `testing/synctest`, and `golang.org/x/sync` are all available.

## Kit idioms

Match these when writing new concurrent code; deviating needs a reason.

1. **One owner goroutine, coalesced wakes.** Mutators update desired state
   under the lock and signal a 1-slot channel with a non-blocking send. The
   owner then reads current state; it doesn't consume queued commands. See
   `Host.signal` and `Host.Start` in `internal/plugin/host.go` and
   `Observer.ChangeCWD` in `internal/vcs/observer.go`.
2. **Fence unlocked I/O with an epoch.** Snapshot state and an epoch under the
   lock, do the git/process/network work without it, then re-lock and drop the
   result if the epoch, cwd, or closed state changed. See `Host.refreshGit` in
   `internal/plugin/git_events.go`. `Observer.refreshGit` fences the same way,
   but don't copy its failure handling (see "Known exceptions"). A value derived
   from a read that can race with a newer write must only move forward: use a
   compare-and-swap rather than storing a value read earlier.
3. **Idempotent close that never abandons cleanup.** The first caller flips
   `closed` under the lock, cancels, and starts teardown owned by a worker.
   Every caller waits on a `done` channel or its own context, and a caller's
   cancellation ends only its own wait. See `Host.Close`, `Observer.Close`,
   `Process.Stop` in `internal/childproc/process.go`, and `Manager.Shutdown` /
   `finishShutdown` in `internal/session/manager.go`.
4. **WaitGroup admission.** A positive `Add` is ordered before the group's
   `Wait` when one of these holds:
   - It runs under the lock that checks `closed`, and shutdown sets `closed`
     before waiting (`Manager.beginOperation`, `beginAdmission`,
     `wakeSubagentMailbox`).
   - The caller already holds a count in the same group
     (`enqueueSubagentChanged` runs inside `beginOperation`).
   - The caller is counted in a group that `finishShutdown` waits on earlier
     (`scheduleAutoName` runs inside a counted run or load, and `cleanups` is
     waited last; `StartBash` runs inside `beginAdmission`, and `admissions`
     is waited before `bashRuns`).

   When adding a group or an `Add` site, check it against `finishShutdown`'s
   wait order.
5. **Broadcast by closing and replacing a channel, not per-subscriber
   queues.** The session event log appends to a bounded, replayable buffer,
   closes `changed`, and installs a new channel. Readers page by cursor and get
   `ResyncRequired` when they fall behind (`eventLog.signalChangedLocked` and
   `eventLog.wait` in `internal/session/events.go`; ADR 0011).
   - To avoid lost wakeups, capture the current `changed` channel before, or
     in the same critical section as, the read of the state you'll wait on.
     Then select on the captured channel (`eventLog.wait`;
     `githubpr.Cache.Changes` is sampled before `Get`). Fetching the channel
     after checking state can miss the close.
   - Latest-state subscriptions keep a 1-slot channel and replace its value
     (`Observer.publishLocked`).
   - Transition streams that must not drop values fail the subscriber
     explicitly on overflow (`PullRequestStatusSubscription`).
6. **Clone at the goroutine boundary.** Data stored from, or returned to,
   another goroutine is copied: `cloneSnapshot`, `cloneSessionContext`,
   `Host.Instances`. Locking the `append` doesn't protect a slice or map that
   was handed out before.
7. **Callbacks across ownership boundaries don't block.**
   `EventSink.SubagentChanged` must not do network, disk, or other blocking
   I/O. The session implementation stores a pending event and returns, and a
   single drainer performs the append (`enqueueSubagentChanged`). This is why
   the supervisor may call `emitChanged` while holding `s.mu`. Don't call into
   another component while holding a lock that its callbacks can need.
8. **Results that can be abandoned use a 1-slot buffer.** When a caller may
   stop waiting, the worker sends into `make(chan T, 1)` so it never blocks
   forever (`closeWithinDeadline` in `internal/mcpruntime/close.go`, `exited`
   in `childproc`). When a worker and a timeout path can both write a
   future-style result, exactly one of them may win. `RPCEndpoint.callAdmitted`
   re-checks ownership of the pending entry in its `ctx.Done()` arm, and if it
   lost, it waits for the responder's result.
9. **Bounded fan-out.** Concurrency limits are buffered-channel semaphores
   (`Manager.mailboxSlots`, `bashSlots`) or an errgroup-style batch with `Add`
   before `go` and writes to disjoint result indexes
   (`sdkRuntime.executeToolBatch` in `droids/sdk_loop.go`).

## Lock orders

Acquire nested locks in these orders:

- **Sessions:** follow the order documented on `Manager` in
  `internal/session/manager.go`: runtime `transitionMu` → `admissionMu` →
  runtime `mu` → workspace `mutationMu` → event-log `mu` → `bashMu` →
  `Manager.mu`.
- **Subagents:** `configurationLock` → owner `transitionMu` →
  `conversationLock` → `Supervisor.mu` → `worker.mu`. Through idiom 7,
  `emitChanged` → `SubagentChanged` → `beginOperation` adds the
  cross-component pair `Supervisor.mu` → session `Manager.mu`. So never call
  the supervisor while holding `Manager.mu`. `worker.mu` guards only short
  state flips: take a runtime lease under it (`acquireRuntimeLocked`) and call
  the child runtime without it. The worker closes the runtime only after
  `beginClose` reports that every lease has been released.
- **Plugins:** `Host.mu` → `Instance.mu` → `RPCEndpoint.mu`. The one reversal
  is a known exception: `pending.validate` runs under `RPCEndpoint.mu`, and
  the initialization validator takes `Instance.mu` (#248). Until that's fixed,
  validators must not take `Host.mu`, and nothing may enqueue on the endpoint
  while holding `Instance.mu` during initialization.

## Known exceptions

These are tracked issues; don't treat them as precedent:

- `Observer.refreshGit` publishes `Git: nil` on any probe error, and
  `vcs.Probe` returns `(nil, nil)` on its own timeout. That turns a slow `git`
  into "not a repository" (#235).
- `WatchStreamIdle` measures idle time through `UnixNano`, losing the
  monotonic clock. Plugin response validation runs under `RPCEndpoint.mu` and
  takes `Instance.mu`, reversing the plugin lock order (#248).

## Review checklist

Check each item against the change, beyond what vet and `-race` report.

- **Locks held across blocking work.** No mutex is held across process waits,
  HTTP, SQLite, `droid.Shutdown`, plugin RPC, or calls that can reach another
  component's lock. Take a reference under the lock, release the lock, then do
  the work.
- **Lock order.** New nested locking follows the orders above. If it
  introduces a new pair, add the pair to this skill.
- **Deadlines are honored end to end.** A `Shutdown(ctx)` or `Close(ctx)`
  does no unbounded synchronous work, including acquiring a lock, before it
  selects on `ctx.Done()`. Avoid new `context.Background()` calls on paths
  that shutdown waits for.
- **Transient vs. terminal failure.** Don't publish a timeout or transient
  error as an authoritative state such as "not a repository" or "no PR". Keep
  the last good value or retry, and make probes return results that tell the
  two cases apart.
- **Monotonic state.** Generations, epochs, and sequence numbers that are read
  from storage and written to shared state can't regress under concurrent
  writers.
- **Admission and wakeups.** Every new `wg.Add`, send, and close is ordered
  against shutdown (idiom 4). Every wait on a broadcast channel captured the
  channel with the state it guards (idiom 5). Any send that may outlive its
  receiver is buffered or selects against cancellation.
- **Shared values.** Maps, slices, and pointers that cross goroutines are
  cloned, or treated as immutable after publication. A goroutine closure
  doesn't assign to an outer `err` or named result that the enclosing function
  also uses.
- **Lifetime.** Each new goroutine is joined (`WaitGroup` or a done channel) or
  stops on a context that shutdown cancels.
- **Elapsed time.** Durations and deadlines compare `time.Time` values from
  `time.Now()`. `UnixNano()`, `Round(0)`, and JSON or database round-trips
  strip the monotonic reading, so they are only for wall-clock display and
  persistence.

## Testing and triage

Run Go tests with a disposable home (see the `worktree-development` skill):

```sh
export KIT_HOME="$(mktemp -d)"
go test -race -count=20 -run '^TestName$' ./internal/pkg/   # reproduce one test
go test -race -count=3 ./internal/pkg/ ./internal/other/      # add concurrent package load
go test -race -count=1 ./...                                  # CI's load; flakes often appear only here
go test -shuffle=on -count=3 ./internal/pkg/                  # order dependence
```

`-shuffle=on` prints its seed. Pass `-shuffle=<seed>` to replay the same
order.

Classify a failure before changing code:

- **Data race:** a `WARNING: DATA RACE` report with two stacks. Find the
  missing happens-before edge.
- **Logical race:** wrong state with no race report, such as a transient
  failure published as terminal state or a stale value overwriting a newer
  one. Find the unfenced check-then-act.
- **Deadline too tight for the environment:** passes alone, fails under
  `-race`, on macOS CI, or under full-suite load. It's common around
  subprocesses (`git`, fake `gh` scripts, plugin children), because `-race`
  slows execution several times and process start-up varies widely. Prefer a
  synchronization point. Otherwise size the deadline for the slowest CI
  runner, not from a local run.
- **Hang or deadlock:** run with `go test -timeout 60s ...`. The timeout panic
  dumps every goroutine's stack. Or send `kill -QUIT` to the test binary. Look
  for lock waits and blocked sends that point at each other.

Test-writing rules:

- Synchronize on observable completion: a returned channel, `Next(ctx)` on a
  subscription, or a polling helper with a deadline (`eventually` in
  `internal/server`, `eventuallyHost` in `internal/plugin`). Don't use a bare
  `time.Sleep` as the synchronization.
- Inside polling conditions, use checked type assertions and report the
  observed value. In #235, an unchecked `params["git"].(map[string]any)` turned
  an unexpected intermediate event into a panic that hid the cause.
- Prefer injected seams on the owning struct, set before `Start` (e.g.
  `Observer.probe`), over package-level variables. When a test must replace a
  package variable, that test and every test that reads it run sequentially,
  with a comment saying so (`internal/serveridentity/identity_test.go`).
- `testing/synctest` makes timer- and ticker-driven logic deterministic, but a
  goroutine counts as durably blocked only when it waits on:
  - channels created inside the bubble
  - `sync.Cond`
  - `time.Sleep`
  - `WaitGroup.Wait`, when every `Add` happened inside the bubble

  Mutex waits, pipes, SQLite, subprocesses, and network I/O don't count. So
  synctest doesn't fit tests that use plugin RPC over `os.Pipe`, child
  processes, `git`/`gh`, or storage.
- When a flake is seen once and doesn't reproduce, file or update a `core`
  issue with the full output, command, platform, and load conditions. Don't
  just retry silently.
