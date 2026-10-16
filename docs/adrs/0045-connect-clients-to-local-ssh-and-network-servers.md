# 0045: Connect clients to local, SSH, and network servers

## Status

Proposed

## Context

Kit is a single-user agent whose Go server is authoritative for sessions
([ADR 0001](./0001-native-go-architecture.md)). Clients are viewports over the
session API ([ADR 0013](./0013-native-macos-client.md),
[ADR 0035](./0035-build-the-terminal-client-with-ard-and-cooper.md)) and reach
it through the public Go client or generated Swift client
([ADR 0038](./0038-expose-a-typed-go-client-api.md)).

Several workflows need a client to reach a server other than the one its own
Kit home would start:

- a server on a workstation, VPS, or cloud host, served through a tunnel,
  private network, or reverse proxy, or reached over SSH;
- the native macOS app holding the local server and remote servers at once;
- development worktrees, where a worktree that changes the server runs an
  isolated daemon and another worktree's compatible client attaches to it
  without adopting that daemon's home or replacing any daemon.

These workflows share requirements: a client must address a server
explicitly, authenticate durably where needed, survive server restarts,
refuse incompatible servers without managing their lifecycle, and stop
treating the server's filesystem, credentials, and settings as the client's.

The session protocol changes frequently. Compatibility is an exact protocol
version match, and the database migrates forward only.

OpenCode 2 was studied as a reference. It serves one listener whose exposure
is a service setting, authenticates with one shared password plus
password-derived pairing tokens, attaches desktop clients over SSH by reading
the remote service registration and forwarding a loopback port, and keeps a
server registry in its GUI with one server per terminal process.

## Decision

### Server identity

Every server reports, through the authenticated health operation:

- `serverId`: a stable identifier persisted in the Kit home. Saved servers,
  reconnection, and client state attach to it.
- `instanceId`: the identifier of the running process. A change means the
  server restarted; clients re-handshake and resubscribe rather than fail.
- `machineId`: a Kit-scoped hash of the host machine identifier
  (`IOPlatformUUID` on macOS, `/etc/machine-id` on Linux). Clients compute the
  same hash locally. A server is *same-machine* exactly when the values match.
  Locality is never inferred from the request address, because SSH forwarding
  makes every remote request arrive on loopback.
- `home`: the server's user home directory, used only to present server paths.
- `capabilities`: optional, configuration-dependent features. Every field is
  optional so newer clients decode older servers. Capabilities grow
  additively and do not require a protocol bump.

Clients learn `instanceId` from health. Explicit targets do not require it up
front.

### One listener

The daemon serves the session API on one listener. Its address is the
`server.listen` setting, overridable per invocation with
`kit server start --listen <address>`. The default is loopback on an
ephemeral port.

- Binding a non-loopback address requires that explicit setting.
- Kit does not terminate TLS. Non-loopback deployments rely on a trusted
  tunnel, private network, or reverse proxy. Clients treat plain HTTP to a
  non-loopback host as an explicit insecure choice.
- Changing the listener or exposure settings restarts the daemon.

Request validation:

- `Host` must be a loopback IP literal or `localhost` on any port, or a
  configured allowed host. Hostname allowlisting preserves DNS-rebinding
  protection, which `localhost` cannot defeat because no remote site controls
  it; loopback hosts on arbitrary ports permit SSH-forwarded connections.
- A present `Origin` must be the request's own origin or a configured allowed
  origin.
- Every request requires a credential, except redeeming a pairing code.
  Lifecycle operations remain available to any valid credential; clients
  offer daemon control only for same-machine servers.
- Request, upload, connection, and stream bounds apply to every request
  regardless of credential. Pairing redemption and failed authentication are
  rate-limited.

### Deployment

Serving through a tunnel, private network, or reverse proxy that terminates
TLS and forwards to the Kit listener is the primary remote deployment. It
applies equally to a workstation, a VPS, and a cloud host.

- **Private networks**, such as a tailnet, are the recommended deployment.
  Network membership and access rules gate reachability before Kit's
  credentials are checked.
