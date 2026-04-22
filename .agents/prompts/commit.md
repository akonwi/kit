---
description: Review staged/uncommitted changes, validate them, and create a git commit
---
Review the current git changes and prepare a commit.

Process:
1. Inspect `git status` and the relevant diff.
2. Summarize what changed.
3. Run the required validation/check commands for this repo before committing.
4. If checks fail, fix the issues when appropriate and re-run the necessary checks.
5. Create a concise, accurate commit message.
6. Commit the changes.

If the working tree contains unrelated changes, ask before including them.
If there is nothing to commit, say so.

Additional user instruction: $@
