---
name: release
description: Prepare and publish a Kit release. Use when asked to release Kit, cut a release, publish a new version, or bump the package for release.
---

# Release skill

Publish Kit's Go executable through GitHub Releases and Homebrew. Do not build
`apps/web`, change its package version or release notes, or publish to npm.

## Steps

1. Inspect `git status --short`, the current branch, and recent release tags.
   Do not include unrelated changes. Release from reviewed, committed `main`.
   Read the release requirements in `backlog/README.md` and the core/TUI backlogs; a successful
   build alone does not establish release readiness. Report unresolved gates
   before publishing.

2. Review commits since the last published release (`gh release list` and
   `git log <previous-tag>..HEAD`). Choose a patch for fixes and maintenance,
   minor for new capabilities; ask if ambiguous. Use a stable `vX.Y.Z` tag.
   The tag is the release version: there is no package.json version bump.
   `internal/version.Version` and `Commit` keep their development defaults;
   the workflow overrides them with linker flags.

3. Write curated notes in `docs/releases/vX.Y.Z.md` before publishing. Explain
   user-facing changes, getting started, compatibility changes, platform limits,
   and installation/upgrade instructions. For runtime or plugin changes, include
   version-matched migration guidance that users and coding assistants can follow.
   Review the notes against the shipped code; do not claim outstanding checks
   have passed. The workflow requires this file and publishes it verbatim.

   Validate before publishing:
   ```sh
   gofmt -l .
   go build ./...
   go vet ./...
   go test ./...
   go test -race ./...
   go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/*.yml
   git diff --check
   ```
   Formatting must produce no filenames. Use an isolated `KIT_HOME` for tests.
   CGO and a C/C++ toolchain are required for Tree-sitter (ADR 0021).

4. Dry-run the release build on the local platform before tagging. Substitute
   the intended version below. Use a temporary output directory, not tracked
   build artifacts:
   ```sh
   version=1.2.3
   commit=$(git rev-parse HEAD)
   out=$(mktemp -d)
   # On macOS, also export MACOSX_DEPLOYMENT_TARGET=14.0.
   CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X github.com/akonwi/kit/internal/version.Version=$version -X github.com/akonwi/kit/internal/version.Commit=$commit" -o "$out/kit" ./cmd/kit
   tar -czf "$out/kit.tar.gz" -C "$out" kit
   mkdir "$out/extracted"
   tar -xzf "$out/kit.tar.gz" -C "$out/extracted"
   KIT_HOME="$out/home" "$out/extracted/kit" version
   KIT_HOME="$out/home" "$out/extracted/kit" --help
   ```
   Assert version output is exactly `kit X.Y.Z (<full commit SHA>)` and the
   tarball contains only `kit`. Remove the temporary directory when done.
   This validates only the host platform, not all four release targets.

5. Commit any intended release preparation with a Conventional Commit and push
   `main`. No empty version-bump commit is needed. Ensure the working tree is
   clean and the intended commit is on `origin/main`. Only with authorization
   to publish, tag that commit and push the tag:
   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
   `.github/workflows/release.yml` builds on native macOS/Linux arm64/amd64
   runners using Go from `go.mod`, with CGO enabled. macOS targets 14.0 or newer;
   Linux builds use Ubuntu 24.04 (glibc 2.39 baseline). Tree-sitter is compiled
   in; system libraries remain platform dependencies. Do not claim fully
   static Linux binaries or compatibility with older libc versions.

6. Watch the tag's workflow (`gh run list --workflow=release.yml`, then
   `gh run watch <run-id> --exit-status`). Each job packages only the Go `kit`
   executable and smoke-tests the extracted binary's version and help.
   The release job publishes `docs/releases/vX.Y.Z.md`, not generated PR summaries
   or web-bundled notes. Verify the published body matches the curated notes.
   Confirm all four `kit_vX.Y.Z_<platform>.tar.gz` assets exist:
   ```sh
   gh release view vX.Y.Z --json assets,body
   ```
   Platforms: `darwin_arm64`, `darwin_amd64`, `linux_arm64`, `linux_amd64`.

7. Update `../homebrew-tap/Formula/kit.rb` using downloaded release assets and
   their SHA-256 hashes. Update version, all four URLs/hashes, install logic
   (the archive now contains only `kit`, no `runtime`), OS requirements, and
   the version test. Inspect the formula before editing. Commit with subject
   `kit X.Y.Z` and push with release authorization. Verify:
   ```sh
   brew update && brew upgrade akonwi/tap/kit && brew test kit
   ```
   Verify manual extraction/install too. Existing npm users must remove their
   npm installation and verify PATH resolves to the new binary; do not publish
   an npm update. Follow the migration guidance and outstanding distribution
   verification gates in the backlog.

8. Report the version, validation results and platform limits, release commit,
   tag, GitHub release URL, and Homebrew tap commit. Never imply a local dry run
   published a release or verified other platforms.
