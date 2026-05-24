---
name: release
description: Prepare and publish a Kit release. Use when asked to release Kit, cut a release, publish a new version, or bump the package for release.
---

# Release skill

Use this skill to publish a new Kit package release.

## Steps

1. Inspect the working tree.
   - Run `git status --short`.
   - Do not include unrelated local changes or generated artifacts.
   - If unrelated changes are present, ask before continuing.

2. Find the last release bump commit.
   - Use a commit whose subject is exactly `bump: x.x.x`, for example:
     ```sh
     git log --grep='^bump: [0-9]\+\.[0-9]\+\.[0-9]\+$' --format='%H %s' -n 1
     ```

3. Review changes since that bump.
   - Inspect commits and relevant diffs, for example:
     ```sh
     git log <last-bump-sha>..HEAD --oneline --no-merges
     git diff --stat <last-bump-sha>..HEAD
     ```
   - Choose the release type:
     - `minor` for user-facing features or meaningful new capabilities.
     - `patch` for fixes, documentation, refactors, chores, and small internal improvements.
   - If the release type is ambiguous, ask the user to choose minor or patch.

4. Update `package.json` to the next version.
   - Increment the current version based on the chosen release type.
   - Do not use prerelease versions unless explicitly requested.

5. Run required validation before publishing.
   - `bun run typecheck`
   - `bun run check`
   - Address any remaining Biome warnings.
   - Re-run `bun run typecheck` after `bun run check`.
   - Run `bun test` before publishing.

6. Publish to npm.
   - Run:
     ```sh
     npm publish --access public
     ```
   - This command requires user approval in this project.
   - If publishing fails, fix the issue when appropriate and retry only after explaining the failure.

7. Commit the version bump.
   - Stage only the version file changes required for the release.
   - Use this exact commit subject format:
     ```text
     bump: x.x.x
     ```
   - Replace `x.x.x` with the version that was published.

8. Report the result.
   - Include the published version, release type, validation commands, and bump commit hash.
