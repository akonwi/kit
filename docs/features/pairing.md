# Pairing devices

A client on the server's machine authenticates with the token the daemon
publishes in its Kit home. A client on another machine needs a **device
token**, which it obtains only by redeeming a **pairing link**.

## Pairing links

```sh
kit server pair
kit server pair --url https://kit.example.com
```

`kit server pair` asks the running server for a pairing code and prints a link
for every URL that reaches it:

- each `server.allowedHosts` entry as `https://HOST`, or as the
  `server.allowedOrigins` entry naming that host when there is one;
- each `--url`, for a reverse proxy or tunnel whose address the settings do not
  name;
- the server's loopback URL, for a client on this machine or one reaching it
  through an SSH port forward.

On a terminal it also draws a QR code of the first link. Run it in any shell
on the server's machine: its own terminal, SSH, or a hosting console. It never
starts the server.

A link looks like `https://kit.example.ts.net/pair#a3k9…`. The code is in the
URL fragment, so browsers and proxies never send or log it. A code works once
and expires after 5 minutes.

A plain `http://` link to another machine carries the code, and then the
device token, unencrypted. `kit server pair` warns about such links; use them
only on a private network such as a tailnet.

### Serving on a tailnet

```sh
kit server expose --tailscale
kit server expose --tailscale --off
```

`kit server expose --tailscale` serves this machine's server on its tailnet
with Tailscale Serve and prints a pairing link for the served URL. It requires
Tailscale to be running with HTTPS certificates enabled for the tailnet. It:

- fixes `server.listen` to a port when none is set or its port is ephemeral,
  keeping its host and the server's current port when one is running;
- adds the machine's tailnet DNS name to `server.allowedHosts`;
- restarts the server when those settings change, or starts it;
- runs `tailscale serve --bg` to proxy `https://NAME.TAILNET.ts.net` to that
  port. `--https-port` chooses another tailnet port.

It never uses Tailscale Funnel, and refuses a port where Funnel is on or that
Tailscale already serves for something else. When `server.listen` is
`0.0.0.0` or `::`, the server also accepts connections on every other
interface, and `expose` says so.

It records what it changed in `$KIT_HOME/tailscale-serve.json`. `--off`
removes that Serve handler and only the settings it added, restoring an
earlier `server.listen`, then restarts the server if it is running. If it
cannot confirm the handler is gone, for example because Tailscale is not
running, it changes nothing and fails; `--off --force` removes the settings
anyway. A server exposed on one port must be stopped with `--off` before it is
exposed on another.

A server started with `kit server run` belongs to whatever started it, such as
a service manager, so `expose` and `--off` update the settings and Serve but
do not restart it; they say to restart it there.

Once restarted with an allowed host and no paired device, the server also
logs a 1-hour [startup link](#first-device-on-a-headless-server). Redeem the
printed link promptly; once a device is paired, later starts log no links.

### First device on a headless server

A server that serves beyond loopback (`server.listen` is not loopback, or
`server.allowedHosts` is set) and has no unexpired devices logs pairing links
when it starts:

```
level=INFO msg="pair a device with this link" link=https://kit.example.ts.net/pair#a3k9… expires=…
```

That code expires after 1 hour. Each start logs a new one until a device is
paired, for deployments whose only visible output is the server log.

## Device tokens

Redeeming a link returns a device token for a device named by the client. A
server keeps at most 1024 devices; past that, pairing fails with
`capacity_exceeded` until one is revoked.
Tokens begin with `kit_device_` so secret scanners recognize them. The server
stores only their SHA-256 hashes.

A token expires 30 days after its last use or one year after pairing,
whichever comes first. An expired token is rejected with the error code
`credential_expired`, so a client can ask to pair again, and requests already
open, including event streams, end when the token expires. Expired devices are
deleted 30 days after they expire.

Every credential has the same authority: a paired device can do anything this
machine's own clients can, including pairing further devices and stopping the
server.

## Managing devices

```sh
kit server devices                 # list
kit server devices revoke laptop   # by name, or by ID
```

The list shows each device's name, ID, last use, last address, and expiry. The
last address is the request's network peer as the server saw it; behind a
proxy it is the proxy's address. Revoking a device stops its token at once and
ends its open requests, including event streams.

## Limits

Failed authentications and failed pairing redemptions are rate-limited per
remote address, or per /64 for IPv6. Past the limit, further failures receive
HTTP 429 with the error code `rate_limited`. A valid credential or pairing code
is never refused, so clients that share an address behind a proxy are not
locked out by one of them.

A request without a valid credential, including a pairing redemption, must
send its body within 10 seconds, so callers cannot hold connections open by
sending slowly. A server holds at most 1024 devices; redeeming a code past that
fails with `capacity_exceeded` until devices are revoked or expire.

## Session API

| Operation | Credential |
| --- | --- |
| `POST /v1/pairing/redeem` | none; redeems a code for a token |
| `POST /v1/pairing` | any; issues a 5-minute code |
| `GET /v1/devices` | any; marks the requesting device `current` |
| `DELETE /v1/devices/{deviceID}` | any |

Go clients use `kit.Pair(ctx, link, name)`, then
`kit.Connect(ctx, kit.Endpoint(paired.ServerURL, kit.AccessToken(paired.Token)))`,
and `Client.CreatePairing`, `Client.Devices`, and `Client.RevokeDevice`.

For serving a server to other machines, see
[Remote deployment](remote-deployment.md). To pair the Kit CLI on another
machine and attach to this server, see
[Attaching to other servers](attach.md#servers-at-a-url). A paired machine can
print links for further devices with `kit pair NAME`. In the TUI, the **pair**
command in the command palette issues a code on the attached server and shows
a copyable link for each URL that reaches it.
