# Kit for macOS

Native SwiftUI/AppKit client for the Kit v2 server. Requires macOS 15+ and
Xcode 26 or newer. The Go server remains authoritative for sessions, files,
execution, and shared state; the app does not embed the agent engine or database.

## Build and run

```sh
apps/macos/script/build_and_run.sh --verify
```

The script builds and packages `apps/macos/dist/Kit.app`, then launches it.
`--build` packages without launching. Xcode is required for the editor dependency's
resource generation. The development bundle is ad-hoc signed and must not be
published. The separately signed macOS app release procedure is documented below;
bundled-daemon work remains on the backlog.

Start a compatible Kit daemon separately. The app discovers it through
`$KIT_HOME/run`, defaulting to `~/.kit/run`, and checks identity and protocol
compatibility. It never starts a daemon or reads a separate legacy home. There is
no demo launch mode or packaged private transcript data.

## Apple Silicon app release

The macOS 15+ Apple Silicon app is released separately from the Go executable
as a notarized GitHub Release ZIP and a separate Homebrew Cask with manual
updates; see [ADR 0029](../../docs/adrs/0029-distribute-native-macos-app-separately.md).
It **does not include or start** a server. Install and start a compatible Kit
server separately. Discovery authenticates the registered server, checks its
identity and readiness, and negotiates protocol/release compatibility. For
protocol 42, the app's `KitClientRelease` identity (`0.39.0` for this build)
can attach to canonical stable server versions >= 0.39.0 that retain protocol 42,
without requiring the app and CLI to share an exact version. App version `0.1.3`
is independent of that client compatibility identity. Apps 0.1.1 and 0.1.2 use
protocol 41 and cannot attach to a protocol-42 server. Incompatible running
servers are left alone; update to compatible releases rather than restarting or
replacing one from the app.

An operator with a **Developer ID Application** certificate and a `notarytool`
keychain profile can prepare an artifact without launching the app, touching
the daemon, tagging, or publishing:

```sh
export KIT_DEVELOPER_ID_APPLICATION='Developer ID Application: Akonwi Ngoh (M7B73F53MK)'
# The operator's existing notarytool profile is named kit (the script defaults to it).
apps/macos/script/package_app.sh macos-vX.Y.Z
```

Run it only from a clean checkout at the reviewed `macos-vX.Y.Z` tag; the
initial app version was `0.1.0`. The script builds arm64 Release, stamps app
version, source-derived build number, source commit, and the pinned Kit client
compatibility release (`0.39.0`); signs with
hardened runtime; notarizes and staples the app; checks Gatekeeper; and writes
`apps/macos/dist/releases/kit_macos-vX.Y.Z_darwin_arm64.zip` plus its SHA-256
file. It fails if required credentials are missing. The development
`build_and_run.sh` remains ad-hoc signed by default; **never publish its output**.

Before release, confirm the build number increases over the last app release,
then run the full macOS tests and prepare curated notes stating the
source commit, protocol compatibility range, macOS 15+ Apple Silicon support,
external-server dependency, and manual-update steps. Create a **draft** GitHub
Release for the tag and upload the ZIP and checksum. Freshly download those
assets outside the checkout; verify `shasum -c`, bundle contents and source
commit, Developer ID signature, stapled ticket, Gatekeeper acceptance, launch,
and a live connection to a compatible server with an isolated `KIT_HOME`.
Also verify incompatible-server recovery and preservation of settings, drafts,
and window restoration after manual replacement. Only then publish the draft
as a regular release. Add/update a **separate** Homebrew Cask referencing the
published asset URL and exact SHA-256 without changing the CLI formula. Review
both manual and Cask upgrade paths. This is not release authorization or a claim
that these gates have passed.

## Navigation

Saved native session windows and tab groups restore on launch. Without a saved
layout, Kit shows the session launcher rather than selecting a session itself.
Cmd+T opens the launcher in a native tab. Search matches session names and paths;
arrow keys navigate and Return opens. New session accepts a working directory,
optional name, model, and thinking level.

Within a session, Agent, files, and subagents occupy retained workspace tabs.
Use a tab's context menu to split, move, join, or close panes.
The shared composer always addresses the parent session. Enter submits;
Cmd+Enter inserts a newline. Cmd+P opens commands; Cmd+, opens settings.
Session workspace controls (Diff, Scratchpad, Subagents, Open file, and Commands)
are centered in the footer, with status on the left and cwd/Git information on the
right. They remain available when an interaction replaces the composer.

