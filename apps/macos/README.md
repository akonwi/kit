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
notarization, and bundled-daemon work remain on the roadmap.

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
Use a tab's context menu or the group menu to split, move, join, or close panes.
The shared composer always addresses the parent session. Enter submits;
Cmd+Enter inserts a newline. Cmd+K opens commands; Cmd+, opens settings.

File views read from the session server and support highlighted, selectable
content. Review and scratchpad server integration remain roadmap work. Subagent
conversations are read-only by product decision.

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
- [Outstanding work](../../docs/roadmap/macos.md)
