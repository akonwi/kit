# Server listen address

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

Binding beyond loopback does not by itself make Kit usable remotely: requests
must also pass Host and Origin validation, and Kit does not terminate TLS. See
[ADR 0045](../adrs/0045-connect-clients-to-local-ssh-and-network-servers.md).