File views read from the session server and support highlighted, selectable
content. The Diff toolbar opens working-tree, branch, and commit comparisons with
old/new line numbers and inline comments. Use Unified/Split and Wrap in the header
to adjust the presentation. Drag a gutter range to annotate it.
Scratchpad edits synchronize through the bound session with guarded autosave
and conflict review. Subagent conversations are read-only by product decision.

While Kit is inactive, a completed or failed turn or a new input request briefly
bounces its Dock icon. Closely timed events share one bounce; initial snapshots
and reconnects do not replay attention feedback. No notification permission is needed.

Session feedback appears above the composer. Confirmations fade after five seconds;
hovering or keyboard focus keeps them visible. Persistent warnings remain available in the footer’s notice list. Dismissing a
card or its list entry removes the notice from both surfaces. Notices are local
to each session and do not send system notifications. Compaction outcomes, shell
operation errors, run failures, and subagent-definition diagnostics use this surface;
compaction progress stays in the footer, while reload progress appears in the command
palette until hidden or complete. Hiding the palette does not stop the reload.
Input-specific errors remain at their source.

Live tool groups reveal their first five calls, then collapse when the sixth arrives.
The collapsed summary shows activity, the cumulative call count, and failures.
Groups also settle closed at completion; a manual expand/collapse choice takes
precedence and stays with the session. Automatic changes preserve scroll position.

Long final responses open at their beginning when you were following the live turn.
Reading earlier content keeps your current position. A quiet section menu and
previous/next controls navigate the visible long response using its rendered
Markdown headings; scrolling remains continuous and the composer stays available.
Latest resumes following the bottom. Loading a saved transcript does not initiate
a new-response jump. The same behavior applies to subagent conversations.

## Development and validation

The macOS client uses the committed, tag-filtered generated OpenAPI client. After changing `api/kit-session.openapi.json`:

```sh
apps/macos/script/generate_openapi.sh
apps/macos/script/generate_openapi.sh --check
```

Run macOS tests from the package directory:

```sh
cd apps/macos
xcodebuild -scheme Kit -destination 'platform=macOS' \
  -derivedDataPath .build/xcode -skipPackagePluginValidation test
```

The read-only local-server compatibility check is opt-in:

```sh
TEST_RUNNER_KIT_LIVE_TEST=1 xcodebuild -scheme Kit \
  -destination 'platform=macOS' -derivedDataPath .build/xcode \
  -skipPackagePluginValidation -only-testing:KitTests/LiveClientSmokeTests test
```

Other opt-in integration checks are documented at their test declarations;
some create temporary sessions and invoke model or shell work. Ordinary tests
use synthetic clients. `Fixtures/Themes` contains Flexoki import-regression
inputs used by `ThemeTests`, not app resources.

## References

- [Client architecture](../../docs/adrs/0013-native-macos-client.md)
- [Source editing and highlighting decision](../../docs/adrs/0013-native-source-editing-and-highlighting.md)
- [Native design language](../../docs/design/macos-design-language.md)
- [Client implementation reference](../../docs/design/macos-client.md)
- [Outstanding work](../../backlog/macos.md)

### Settings

The Settings window separates app-local Appearance preferences (themes and
interface/code typography) from Models defaults shared with the TUI. Models
reads `$KIT_HOME/settings.json`, defaulting to `~/.kit/settings.json`, and
edits `defaultModel` and per-provider/model `modelOverrides.contextWindow` values.
Local daemon discovery uses the same home. Appearance discovers custom themes in
`$KIT_HOME/themes` (defaulting to `~/.kit/themes`), the same directory used by
the TUI. Theme files are managed directly in that directory; light and dark
assignments remain app-local. New sessions prefer the configured default when it is available;
changing it does not reconfigure existing sessions.

Settings writes re-read the latest document, preserve unrelated JSON fields,
and replace the file atomically. Reads and writes are limited to 1 MB. Invalid
configuration is reported without overwriting it; unavailable model catalogs do
not prevent editing saved overrides. Removing an override restores the model's
default context limit. As with the CLI settings store, simultaneous writes by
independent processes are last-writer-wins. App-local appearance remains in
UserDefaults and does not change the terminal theme.
