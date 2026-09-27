# 0029: Distribute the native macOS app separately

## Status

Accepted

## Context

Kit's Go executable and native macOS client have independent installation and
update paths. The app connects to an existing local Kit server; it does not
bundle or manage the daemon. Developer builds are ad-hoc signed and cannot be
distributed as trusted macOS apps. A usable desktop release must state its
external-server dependency and verify compatibility without prescribing an
identical CLI version.

## Decision

Kit distributes the Apple Silicon, macOS 15+ app as a **regular release**
separately from the Go executable. App tags use `macos-vX.Y.Z` and do not
trigger the Go release workflow's `vX.Y.Z` tags. The bundle uses `X.Y.Z` for
`CFBundleShortVersionString` and a positive source-commit-count build number for
`CFBundleVersion`, retains the bundle identifier `com.akonwi.kit`, and embeds
the reviewed source commit. The first
download is a versioned ZIP containing `Kit.app` on a GitHub Release. A
separate Homebrew Cask installs that same ZIP; it does not replace the `kit`
CLI formula. Publish its SHA-256 alongside the archive.

The app requires a separately installed, running **compatible** Kit server.
It discovers the registered loopback server without starting, replacing, or
restarting it. It authenticates health, checks registry/health identity and
database readiness, then negotiates the protocol and release policy used by the
Go client. For protocol 40, differing canonical stable releases at or above
`0.37.0` are compatible; equal labels attach, but equal `dev` labels alone do
not guarantee compatibility across different builds. Differing dev/prerelease
labels or protocol versions do not attach. The app's bundle release version is
its client release identity. Release notes and the Cask description disclose
the external-server requirement and explain recovery from an incompatible
running server without replacing it automatically. Verify the released app
against a compatible daemon in an isolated Kit home; do not require one exact
CLI version.

Updates are manual: download and replace the app or upgrade its Cask. Keep the
bundle identifier stable and verify settings, drafts, and window restoration
across replacement. No updater or background update service is included.

Distribution requires a Developer ID Application signature with hardened
runtime, Apple notarization, and a stapled ticket. Signing identity and
notarization credentials are provided by the operator's keychain, never stored
in the repository. Packaging fails closed if credentials are missing, with no
ad-hoc fallback. The operator verifies the build number increases over the
last app release, tags a reviewed clean commit, creates a draft GitHub Release,
and uploads the ZIP and checksum. Freshly download the draft
asset outside the checkout and verify Gatekeeper acceptance before publication.
Updating the Homebrew tap is a separate reviewed step, not a build side effect.
The existing Go release workflow remains independent.

## Consequences

- App users install and run a compatible Kit server separately until a
  self-contained app is available.
- Signing, notarization, fresh-download verification, and an installed-artifact
  daemon compatibility test are release gates, not consequences of a CI build.
- The CLI formula and macOS Cask have distinct installation and upgrade paths.
- Bundled daemon management, in-app updates, and Intel coverage remain separate
  work; none blocks describing this external-server app as a regular release.

Outstanding verification and distribution work is tracked in
[`backlog/macos.md`](../../backlog/macos.md).