- **Public internet** exposure, such as Tailscale Funnel or a public reverse
  proxy, is supported. Kit's credentials are then the only barrier to code
  execution as the server user. Deployment documentation states this and
  requires TLS.
- **`kit server expose --tailscale`** is a convenience for tailnet serving.
  It reads the machine's tailnet DNS name from the `tailscale` CLI, assigns a
  fixed loopback port when the listener is ephemeral, adds the DNS name to the
  allowed hosts, runs `tailscale serve` for that port, and prints the served
  URL and a pairing link. `kit server expose --tailscale --off` removes the
  Serve configuration and the settings it added. It does not use Funnel.

A server is *serving* when its listener or allowed hosts differ from the
defaults.

### Credentials

Kit remains single-user. All valid credentials have identical authority; there
are no scopes or accounts.

**Local token.** Each daemon process generates a token and publishes it
privately in its home alongside the discovery registry. It is distributed only
through the filesystem, including over SSH, and is replaced on every start.

**Device tokens.** Durable access for explicitly addressed servers uses device
tokens:

- 256 random bits with a recognizable prefix (`kit_device_`) for secret
  scanning, issued only by redeeming a pairing code.
- Stored in the server database as a SHA-256 hash with a name, creation time,
  last-use time, last remote address, and expiry. Last-use updates are
  throttled to at most once per minute per device.
- Expiry is the earlier of 30 days without use and 1 year after creation.
  Expired requests fail with a distinct expired-credential error code so
  clients can ask the user to pair again. Expired rows are purged in the
  background 30 days after they expire, so clients keep receiving that code
  until then.
- Revocation takes effect immediately and terminates the device's open event
  streams.
- `kit server devices list|revoke` and equivalent API operations manage them.

**Pairing.** Pairing links are the only way to obtain a device token. A
pairing code is single-use, has 128 bits of entropy, and is held only in
server memory. A link combines a reachable server URL with the code as
`https://host/pair#CODE`; the code is in the fragment, so browsers and
proxies never send or log it. An
unauthenticated, rate-limited redeem operation consumes the code and returns
a new device token named by the redeeming client.

The first device obtains a link from the server itself, in one of two ways:

- **`kit server pair`**, run in any shell on the server: its own terminal,
  SSH, or a hosting platform's console. The code expires after 5 minutes.
  The command prints a link for every reachable server URL and a QR payload.
- **Startup output.** A serving server with no unrevoked, unexpired devices
  prints a pairing link in its log output at startup, for deployments where
  only logs are visible. That code expires after 1 hour; each start prints a
  new one until a device is paired.

Any paired device then issues links for further devices through the
authenticated pairing operation: `kit pair <server>` on the command line or
"Pair a device" in the TUI and macOS app. A server is never left without a
pairing path, since its own shell can always run `kit server pair`.

Browser cookie sessions are not part of this decision.

**Future direction: device keys.** Device tokens are bearer secrets. A later
decision may replace them with device key pairs whose private keys stay in
client secure storage, such as the Secure Enclave, and whose public keys the
server stores. The device table, pairing, expiry, and revocation would
remain; only the credential and its verification would change.

### Connectors

A client reaches a server through a connector. Every connector resolves to a
URL, a credential, and the server identity, and can resolve again after a
disconnect or server restart.

| Connector | Resolves through | Credential |
|---|---|---|
| Local | the effective Kit home's discovery registry | local token |
| Home | another Kit home's discovery registry | that home's local token |
| SSH | `kit server connect-info` over SSH, then a loopback port forward | the remote local token |
| URL | a configured HTTP(S) URL | device token |

- The local connector is the only one that may start or replace a daemon.
  Every explicit connector attaches only: it never installs, starts, stops,
  replaces, or migrates a server.
- The URL connector is the primary remote connector. The SSH connector is an
  alternative for hosts reachable over SSH that should not serve an
  endpoint.
