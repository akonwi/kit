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
resource generation. The development bundle is ad-hoc signed; distribution,
notarization, and bundled-daemon work remain on the backlog.

Start a compatible Kit v2 daemon separately. The app discovers it through
`~/.kit-v2/run` and checks identity and protocol compatibility. It never starts
a daemon or reads legacy `~/.kit` data. There is no demo launch mode or packaged
private transcript data.

## Navigation

Saved native session windows and tab groups restore on launch. Without a saved
layout, Kit shows the session launcher rather than selecting a session itself.
Cmd+T opens the launcher in a native tab. Search matches session names and paths;
arrow keys navigate and Return opens. New session accepts a working directory,
optional name, model, and thinking level.

Within a session, Agent, files, and subagents occupy retained workspace tabs.
Use a tab's context menu to split, move, join, or close panes.
The shared composer always addresses the parent session. Enter submits;
Cmd+Enter inserts a newline. Cmd+K opens commands; Cmd+, opens settings.

File views read from the session server and support highlighted, selectable
content. The Diff toolbar opens working-tree, branch, and commit comparisons with
old/new line numbers and inline comments. Use Unified/Split and Wrap in the header
to adjust the presentation. Drag a gutter range to annotate it.
Scratchpad server integration remains backlog work. Subagent
conversations are read-only by product decision.

While Kit is inactive, a completed or failed turn or a new input request briefly
bounces its Dock icon. Closely timed events share one bounce; initial snapshots
and reconnects do not replay attention feedback. No notification permission is needed.

Session feedback appears above the composer. Confirmations fade after five seconds;
hovering or keyboard focus keeps them visible. Persistent warnings remain available in the footer’s notice list. Dismissing a
card or its list entry removes the notice from both surfaces. Notices are local
to each session and do not send system notifications. Compaction outcomes, shell
operation errors, run failures, and subagent-definition diagnostics use this surface;
ongoing progress stays in the footer and input-specific errors remain at their source.

Live tool groups reveal their first five calls, then collapse when the sixth arrives.
The collapsed summary shows activity, the cumulative call count, and failures.
Groups also settle closed at completion; a manual expand/collapse choice takes
precedence and stays with the session. Automatic changes preserve scroll position.

## Development and validation

The Swift wire graph is generated from `internal/protocol`. After protocol changes:

```sh
python3 apps/macos/script/generate_wire.py
python3 apps/macos/script/generate_wire.py --check
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
reads `$KIT_HOME/settings.json`, defaulting to `~/.kit-v2/settings.json`, and
edits `defaultModel` and per-provider/model `modelOverrides.contextWindow` values.
Local daemon discovery uses the same home. New sessions prefer the configured
default when it is available; changing it does not reconfigure existing sessions.

Settings writes re-read the latest document, preserve unrelated JSON fields,
and replace the file atomically. Reads and writes are limited to 1 MB. Invalid
configuration is reported without overwriting it; unavailable model catalogs do
not prevent editing saved overrides. Removing an override restores the model's
default context limit. As with the CLI settings store, simultaneous writes by
independent processes are last-writer-wins. App-local appearance remains in
UserDefaults and does not change the terminal theme.
