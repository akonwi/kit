# 0048: Check for updates from each client

## Status

Accepted. Specifies the update path of the app distributed under
[ADR 0029](0029-distribute-native-macos-app-separately.md).

## Context

Kit ships two independently versioned artifacts from one GitHub repository:
the `kit` executable, tagged `vX.Y.Z`, and the macOS app, tagged
`macos-vX.Y.Z`. Each GitHub release carries curated notes and archives whose
SHA-256 digests GitHub publishes with the assets. Most users install the CLI
with Homebrew (`akonwi/tap/kit`); others extract a release archive.

Nothing tells a user that a newer release exists, and upgrading the CLI is not
the end of an upgrade: the local server keeps running the old executable until
it restarts. A client may also be attached to a server on another machine,
whose installation it does not own.

## Decision

### Each client checks GitHub for its own artifact

Clients read the public GitHub releases API directly. The server does not
fetch, cache, or serve release data, and there is no session API for it: what
a client can update is the client's own installation, which only the client
knows. The terminal client compares its version with the newest `vX.Y.Z`
release; the macOS app compares its own with the newest `macos-vX.Y.Z`
release. Drafts, prereleases, and tags of the other artifact are ignored, and
versions compare numerically by major, minor, and patch.

A build without a release version — `dev`, or any build bound to a channel
([ADR 0047](0047-give-development-builds-an-isolated-kit-home.md)) — never
checks, never offers an update, and refuses `kit update`.

There is no setting to turn checks off. A check is one unauthenticated request
made when a client starts and again every six hours while it stays open, well
inside GitHub's anonymous rate limit. A failed check is silent; the client
tries again on its next schedule.

`kit print` and the other headless commands never check.

### `kit update` installs the CLI

`kit update` updates the running executable to the newest release:

- An executable inside a Homebrew Cellar is upgraded with
  `brew upgrade akonwi/tap/kit`, so Homebrew keeps owning its files.
- Any other executable is replaced with the release archive for its platform
  (`kit_vX.Y.Z_<os>_<arch>.tar.gz`). The archive must match the SHA-256 digest
  GitHub reports for it, and the extracted executable must report the expected
  version, before it atomically replaces the old one in its directory.

After installing, `kit update` offers to restart the local server when it runs
an older version. It says how many sessions are working or waiting on the user,
because a restart interrupts them, and restarts only when the user agrees.
`--yes` answers its prompts for scripts but restarts only an idle server.
Servers on other machines are never updated or restarted.

### The terminal client offers the update

When a newer release exists, the left side of the terminal client's footer
reads `Update available: vX.Y.Z`. Clicking it opens a dialog with the
release's summary, the command that will install it, and **Later** and
**Update**; `o` opens the release page. **Update** closes the dialog and runs
the same installation as `kit update` in the background, with its progress in
the footer.

When it succeeds, the client asks before finishing the update:

- Attached to this home's server, **Restart** restarts the server, which
  interrupts the running sessions the dialog counts, then relaunches the
  terminal client on the same session.
- Attached to another server, **Relaunch** relaunches only the terminal client
  on the same session; the server keeps its version.

**Later** keeps the footer reminder. A failure is reported in the footer, and
clicking it retries.

### The macOS app updates itself

The app shows one update chip — at the bottom of the sidebar with side tabs, or
before the tab-placement toggle with top tabs — that reads **Update
available**, then **Updating… N%**, then **Relaunch to update**, or **Update
failed**. **Kit › Check for Updates…** checks immediately. The chip opens a
sheet with the release notes, **Later**, and **Update**.

**Update** downloads `kit_macos-vX.Y.Z_darwin_arm64.zip` in the background. The
app installs it only when the archive matches GitHub's SHA-256 digest and the
extracted `Kit.app` is signed with Kit's Developer ID (team `M7B73F53MK`) and
passes Gatekeeper's notarization assessment. The verified app replaces the
running bundle when the user clicks **Relaunch to update** or quits Kit, so an
update never interrupts work; relaunching is an ordinary quit that restores
windows, tabs, and drafts. A failure leaves the installed app untouched.

Settings › Servers shows the version of the server on this Mac. When a newer
CLI release exists, **Update…** opens a terminal tab running `kit update`. The app never updates or restarts a server itself.

### Release notes open on GitHub

`/release-notes` in the terminal client and **Kit › Release Notes** in the app
open the GitHub release of the running version in the browser, or the releases
list for a build without a release version. Clients do not keep a release
history of their own.

## Consequences

- Release notes are written once, in `docs/releases`, and are what both the
  update prompt and GitHub show.
- The update prompt and `kit update` depend on the release workflow's tag and
  asset names and on GitHub publishing asset digests; changing either breaks
  updates for every installed client.
- Homebrew remains the owner of Homebrew installations. A Homebrew Cask of the
  app must be marked `auto_updates` so Homebrew does not fight the app's own
  updater.
- A user offline, or behind a proxy that blocks GitHub, sees no update
  prompts; `kit update` reports the failure.
- Restarting the server after an update stays a decision the user makes with
  the cost in view.