- The SSH connector uses the system `ssh` executable so user SSH
  configuration, agents, jump hosts, and private-network SSH apply. It runs
  `kit server start` and `kit server connect-info` on the host, which prints
  the server URL, local token, identity, and protocol version, then holds
  `ssh -N -L 127.0.0.1:<local>:<remote>` for the connection's lifetime.
  Reconnection repeats both steps. Interactive SSH prompts are supported only
  before the TUI takes the terminal or through the macOS app's askpass
  bridge; reconnection requires non-interactive authentication or connection
  sharing.
- The macOS app runs its bundled `kit` executable as the SSH connector helper
  (`kit server tunnel <name>`), which reports resolved endpoints as structured
  output. SSH and reconnection logic exist once, in Go.

The public Go client exposes these as connection targets. Its explicit
endpoint target accepts a credential without an instance identifier.

### Client server registry

Saved servers live in `<home>/servers.json`, shared by the TUI and the macOS
app. It contains only non-secret connector configuration: a name, connector
kind, and its address (SSH destination, Kit home path, or URL). `local` is
implicit and cannot be removed.

Only URL connectors need stored secrets. The macOS app stores device tokens in
Keychain; the TUI stores them in a private, atomically replaced file beside
`servers.json`, separate from provider credentials.

Command surface:

- `kit servers add|list|remove` manages saved servers.
- `kit pair <server>` prints a pairing link for another device from an
  already paired one.
- `kit attach <target>` runs the TUI against one server. A target is a saved
  name, then a pairing link, an HTTP(S) URL, an SSH destination, or a Kit home
  path. Attaching through a pairing link redeems it, saves the server, and
  stores its device token. Without a target it shows a server picker.
- `kit attach <target> --cwd <path>` starts in a server path. Without
  `--cwd`, new sessions on non-local servers start in the server's home
  directory.
- `kit` without `attach` always uses the local connector.

### Client behavior

- A TUI process is attached to exactly one server for its lifetime. Running
  against several servers means running several processes.
- The TUI permanently shows the server name in its chrome when attached to a
  server other than `local`.
- The macOS app may hold several servers concurrently, as ADR 0013 specifies.
  Each session view is bound to one server.
- Session identifiers, session references, peer sessions, and subagents are
  scoped to one server.

### Compatibility

Clients and servers are compatible exactly when their session protocol
versions match. Servers do not support multiple protocol versions.

- Every explicit connector reports a mismatch with both Kit and protocol
  versions and the corrective action: upgrade the server host when the server
  is older, upgrade the client when the server is newer.
- The SSH connector checks the protocol reported by `connect-info` before
  forwarding.
- Clients never replace, downgrade, or upgrade a server reached through an
  explicit connector, and Kit does not install or upgrade itself on remote
  hosts.

### Server and client ownership

The server owns everything that affects sessions or executes on the server
host. Clients own presentation and the client machine.

Server-owned, exposed through the session API:

- **Provider login.** Listing providers and methods, device, browser, and API
  key login, and logout run on the server and store credentials in the
  server's home. For browser login whose redirect targets a loopback callback,
  the client listens on that callback address on its own machine and relays
  the result to the server; manual code entry remains available. Clients do
  not import credential storage.
- **Server settings.** Default and compaction models, model overrides such as
  context windows, prompt cache retention, and whether Claude configuration
  files are read are read and written through the API. New sessions start at
  their model's default thinking level.
- **Working directories.** New-session working directories are server paths,
  validated by the server. On a server that shares the client's machine, a
  new session uses the client's working directory. Otherwise it starts in the
  server user's home directory, or in a path the user names: absolute, or
  starting with `~/` for the server user's home.
- **Path presentation.** Clients abbreviate server paths relative to the
  server-reported `home`.

Client-owned:

- Theme, diff presentation, keybindings, notifications, and clipboard.
- Attachments, which the client reads from its own machine and uploads.
- Attachment and image previews, which the client downloads and opens
  locally.

Routed to the attached client: an MCP server that needs authorization asks the
user through an `authorize` interaction owned by the tool call that is
connecting. The interaction carries the https page to open and the loopback
address the page redirects to. An attached client opens the page on its own
machine, listens on the redirect address when it can, and answers with the
redirect; otherwise the user pastes it. The server never launches a browser or
listens for the redirect, for same-machine and remote servers alike. With no
client attached, the request waits for one, for a bounded time, and the tool
call then fails. Only the session's own conversation can ask; a subagent
borrowing the session's MCP servers cannot. Plugins cannot open URLs
([ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md)).

