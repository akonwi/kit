# Code Review: Commit Targets

Design for extending `/code-review` beyond the working tree so a previous
commit's diff can be reviewed and commented on. The primary use case is
commenting on the most recent commit right after the agent makes it.

This is deliberately **not** a history explorer: one commit at a time, no
graph, no ranges, no branches. Status: designed, not yet implemented.

## Review targets

The review screen gains a `target` — a piece of state read on mount and
swappable in place. One review screen, multiple targets:

```ts
type ReviewTarget =
	| { kind: "working" } // today's behavior, default
	| { kind: "commit"; sha: string }; // commit vs its parent
```

- `working` — `git diff HEAD` + synthesized untracked patches (unchanged).
- `commit` — `git diff <sha>^ <sha>` (root commit diffs against the empty
  tree). No untracked set.

Everything downstream of `loadReviewFiles` already consumes `ReviewFile[]`
and needs no changes, with one exception: content reads. The read-only
file view, skipped-section line sourcing, and the file tree's "all files"
listing read the live filesystem today, which is only correct for the
working tree. In commit mode these must read the commit snapshot via
`git show <sha>:<path>` (and `git ls-tree`, or restrict the tree to
"changes" mode as an MVP cut).

## Switching targets

Two keybindings, added to the review hint bar:

- `t` — cycle target: `working ↔ HEAD`. Single keystroke for the dominant
  workflow ("review what the agent just committed, flip back").
- `T` — open the commit picker.

### Commit picker

Tier 1 picker (no border, `pickerBg`, floats over the review screen, no
backdrop dim — the user is orienting between two things):

- Rows: `shortSha` (`metaText`) · subject (`textPrimary`, truncated) ·
  relative time (`textMuted`, right-aligned).
- **Working tree pinned as the first row** with a summary
  (`3 files · 42+/8−`). The picker is the single source of truth for
  target selection and teaches that the working tree is just another
  target.
- Filterable by subject + sha.
- Hard cap: last 20 commits, enforced in the picker with no "show more"
  affordance. Older history is git tooling's job. If ever expanded, add a
  free-text sha input before pagination.

## Communicating the current target

- **Screen header crumb** (left slot): `Code review › <target>` —
  `Code review` in `textMuted`, `CHEVRON_RIGHT` separator, then either
  `working tree` in `textPrimary` or `shortSha` (`metaText`) + subject
  (`textPrimary`).
- **Right slot**: target-appropriate stats. Working tree:
  `3 files · 42+/8−`. Commit: `authored 8m ago · 3 files · 42+/8−`.
- Drop the per-file source label in the diff pane header when it is
  redundant with the screen target (keep it only when a file's source
  diverges, e.g. untracked files in working-tree mode).
- File tree: no banner; a one-line footer in `textPlaceholder` anchors the
  scope (`HEAD~1 → HEAD` or `working tree`).

### Clean-tree auto-targeting

Opening review with a clean working tree auto-targets `HEAD` instead of
showing a dead-end empty state — that *is* the primary use case, reached
with zero keystrokes. Announce with an inline one-line banner in the diff
area (`textSecondary` on `nearBlack`, no border, dismissed by any
navigation key):

```
Working tree is clean — showing last commit (a1b2c3d).  t swap · T pick commit
```

If `HEAD` also has an empty diff, fall through to the standard empty state
with a hint to press `T`.

### Dirty-tree HEAD ambiguity

When cycling to `HEAD` while the working tree is dirty, show a transient
(~1.5s) strip in the diff area: `Showing HEAD (working tree has changes)`.
One consistent inline affordance for "your context changed" — never a
modal.

## Comments and drafts

- **Per-target draft maps**, keyed `working` / `commit:<sha>`, kept in
  memory and preserved across target switches. Note keys gain the target
  prefix (`<target>::path::side:start-end`) so drafts never collide.
- No discard prompts on switch — the indicators carry the state:
  - rose `CIRCLE_FILLED` next to picker rows that have draft notes
  - drafted-note count in the header crumb
    (`Code review › working tree · 2 notes drafted`)
- Commit-scoped annotation boxes get a subtle `a1b2c3d ›` prefix in
  `metaText`. Working-tree annotations stay unbadged (badges everywhere =
  badges nowhere). Comments keep the rose attachment accent in both modes.
- **Submit is scoped to the current target only.** Never batch across
  targets. If drafts exist on other targets at submit time, show a
  transient inline notice
  (`Submitting working tree notes only. 2 notes on a1b2c3d not included.`).
- After submit, stay on the submitted target with a cleared draft — no
  auto-switching.
- Transcript attachment chip labels the scope:
  `Review · a1b2c3d · 4 notes` vs `Review · working tree · 4 notes`.

## Submission payload

`CodeReviewSubmission` gains optional commit metadata:

```ts
commit?: { sha: string; parentSha: string; subject: string };
```

The prompt-text rendering states the target ("comments on commit a1b2c3d
(fix: …)") so the agent knows the line numbers refer to that commit's diff
and can `git show sha^ sha` for exact context.

## Amend/rebase staleness

The real bug hazard: comment line numbers only make sense against the
drafted sha. Defense:

- Store the target `sha` **and its `tree` hash** with commit drafts.
- On submit, re-resolve. If the sha no longer exists or the tree changed,
  **block submission** with a Tier 2 dialog:
  `Commit a1b2c3d was amended. Your 3 notes reference the old diff.` with
  actions: Discard notes / Keep drafting on new commit (remap by
  file+content hash, drop unremappable) / Cancel.
- Never silently rebind line numbers — silent rebinding leads to the agent
  editing the wrong lines.

## MVP slice

1. `ReviewTarget` plumbing in `model.ts`: commit diff loading +
   `git show`-backed content reads in commit mode.
2. `t`/`T` bindings + Tier 1 commit picker with pinned working-tree row.
3. Header crumb + clean-tree auto-targeting of `HEAD`.
4. Per-target draft maps, target-scoped submit, commit metadata in the
   submission and prompt text.
5. Amend detection at submit time.

## Non-goals

- Commit graphs, ranges (`sha..sha`), branch selection, author columns,
  pagination beyond 20 commits.
- Reviewing merge commits against multiple parents (first parent only).
- Persisting drafts across app restarts (in-memory per session, matching
  current working-tree behavior).
