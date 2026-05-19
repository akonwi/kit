# GitHub Integration

Kit has lightweight built-in GitHub awareness through the internal VCS status plugin when the GitHub CLI (`gh`) is available and authenticated.

Current behavior:

- Kit uses local git state for branch and dirty status.
- When the current directory is a GitHub repository and the current branch has an associated pull request, Kit asks `gh pr view` for PR metadata.
- The bottom status footer displays the pull request number inside the VCS location, for example:
  - `/path/to/repo (feature-branch · PR #123)`
  - `/path/to/repo (feature-branch* · PR #123)` when the worktree is dirty
- GitHub lookup failures are silent. If `gh` is missing, unauthenticated, outside a GitHub repository, or the branch has no PR, no PR label is shown.

Implementation notes:

- `gh` is used as the first integration boundary so Kit can reuse the user's existing GitHub authentication.
- PR metadata is resolved asynchronously by the VCS status plugin and cached briefly per branch to avoid calling `gh` on every local VCS update.
- Future GitHub features, such as PR diff review and rendering PR comments in review views, should build on the same GitHub adapter surface.
