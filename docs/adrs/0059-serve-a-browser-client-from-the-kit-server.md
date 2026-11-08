# 0059: Serve a browser client from the Kit server

## Status

Accepted. Realizes the semantic web client of
[ADR 0001](0001-native-go-architecture.md) as a viewport over the session API,
reached through the pairing of
[ADR 0045](0045-connect-clients-to-local-ssh-and-network-servers.md).

## Context

A Kit server can be reached from other devices through a private tunnel or a
reverse proxy, and a device obtains a revocable device token by redeeming a
pairing link (ADR 0045). The TUI and the macOS app need Kit installed on the
machine in front of the user, and the iOS app
([ADR 0056](0056-build-a-native-ios-client.md)) needs an iPhone. An Android
phone, or a computer without Kit installed, has only a browser.

The session API is described by the Go-defined OpenAPI contract
([ADR 0031](0031-publish-go-defined-openapi-contract.md)), with closed
discriminated unions ([ADR 0032](0032-model-protocol-variants-as-discriminated-unions.md))
and described server-push streams
([ADR 0035](0035-describe-server-push-streams-in-the-session-contract.md)).
TypeScript types can be generated from it the way the Apple clients generate
Swift. The server checks each request's `Host` and `Origin` headers and
accepts a browser origin that names the request's own host.

## Decision

Kit provides a browser client written in TypeScript with Solid and Mica, for
Android phones and any browser on a machine without Kit installed. Like the
other clients it is a protocol client only: it contains no agent logic and
reads no server-local state.

### Served by the server it controls

The Kit server serves the client's compiled HTML, JavaScript, CSS, and fonts
from its one listener, on the same origin as the session API. The client
talks only to the origin that served it.

- The client answers a fixed set of `GET` and `HEAD` paths: its pages (`/`,
  the pairing link path `/pair`, and `/sessions/{id}`), the hashed files of
  its build under `/assets/`, and the root files a phone reads to add it to
  the home screen (`/manifest.webmanifest`, `/favicon.ico`, the icons the
  manifest names, and `/robots.txt`, which disallows crawling). Any other path
  is served exactly as it would be without the client; there is no catch-all
  page.
- These requests need no credential, instance, or protocol, so an unpaired
  browser can load the page that pairs it, and they never count as failed
  authentications. Their responses contain no session, settings, or server
  data. `Host` and `Origin` checking applies to them as to every request.
- They are not session API operations and are not part of the OpenAPI
  contract. The session API under `/v1` keeps its credential, instance, and
  protocol checks. Same-origin requests need no CORS.
- Pages revalidate on every load and build files are cached as immutable.
  A build file may have a precompressed gzip variant, served when the browser
  accepts it.
- An executable built without the client answers its pages with a 404 page
  that says so.
- The page served for a pairing link's path redeems the link, so a link
  printed by `kit server pair`, the TUI, or the macOS app opens the browser
  client when it is followed in a browser.
- Because the server serves the client, the two always speak the same
  protocol. When the server the page loaded from is replaced, the session API
  answers with an instance or protocol mismatch, and the client reloads to
  load the replacement's client. It reloads at most once for each server
  instance, then shows an error, so a mismatch that persists cannot loop.

### Build and release

The client lives in `apps/web`. Bun is a development and build-time dependency
only; users need no JavaScript runtime. The release build compiles the client
into `internal/webui/build`, and the `kit` executable embeds that directory
with `go:embed`, which adds no native code or runtime dependency. A checkout
holds no build output there, so building and testing the Go module does not
require Bun, and a release refuses to publish an executable without the
client.

### Contract

The client's wire types are generated from `api/kit-session.openapi.json`
with `openapi-typescript`, committed, and checked for staleness in CI like
the Swift client's. The client does not declare its own request, response, or
event records. Session events are decoded through the contract's closed
`SessionEvent` union.

### Credentials

A browser pairs by redeeming a pairing link, which names the device after the
browser. It stores the returned device token in `localStorage` for its origin
and sends it as a bearer token, including on event streams, which it reads
with `fetch` because `EventSource` cannot send headers. Images and other
authenticated resources are fetched the same way.

- Signing out revokes the device on the server and clears the stored token.
- An expired or revoked token returns the browser to pairing.
- A bearer token is not sent automatically by the browser, so other sites
  cannot make authenticated requests on the user's behalf.

Any script running in the client's origin can read the token, so the client
treats the origin as a security boundary:

- Responses carry a Content Security Policy that allows scripts, styles, and
  fonts only from the client's own origin, with no inline script and no
  third-party content.
- Remote and persisted content, including Markdown, renders as untrusted text
  unless it crosses an explicit sanitization boundary.
- The client never opens HTML, SVG, or other active content from a session in
  its origin. Attachments are shown as raster images or offered as downloads.

### Presentation

The client follows Kit's design language and is laid out for a phone first,
widening to use a larger window. It provides, in order:

1. **Connect.** Pairing, signing out, and reconnection with event-stream
   resume.
2. **Read and act.** The session list, a live transcript with Markdown and
   tool activity, the composer with steering, follow-ups, and Stop,
   interactions, and the model and thinking pickers.
3. **Parity.** Attachments, changes with diffs and comments, files, the
   scratchpad, subagents, and paired devices.

Mermaid flowcharts and sequence diagrams are drawn with Pele, as
[ADR 0046](0046-draw-mermaid-diagrams-with-kits-own-renderer.md#the-browser-client)
describes. Data figures are drawn by a TypeScript port of the reference
parser in [ADR 0054](0054-draw-data-figures-from-fenced-markdown.md), held to
the shared fixtures in `testdata/figures` like the Swift port, and laid out
as the macOS client lays them out.

### Non-goals

- Cookie sessions.
- Sharing sessions with other people or read-only links; Kit remains
  single-user.
- A `kit web` command or other local launcher.
- Hosting the client anywhere other than the server it talks to, under a
  path prefix, or one page talking to several servers.
- Offline use and push notifications. Adding the client to a home screen
  opens it like a bookmark; it installs no service worker.
- Terminal and browser panes, the tiled workspace, source editing, provider
  login, server settings, and hosting session plugins.

## Consequences

### Positive

- Any device with a browser can follow sessions, answer interactions, and
  send follow-ups after pairing, with no app to install.
- Serving the client from the server removes version skew between them and
  removes cross-origin configuration.
- Generated types keep the client on the canonical contract, and the closed
  event union gives it the same exhaustiveness the Go and Swift clients have.

### Negative

- The transcript, composer, interaction, and diff presentation are
  implemented a third time, and figure and Mermaid rendering a third time
  once ported.
- A device token in `localStorage` is readable by any script in the origin;
  the client's security depends on its Content Security Policy and
  sanitization holding.
- A browser on a shared or borrowed computer keeps its token until the user
  signs out, the device is revoked, or the token expires.
- The release build needs Bun, and the executable grows by the client's
  assets.
- The session API gains unauthenticated routes for static assets, which must
  stay free of data.

### Relationship to other decisions

- Realizes ADR 0001's semantic web client: Solid and Mica, built with Bun,
  embedded in the executable, served same-origin, with sanitized rendering.
- Uses ADR 0045's pairing and device tokens. Cookie sessions stay outside
  that decision and this one.
- Consumes the contract of ADRs 0031, 0032, and 0035; a new event kind is a
  protocol change, which the served client always matches.
- Complements ADR 0056: the iOS app serves iPhones, this client every other
  browser.
