---
description: Review staged/uncommitted changes, validate them, and create a git commit
---
Review the current git changes and prepare a commit.

Process:
1. Inspect `git status` and the relevant diff.
2. Summarize what changed.
3. If the changes complete or substantially address an item from `backlog/README.md`, update that backlog file before committing.
4. Run the required validation/check commands for this repo before committing.
5. If checks fail, fix the issues when appropriate and re-run the necessary checks.
6. Create a concise, accurate commit message.
7. Commit the changes.

If the working tree contains unrelated changes, ask before including them.
If there is nothing to commit, say so.

Additional user instruction: $@
