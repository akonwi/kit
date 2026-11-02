# 0056: Build a native iOS client

## Status

Proposed. Adds a viewport alongside the macOS app of
[ADR 0013](0013-native-macos-client.md), reaching servers through the URL
connector and pairing of
[ADR 0045](0045-connect-clients-to-local-ssh-and-network-servers.md).

## Context

An agent session often runs for minutes or hours without needing its user,
then stops on a question, a confirmation, or a finished turn. Away from the
desk, the user wants to see what each session is doing, answer what it asks,
and send a follow-up. The TUI and the macOS app need the user at the
machine. The browser client is not maintained.

A Kit server can already be reached from another device. It serves its one
listener behind a private tunnel or reverse proxy, and a device obtains a
revocable device token by redeeming a pairing link that `kit server pair`
prints as a URL and a QR code (ADR 0045). A phone needs a client that speaks
the same contracts.

The macOS app is written in Swift. Its session client, generated wire types,
Mermaid renderer, and figure layout use no AppKit and can serve a second
Apple client.

## Decision

Kit provides a native iPhone application written in Swift with SwiftUI. Like
the macOS app, it is a protocol client only: it contains no agent logic and
uses no web content for Kit's own interface.

### Viewport over network servers

The application reaches servers only through the URL connector. It does not
discover or start a local daemon, and it has no SSH connector, because iOS
has no system `ssh` for the connector to run.

- A server is added by redeeming a pairing link. The user scans the QR code
  that `kit server pair` shows or pastes the link. A confirmation names the
  server and the device; the device name defaults to the phone's name and is
  what `kit server devices` lists.
- Device tokens are stored in the Keychain, readable after first unlock and
  only on this device. They are neither synced nor restored to another
  device.
- Saved servers are kept in the application's own `servers.json`, in the
  saved-server format of ADR 0045, holding only URL entries. The list is not
  shared with any Kit home.
- The application may hold several servers at once. Sessions, session
  references, and subagents stay scoped to their server.
- Every saved server shows a state: connecting, online, unreachable, no
  longer paired, needing an update, or a failure the app reports as is. Expired and revoked device tokens
  both read "No longer paired. Pair again.", since the server answers both
  with the same error. Pairing again replaces the token of a saved server and
  is offered only for a link to the same URL.
- Compatibility follows ADR 0045: protocol versions must match, and a
  mismatch names the side to upgrade. A server with another protocol is kept
  after pairing and shown as needing an update.

### Transport security

Kit does not terminate TLS. The recommended deployment is HTTPS through a
private tunnel, such as Tailscale Serve on a tailnet. App Transport Security
permits plain HTTP only to local network names and IP addresses. Reaching
a server on the local network needs the system's Local Network permission,
and pairing over plain HTTP requires the user to confirm a warning that the
connection is not encrypted.

### Shared Apple code

Code that both Apple clients use lives in the `apps/apple/KitShared` Swift
package, which contains no AppKit or UIKit code:

- `KitClient`: the generated OpenAPI transport and types, the projections
  and validation built on them, pairing, device tokens, and client models.
  The generator lives with it at
  `apps/apple/KitShared/script/generate_openapi.sh`.
- `KitMermaid` and `KitFigures`: the native renderers of
  [ADR 0046](0046-draw-mermaid-diagrams-with-kits-own-renderer.md) and
  [ADR 0054](0054-draw-data-figures-from-fenced-markdown.md).
- `KitTranscript`: what a transcript shows, apart from how each platform
  draws it. It projects Markdown into blocks, presents tool calls with their
  titles, chips, and results, and links session mentions.

Code that only one platform can run stays in that platform. The SSH connector
is compiled only for macOS; on iOS, connecting to an SSH server fails with
an error saying that only Kit for Mac can reach it. Local daemon discovery
and startup stay in the macOS app, and each app draws transcripts with its
own views. A change to the shared package keeps both apps building.

### Presentation

