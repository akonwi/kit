# Remote deployment

A Kit server can serve clients on other machines: a workstation you reach from
a laptop, a VPS, or a cloud host. Clients on the server's own machine need none
of this; see [Attaching to other servers](attach.md) for Kit homes on one
machine.

Serving takes three things:

1. **A reachable listener.** The server listens on loopback by default. A
   tunnel or proxy on the same machine forwards to a fixed loopback port; a
   proxy on another host needs `0.0.0.0`. See
   [Server listen address](server-listen.md).
2. **An allowed host.** The server rejects requests whose `Host` header does
   not name it. List the name clients use in `server.allowedHosts`.
3. **TLS in front.** Kit does not terminate TLS. A private network, tunnel, or
   reverse proxy provides HTTPS and forwards plain HTTP to the listener.

Remote clients then [pair](pairing.md) to get a device token.

A host you already reach with `ssh` needs none of this: `kit attach
user@host` starts the host's server and forwards a port to it over SSH. See
[Servers over SSH](attach.md#servers-over-ssh).

## Choosing a deployment

| Deployment | Who can reach the server | Recommended for |
| --- | --- | --- |
| Private network, such as a tailnet | members of the network | most setups |
| Tunnel or reverse proxy with access rules | whoever the rules admit | existing infrastructure |
| Public internet | anyone | only when the others are impossible |

**Private networks are recommended.** Network membership gates reachability
before Kit checks credentials, and `kit server expose --tailscale` sets one up
in one command.

**On the public internet, Kit's credentials are the only barrier to code
execution as the server user.** Anyone holding a valid device token, or an
unredeemed pairing code, can run commands on the server. TLS is required: a
plain `http://` URL sends pairing codes and device tokens in cleartext. Kit
rate-limits failed authentication and pairing, but it has no accounts, scopes,
or second factor. Tailscale Funnel and a public reverse proxy are both public
exposure.

## Running the server

On a workstation, `kit server start` runs the server as a background daemon
that logs to `$KIT_HOME/logs/server.log`, and `kit server stop` and
`kit server restart` manage it. Running `kit` also starts it when needed.

For a service manager or container, `kit server run` runs the server in the
foreground and logs to stderr. It reads the same settings and accepts the same
`--listen` override as `kit server start`. It exits cleanly on SIGINT or
SIGTERM, or when `kit server stop` is run from another shell. It refuses to
start while a server is already running for the same Kit home.

Kit never restarts a foreground server, which would move it out of whatever
started it: `kit server restart` refuses, and `kit server expose` only updates
settings and says to restart it. Restart it with its service manager, such as
`systemctl restart kit`.

A systemd unit on a Linux host:

```ini
[Unit]
Description=Kit server
After=network-online.target

[Service]
User=me
ExecStart=/usr/local/bin/kit server run
Restart=on-failure
# Provider keys, if not logged in with `kit` on this machine.
EnvironmentFile=-/home/me/.config/kit.env

[Install]
WantedBy=multi-user.target
```

### Provider credentials

The server, not the client, calls model providers, so credentials belong on
the server. Use one of:

- the terminal client attached to the server, which offers the providers'
  logins when the server has no credentials and runs them on the server;
- `kit auth login openai-codex` in a shell on the server, a device-code login
  that works over SSH;
- `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `OPENCODE_API_KEY` in the server's
  environment.

Restart the server after changing environment variables. See
[Provider login](provider-login.md).

## Tailscale

```sh
kit server expose --tailscale
```

This fixes `server.listen` to a loopback port, adds the machine's tailnet name
to `server.allowedHosts`, runs `tailscale serve`, and prints a pairing link for
`https://NAME.TAILNET.ts.net`. It requires Tailscale to be running with HTTPS
certificates enabled for the tailnet, and never uses Funnel.
`kit server expose --tailscale --off` undoes it. See
[Serving on a tailnet](pairing.md#serving-on-a-tailnet).

The manual equivalent is:

```json
{
  "server": {
    "listen": "127.0.0.1:7777",
    "allowedHosts": ["studio.tail3f2a.ts.net"]
  }
}
```

```sh
kit server restart
tailscale serve --bg --https=443 http://127.0.0.1:7777
kit server pair
```

## Cloudflare Tunnel

Run `cloudflared` on the server's machine and route a hostname to the loopback
port:

```yaml
# ~/.cloudflared/config.yml
tunnel: kit
credentials-file: /home/me/.cloudflared/kit.json
ingress:
  - hostname: kit.example.com
    service: http://127.0.0.1:7777
  - service: http_status:404
```

```json
{
  "server": {
    "listen": "127.0.0.1:7777",
    "allowedHosts": ["kit.example.com"]
  }
}
```

A tunnel hostname is on the public internet unless an access policy restricts
it. Kit clients do not send Cloudflare Access credentials, so an Access policy
that requires a login also blocks them.

## Reverse proxies

A reverse proxy terminates TLS and forwards to the listener. It must:

- pass the original `Host` header through, and list that name in
  `server.allowedHosts`;
- not buffer responses, because session events are a server-sent event stream;
- allow idle connections of at least 15 seconds, the event stream's heartbeat;
- accept request bodies of at least 20 MiB, the largest prompt attachment.

Caddy does all of this by default:

```
kit.example.com {
	reverse_proxy 127.0.0.1:7777
}
```

nginx needs:

```nginx
server {
    listen 443 ssl;
    server_name kit.example.com;
    # ssl_certificate and ssl_certificate_key ...

    client_max_body_size 25m;

    location / {
        proxy_pass http://127.0.0.1:7777;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

Kit does not trust `Forwarded` or `X-Forwarded-*` headers, so rate limits and
each device's last address use the proxy's address. A browser client on another
origin also needs `server.allowedOrigins`; the TUI and macOS app send no
`Origin` header.

## Cloud hosts

Cloud hosts take one of three shapes. Check your host's documentation for its
ports, hostnames, and how it proxies traffic.

**A machine you control** (a VPS, or a VM from a service such as exe.dev or
freestyle). Install Kit, run it under a service manager with `kit server run`,
and serve it the same way as a workstation. Installing Tailscale on the
machine and running `kit server expose --tailscale` keeps it off the public
internet.

**A VM behind the host's HTTPS proxy.** Some hosts give each VM an HTTPS URL
that proxies to one of its ports. Point that at the Kit listener: a loopback
port if the proxy connects from inside the VM, or `0.0.0.0` with a fixed port
if it connects over the VM's network. List the URL's hostname in
`server.allowedHosts`. Unless the host restricts who can reach that URL, this
is public exposure.

**A container platform** (such as Zeabur). The platform routes its HTTPS
hostname to a port in the container:

- run `kit server run --listen 0.0.0.0:PORT` as the container's command, with
  the port the platform routes to;
- mount a persistent volume as `KIT_HOME`, and write `settings.json` there with
  the platform hostname in `server.allowedHosts`. Without a volume, every
  deploy loses sessions, paired devices, and provider credentials;
- set provider API keys as environment variables;
- include the tools sessions need, such as `git`, in the image.

A container platform URL is public unless the platform restricts it.

### The first pairing link

A server with no paired devices that serves beyond loopback logs a pairing
link at startup, valid for 1 hour:

```
level=INFO msg="pair a device with this link" link=https://kit.example.com/pair#a3k9… expires=…
```

This is the only way to pair a server whose only visible output is its log.
**Anyone who can read the log while the link is valid can pair a device and
run code on the server.** On hosts where others can read logs, such as shared
team dashboards or forwarded log drains:

- prefer `kit server pair` in the host's shell or console, which prints a
  5-minute link without logging it;
- otherwise redeem the startup link promptly. A code works once, and once a
  device is paired, later starts log no links;
- afterwards, run `kit server devices` on the server and revoke any device you
  do not recognize.

## Pairing and devices

On the server:

```sh
kit server pair                              # links for every served URL
kit server pair --url https://kit.example.com # a URL the settings do not name
kit server devices                           # paired devices
kit server devices revoke laptop             # revoke by name or ID
```

On the client:

```sh
kit servers add studio 'https://kit.example.com/pair#a3k9…'  # pair and save
kit attach studio                                            # run the TUI
kit attach studio --cwd ~/src/app                            # in a server path
kit pair studio                                              # link for another device
```

New sessions on a server on another machine start in the server user's home
directory unless `--cwd` names a server path. Device tokens expire after 30
days unused or one year after pairing; an expired or revoked token shows the
server as needing pairing. Revoking a device ends its open requests at once.
See [Pairing devices](pairing.md) and
[Attaching to other servers](attach.md).

## Checklist

- [ ] `server.listen` is a fixed port, loopback unless a proxy on another host
      must reach it.
- [ ] `server.allowedHosts` names every hostname clients use.
- [ ] Clients reach the server over HTTPS, or a private network.
- [ ] The server has provider credentials.
- [ ] `KIT_HOME` persists across restarts and deploys.
- [ ] Paired devices are ones you recognize.
