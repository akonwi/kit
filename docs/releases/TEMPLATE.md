# Kit X.Y.Z

Briefly summarize the release. Mention that the native macOS app is released
separately and is not included in CLI archives.

## What's changed

List the commits included since the previous CLI release. Commit subjects may be
used directly as the changelog; omit release-only housekeeping that does not help
users understand the release.

- `abcdef12` — feat(scope): describe the change
- `12345678` — fix(scope): describe the fix

<!--
Include this section only when the release contains breaking changes. Describe
exactly what users and client authors must do before or after installing the new
version. Remove this entire section for non-breaking releases.

## Upgrade guide

- Explain the breaking change and affected users.
- Give version-matched migration steps and daemon restart ordering when needed.
- Document protocol or Go client SDK changes with examples where useful.
-->

## Compatibility and limitations

Document relevant protocol compatibility, supported platforms, macOS minimum
version, Linux glibc baseline, known limitations, and plugin or integration
runtime requirements. Avoid repeating unchanged details unless users need them
to evaluate or install this release.