The [Paper "Kit · iOS" file](https://app.paper.design/file/01M658WATTBR1SGQ5WZH3CJ4CP)
defines the presentation, alongside `.agents/skills/design/SKILL.md`. It uses
Kit's semantic surfaces and colors with native iOS navigation, controls, and
Liquid Glass, in light and dark appearance.

- **First run.** Until a server is paired, a full-screen first run offers
  scanning or pasting a link. The tabs appear once a server is paired.
- **Navigation.** Tabs for Sessions, Servers, and Settings. The
  Sessions tab lists the sessions of every paired server, latest activity
  first, with filters for all sessions, sessions that need the user, and each
  server. Needing the user is a filter, not a separate inbox.
- **Session.** A live transcript in Kit's reading layout. A slim composer
  capsule holds attach, text, and Send, which becomes Stop while a turn runs.
  A header menu shows the model and context use, and holds model and thinking
  selection, compaction, rename, fork, details, and delete.
- **Interactions.** Confirmations, choices, guided questions, and MCP
  sign-in replace the composer with a dock, as in the macOS app and the TUI,
  while the transcript stays readable above it.
- **Attachments.** Up to eight attachments sit in a strip above the composer
  and upload as soon as they are added. Send waits until every upload has
  finished.
- **Connectivity.** When the event stream drops, the transcript stays, a
  reconnect line replaces the activity line, and Send is disabled until the
  stream resumes from its cursor. Nothing is queued to send while
  disconnected.
- **Workspace.** Changes, files, and the scratchpad open as full screens from
  the session menu. A comment on a diff or file opens inline under the
  selected lines and waits above the composer until it is sent.

### Repository placement and build

The application lives under `apps/ios` as an Xcode project with synchronized
folders and no project generator. It targets iPhone on iOS 26 or later.
Release builds use the bundle identifier `com.akonwi.kit.ios`, apart from
the macOS app's `com.akonwi.kit`, and development builds use
`com.akonwi.kit.dev`. It shares the macOS app's icon. It has its own build
and tests, and does not affect the Go build or the macOS app's.

### Distribution

Release builds reach phones through TestFlight, listed in App Store Connect
as "Remote Control for Kit" and named Kit on the home screen.
`apps/ios/script/upload_testflight.sh` archives a committed tree and uploads
it with an App Store Connect API key, numbering each build by its UTC date
and time. Because protocol versions must match exactly (ADR 0045), a server
release that changes the protocol needs a TestFlight build that speaks it.
TestFlight builds expire after 90 days. Testing starts with internal
testers; external testing waits on a way for Apple's reviewer to use the app
without a Kit server.

Unit tests cover state and presentation logic. UI tests drive the app in a
simulator; the pairing test pairs with the running server of an isolated Kit
home ([ADR 0047](0047-give-development-builds-an-isolated-kit-home.md)) and
never uses the default home.

### Sequencing

1. **Connect.** First run, pairing, and the Servers tab.
2. **Read.** The Sessions tab and a live, read-only session transcript.
3. **Act.** Prompting, Stop, interactions, attachments, and the session
   menu.
4. Changes, files, the scratchpad, and comments follow.

### Non-goals

- iPad and Mac Catalyst layouts.
- Terminal and browser panes, the tiled workspace, and source editing.
- The SSH connector and local daemons.
- Provider login, server settings, and hosting session plugins.
- Push notifications. Notifying a phone while the app is closed needs a
  delivery service that Kit does not have and is left to a later decision.
- An offline queue of messages to send.

## Consequences

### Positive

- The user can follow sessions, answer interactions, and send follow-ups
  from a phone with no change to the server or the protocol.
- A fourth client exercises the session and pairing contracts, including
  their remote path.
- The shared package gives both Apple clients one implementation of the
  session client and the native renderers.

### Negative

- The transcript, composer, and interaction presentation are implemented
  again for a phone.
- Changes to `KitShared` must keep two apps building and tested.
- Exact protocol matching (ADR 0045) needs the phone app updated in step
  with every server the user pairs, and store review can delay an update.
- A device token is a bearer secret on a device that is carried around and
  can be lost; revoking it with `kit server devices revoke` is the remedy.
- Without push notifications, the user learns that a session needs them only
  by opening the app.
- The release surface grows to include iOS signing, provisioning, and App
  Store distribution.

### Relationship to other decisions

- Extends ADR 0013: the macOS app shares its session client, generated wire
  types, and renderers with this application through `apps/apple/KitShared`.
- Uses ADR 0045's URL connector, pairing, device tokens, and compatibility
  rules. The application keeps its own saved-server list, as it has no Kit
  home.
