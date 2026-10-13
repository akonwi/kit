# Server listen address and allowed hosts

The Kit daemon listens on loopback on an ephemeral port by default. Local
clients find it through the discovery registry in `$KIT_HOME/run`, so the port
does not matter to them. A fixed address is useful when something else must
point at the daemon across restarts, such as a tunnel or reverse proxy, or a
client attaching from another development worktree.

## Settings format

Set `server.listen` in `$KIT_HOME/settings.json`:

```json
{
  "server": {
    "listen": "127.0.0.1:7777"
  }
}
```

The daemon reads the setting when it starts. Run `kit server restart` after
changing it.

`kit server start --listen <host:port>` and `kit server restart --listen
<host:port>` override the setting for that daemon process. When a daemon is
already running, `kit server start --listen` succeeds only if the daemon serves
the requested address; otherwise it reports the mismatch and leaves the daemon
running. A daemon started later without the flag uses the setting again.

## Accepted addresses

The address is `host:port` with an IP address host:

- a loopback address, such as `127.0.0.1` or `[::1]`;
- an unspecified address, `0.0.0.0` or `[::]`, which also accepts connections
  from other machines.

Hostnames such as `localhost` and specific non-loopback addresses are rejected
so local clients can always reach the daemon over loopback. Port `0` selects an
ephemeral port.

An invalid setting is ignored with a warning in the daemon log, and the daemon
listens on the default address. An invalid `--listen` value is an error.

## Allowed hosts and origins

The daemon rejects requests whose `Host` header does not name it, which protects
it from DNS rebinding. It always accepts loopback IP addresses on any port, so
clients reaching it through an SSH port forward work without configuration. A
tunnel or reverse proxy sends its own hostname instead; list it in
`server.allowedHosts`:

```json
{
  "server": {
    "listen": "127.0.0.1:7777",
    "allowedHosts": ["studio.tail3f2a.ts.net"]
  }
}
```

Entries are DNS names or IP addresses without a port and match on any port.
Names are compared case-insensitively.

Browsers also send an `Origin` header. The daemon accepts an origin naming the
request's own host, under `http` or `https` because a proxy may terminate TLS.
Other origins must be listed in `server.allowedOrigins` as
`scheme://host[:port]`. Requests without an `Origin` header, such as those from
the TUI and macOS app, are not affected.

Kit does not trust `Forwarded` or `X-Forwarded-*` headers. Invalid entries are
ignored with a warning in the daemon log. Run `kit server restart` after
changing either list.

Binding beyond loopback and allowing hosts do not by themselves make Kit usable
remotely: Kit does not terminate TLS, and remote clients need credentials. See
[ADR 0045](../adrs/0045-connect-clients-to-local-ssh-and-network-servers.md).