Same-machine only, hidden otherwise with an explanation: revealing workspace
files in Finder or opening them in local applications, and daemon lifecycle
control.

### Server-hosted terminals

Terminal panes attach to a terminal byte-stream interface. Remote servers
provide it through server-hosted terminals: a terminal lifecycle owned by the
server and scoped to a session workspace, with resize, detach and reattach, and
bounded scrollback, carried over an authenticated bidirectional stream. The
terminal design decides whether same-machine panes also use server-hosted
terminals.

### Delivery

0. **Foundation.** Server identity in health; Host and Origin allowlists with
   loopback literals on any port; `--listen`; connector resolution with
   attach-only explicit connectors, re-resolution, and resubscription; home
   connector; `kit server connect-info`; `servers.json`; `kit attach` for
   saved names and home paths. This enables cross-worktree development.
1. **Serving.** Device tokens with expiry and revocation, pairing through
   `kit server pair`, startup output, and `kit pair`, the URL
   connector and client secret storage, `kit attach` for URLs and pairing
   links, allowed-host and allowed-origin settings, non-loopback listener
   configuration, `kit server expose --tailscale`, new
   sessions on non-local servers starting in the server home, server path
   presentation, and deployment documentation for tunnels, private
   networks, public exposure, and reverse proxies. This enables serving a
   workstation or VPS behind a tunnel or proxy.
2. **Ownership.** Server-side provider login with callback relay, server
   settings API, client-routed open requests, and same-machine gating.
3. **Server-hosted terminals.**
4. **SSH.** SSH connector, `kit attach` for SSH destinations, the macOS
   tunnel helper and askpass bridge.

### Non-goals

- Accounts, multiple users, organizations, or credential scopes.
- A hosted tunnel or relay service.
- TLS termination in Kit.
- Serving more than one session protocol version.
- Browsing the server's filesystem to choose a working directory.
- Installing or upgrading Kit on remote hosts.
- Several servers in one TUI process.
- Browser clients and cookie sessions.

## Consequences

### Positive

- One listener and one request pipeline serve local, forwarded, and exposed
  clients, so local use continuously exercises remote semantics.
- Serving behind a tunnel or reverse proxy is the primary remote model and
  deploys the same way on a workstation, a VPS, or a cloud host.
- SSH remains available as an alternative that requires no served endpoint,
  stored secret, or pairing, and keeps the server off the network.
- Worktree development gets attach-only, restart-tolerant connections to
  isolated daemons without sharing homes or risking daemon replacement.
- Device tokens give a revocable inventory of every durable client, and
  abandoned credentials expire on their own.
- Machine identity makes same-machine affordances correct even through SSH
  forwarding.
- Moving login and server settings behind the API removes client imports of
  server-owned storage.

### Negative

- Exposure settings share the daemon with local clients; changing them
  restarts it.
- Public deployments rely on Kit's own credential checks as the only barrier
  to code execution as the server user.
- Any valid credential, including a device token, can stop the daemon.
- Exact protocol matching forces client and remote servers to upgrade in step
  while the protocol changes frequently.
- Device tokens add a table, a migration, and per-request lookup and
  bookkeeping.
- The SSH connector depends on the system `ssh` executable and cannot prompt
  during an in-TUI reconnect.
- Server-hosted terminals add a bidirectional streaming transport alongside
  SSE.

### Relationship to other decisions

- Supersedes the separate remote listener in ADR 0001's "Single-user remote
  access"; its single-user, token-authenticated, externally terminated TLS, and
  Host/Origin validation requirements remain.
- Refines ADR 0013's remote endpoints: named servers come from the shared
  registry, Keychain holds device tokens, and SSH servers use the bundled
  helper.
- Extends ADR 0038's connection targets with home and SSH resolution and an
  explicit endpoint that learns its instance from health.
