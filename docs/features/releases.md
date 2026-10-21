# Updates and release notes

Kit's terminal client and macOS app each tell you when a newer release of
themselves is out, and install it when you ask
([ADR 0048](../adrs/0048-check-for-updates-from-each-client.md)).

## Checking

Each client asks GitHub for Kit's releases when it starts and every six hours
while it stays open. The terminal client compares its version with the newest
`vX.Y.Z` release; the app compares its own with the newest `macos-vX.Y.Z`
release. Drafts and prereleases are skipped. A failed check is silent and is
tried again on the next schedule. There is no setting to turn checks off.

Development builds and builds bound to a channel never check, and `kit update`
refuses to replace them.

## `kit update`

`kit update` shows the newest release's summary and link and asks before
installing it:

- Installed with Homebrew, it runs `brew upgrade akonwi/tap/kit`.
- Installed from a release archive, it downloads
  `kit_vX.Y.Z_<os>_<arch>.tar.gz`, checks it against the SHA-256 digest GitHub
  publishes, checks that the new executable reports the new version, and
  replaces the old one in place. The directory must be writable.

The local server keeps running the old Kit until it restarts. When it runs an
older version, `kit update` says how many sessions are working or waiting on
you, which a restart interrupts, and asks whether to restart it. It does so
even when the CLI is already up to date. Servers on other machines are never
changed; update Kit on that machine.

`kit update --yes` installs without asking, for scripts, and restarts the local
server only when no session is working.

## In the terminal client

When a newer release exists, the left of the footer reads
`Update available: vX.Y.Z`. A session's queue or reconnection status takes
that place while it has one. Click the item to see the release's summary and
how Kit installs it; `o` opens the release on GitHub. **Update** installs it in
the background, with its progress in the footer.

Once it is installed, the footer reads `Restart to finish update`, or
`Relaunch to finish update` when attached to another server. Its dialog:

- **Restart** restarts this home's server, interrupting the sessions it
  counts, and reopens the terminal client on the same session with the new
  Kit.
- **Relaunch** reopens only the terminal client on the same session; the
  server you are attached to keeps its version.

A temporary session is disposed when the client exits, so relaunching opens a
new temporary session in the same directory. A failed install shows its reason
in the footer; clicking it offers to try again.

## In the macOS app

The app shows one update chip at the bottom of the sidebar, or before the
tab-placement toggle with top tabs: **Update available**, **Updating… N%**,
**Relaunch to update**, or **Update failed**. **Kit › Check for Updates…**
checks immediately. The chip opens a sheet with the release notes.

**Update** downloads the app in the background. Kit installs it only when the
archive matches GitHub's SHA-256 digest and the app inside is signed with
Kit's Developer ID and notarized. It replaces the app when you click
**Relaunch to update** or quit Kit. Relaunching restores windows, tabs, and
drafts, and sessions keep running on their servers.

Settings › Servers shows each server's version. When a newer CLI release
exists, **Update…** beside this Mac's server opens a Terminal tab running
`kit update`.

## Release notes

`/release-notes` in the terminal client and **Kit › Release Notes** in the app
open the running version's release on GitHub, or the list of releases for a
development build. Notes are written in `docs/releases` and published with
each release.
