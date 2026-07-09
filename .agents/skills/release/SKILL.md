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
   - Push the bump commit to `origin/main`.

8. Tag the release to trigger the binary build.
   - Tag the bump commit and push the tag:
     ```sh
     git tag vx.x.x
     git push origin vx.x.x
     ```
   - The tag push triggers `.github/workflows/release.yml`, which builds
     the compiled binary on four platforms (darwin/linux × arm64/amd64)
     and attaches `kit_vx.x.x_<platform>.tar.gz` tarballs to a GitHub
     release.
   - Wait for the workflow to finish, for example:
     ```sh
     gh run list --workflow=release.yml --limit 1
     gh run watch <run-id> --exit-status
     ```
   - Verify all four assets exist:
     ```sh
     gh release view vx.x.x --json assets -q '.assets[].name'
     ```

9. Update the Homebrew formula in `../homebrew-tap`.
   - Compute the sha256 of each release tarball:
     ```sh
     for p in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
       curl -sL "https://github.com/akonwi/kit/releases/download/vx.x.x/kit_vx.x.x_${p}.tar.gz" | shasum -a 256
     done
     ```
   - In `../homebrew-tap/Formula/kit.rb`, update the `version`, the four
     `url` values, the four `sha256` values, and the version asserted in
     the `test` block.
   - Commit in the tap repo with the subject `kit x.x.x` and push.
   - Verify the install:
     ```sh
     brew update && brew upgrade akonwi/tap/kit && brew test kit
     ```

10. Report the result.
    - Include the published version, release type, validation commands,
      bump commit hash, release tag, and the tap commit updating the
      formula.
