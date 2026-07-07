# Code Review: Commit and Branch Targets

`/code-review` reviews more than the working tree: it can show the diff of
a previous commit or a branch's total diff, with the same commenting and
submission flow. The primary use case is commenting on the most recent
commit right after the agent makes it.

This is deliberately **not** a history explorer: one commit at a time, no
arbitrary ranges, no graphs.

## Review targets

The review screen holds a `target` — swappable in place, one review
screen for all targets:

```ts
type ReviewTarget =
	| { kind: "working" } // default
	| { kind: "commit"; sha: string } // commit vs its parent
	| { kind: "branch"; base: string; head: string; mergeBase: string };
```

- `working` — `git diff HEAD` + synthesized untracked patches.
- `commit` — `git diff <sha>^ <sha>`; a root commit diffs against the
  empty tree. No untracked set.
- `branch` — the branch's total diff, `git diff <mergeBase> <head>`. The
  base defaults to the **local** default branch (`main`/`master`, or the
  local branch `origin/HEAD` points at), falling back to the
  remote-tracking ref only when no local branch exists. `head` is pinned
  at selection time so the diff stays deterministic while the review is
  open.

Shas are stored full-length everywhere (targets, note keys, submission
payload); abbreviations are display-only, so sha ambiguity in large repos
can't break staleness checks or the agent's `git diff`.

For committed targets, file content (read-only sections, skipped-section
sourcing) is read from the revision snapshots via `git show <rev>:<path>`
— never the filesystem, which reflects the working tree. For the same
reason the file tree and the file finder are restricted to the diff's own
files, the changes/all-files toggle is disabled, and the working-tree
read-only file viewer never opens.

Committed-diff loads degrade gracefully: if a pinned sha stops resolving
(pruned, ambiguous), the target renders as an empty diff instead of
crashing the overlay.

## Switching targets

Two keybindings on the review hint bar:

- `g` — cycle target: `working ↔ HEAD`. Single keystroke for the dominant
  workflow ("review what the agent just committed, flip back").
- `shift+g` — open the target picker.

### Target picker

Tier 1 picker floating over the review screen, filterable by subject +
sha. Git state (commits, branches, merge bases) is snapshotted once when
the picker opens; only draft-count decoration is reactive.

- **Working tree pinned as the first row** — the picker is the single
  source of truth for target selection.
- **Branch diff pinned second** (`branch <name> vs <base>`) when the
  current branch has a resolvable base that isn't the current commit.
- **Custom base**: a `branch <name> vs …` row swaps the picker to a list
  of local branches (sorted by last-commit recency, current branch
  excluded); selecting one becomes the branch-diff base. Escape closes
  the picker rather than stepping back a level.
- Then the last 20 commits: `shortSha  subject` with relative time.
  The cap is enforced with no "show more" affordance — older history is
  git tooling's job.
- Rows for targets holding draft notes carry a `CIRCLE_FILLED` prefix.

While any picker is open, tree navigation and the other review keymap
layers are suppressed.

## Communicating the current target

- **Screen header crumb** (left slot): `Code review › <target>` — either
  `working tree`, or `shortSha` (`metaText`) + subject (`textPrimary`;
  `<branch> vs <base>` for branch targets).
- **Right slot**: file count and drafted-note count; committed targets
  prepend `committed <relative time>`.
- The per-file source label in the diff pane header only appears when a
  file's source diverges from the screen target (e.g. untracked files in
  working-tree mode).
- Target-change notices render as a transient full-width strip above the
  panes.

### Clean working tree

Opening review with a clean working tree keeps the working-tree target
and shows the normal project explorer: the file tree falls back to "all
files" mode and files open in the read-only viewer. Commit and branch
targets remain one keystroke away (`g` / `shift+g`).

### Dirty-tree HEAD ambiguity

Cycling to `HEAD` while the working tree is dirty shows a short-lived
strip — `Showing HEAD (working tree has changes)` — one consistent inline
affordance for "your context changed", never a modal.

## Comments and drafts

- **Drafts are per-target**: note maps are stashed and restored on target
  switch, so switching never loses or mixes drafts. File-note keys are
  revision-scoped (`commit:<sha>:…`, `branch:<base>:<head>:…`); range
  notes are isolated by the per-target map swap.
- No discard prompts on switch — the picker's `CIRCLE_FILLED` indicators
  and the header note count carry the state.
- **Submit is scoped to the current target only.** Never batch across
  targets. Submitting closes the review (the flow continues in the
  composer), which destroys the per-target stash — drafts left on other
  targets are discarded, and the submit toast says so.
- Submitting while a target switch is still loading is a no-op (the old
  file list and new drafts must never mix).
- The transcript attachment chip labels the scope:
  `Code review · a1b2c3d · 4 comments`.

## Submission payload

`CodeReviewSubmission` carries commit metadata for committed targets:

```ts
commit?: { sha: string; parentSha: string; subject: string };
```

The prompt-text rendering states the target as a committed diff
(`<parentSha>..<sha>` plus a human subject — a commit's subject line, or
`<branch> vs <base>` for branch diffs) so the agent knows the line
numbers refer to that diff and can run `git diff <parentSha> <sha>` for
exact context. Branch targets use `sha = head` and
`parentSha = mergeBase`. Subjects are whitespace-collapsed and clamped
before entering the prompt.

## Amend/rebase staleness

Comment line numbers only make sense against the drafted sha. Defense:

- At submit, check `git merge-base --is-ancestor <pinnedSha> HEAD`. An
  amended or rebased-away commit stops being an ancestor of HEAD (the
  old object survives in the reflog, so existence checks — and tree
  hashes, which are immutable per sha — cannot detect rewrites).
- On failure, submission is blocked with an explanatory toast.
- Line numbers are never silently rebound — silent rebinding leads to the
  agent editing the wrong lines.

## Non-goals

- Commit graphs, arbitrary ranges (`sha..sha`), author columns,
  pagination beyond 20 commits.
- Reviewing merge commits against multiple parents (first parent only).
- Persisting drafts across app restarts (in-memory per review session,
  matching working-tree behavior).
