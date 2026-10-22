# Attaching to other servers

`kit` runs the terminal client for the server of its own Kit home, starting the
server when needed. `kit attach` runs it for another server instead: the
server of another Kit home on this machine, such as a development worktree's
isolated home, a server served at a URL on another machine, or the server of
a host you reach over SSH.

```sh
kit attach ~/.kit-dev                         # a Kit home path
kit servers add dev ~/.kit-dev                # save it under a name
kit servers add vps https://kit.example.ts.net/pair#a3k9…   # pair and save
kit attach me@devbox                          # a host over SSH
kit servers add devbox me@devbox              # save it under a name
kit attach vps --cwd ~/src/app                # attach by name
kit attach                                    # pick from the saved servers
```

Attaching never stops or replaces a server, and starts one only on a host
reached [over SSH](#servers-over-ssh). When a home's server is
not running, `kit attach` reports how to start it, as with
`KIT_HOME=~/.kit-dev kit server start`. An incompatible server is refused; the
client and server must speak the same session protocol version. The client
follows the server across restarts, and fails rather than talking to a
different server that appears at the same address.

`local` names this home's own server; `kit attach local` behaves like `kit`.

## Servers at a URL

A server on another machine is reached at the URL it is served at, such as a
tailnet or reverse-proxy address, with a device token obtained by
[pairing](pairing.md). [Remote deployment](remote-deployment.md) covers
serving a server:

1. On the server's machine, run `kit server pair`. It prints pairing links.
2. On this machine, run `kit servers add NAME LINK`. This redeems the link,
   names this device after its hostname on the server, saves the server under
   NAME, and stores the device token.
3. Run `kit attach NAME`, or `kit attach URL`.

A paired device can pair another with `kit pair NAME`, which prints a link at
the saved URL, or with **pair** in the TUI's command palette, which shows the
links for the attached server with copy actions.

When the server revokes this device, or its token expires, `kit servers` shows
`needs pairing` and `kit attach` explains how to pair again. Get a new link
with `kit server pair` on the server and run `kit attach LINK`; attaching
through a link for a saved server's URL pairs it again and attaches. Pairing
again while the old token still works revokes the old device, so each machine
keeps one device on the server.

Redeeming a plain `http://` link to another machine prints a warning: the
pairing code and device token cross the network unencrypted unless the
network itself is private.

### Working directory

On a server that shares this machine, new sessions use the shell's working
directory or `--cwd`, as with `kit`. On a server on another machine, new
sessions start in the server user's home directory, or in `--cwd`, which names
a directory on that machine: an absolute path, or one starting with `~/` for
the server user's home directory.

Paths on the attached server, in the footer and in session lists, are shown
relative to the server user's home directory, so `~` always means the
server's home.

### Provider credentials

A server uses its own provider credentials. When it has none for the model in
use, the terminal client offers the providers' logins, which run on the
attached server and store credentials there. See
[Provider login](provider-login.md).

### Server settings

New sessions start with the server's default model, and
context-window overrides edited in the `/model` picker change the attached
server's settings. See [Server settings](server-settings.md).

### MCP authorization

An MCP server that needs OAuth asks through the attached client, which opens
the authorization page on its own machine and relays the browser's redirect
to the server. See [MCP authorization](mcp-authorization.md).

## Servers over SSH

A host you can reach with `ssh` needs no served URL, pairing, or stored
secret. `kit attach user@host` and `kit attach ssh://host:2222` attach over
SSH. A bare host or an alias from `~/.ssh/config` works once saved, as with
`kit servers add devbox devbox`, or written as `ssh://devbox`; an unsaved bare
name is reported as an unknown server, so a mistyped name never connects
anywhere. Kit runs your system `ssh`, so your SSH configuration, agent, and
jump hosts apply.

1. Kit runs `kit server start` and `kit server status --json` on the host in
   one SSH command, which starts the server of the host's default Kit home if
   it isn't running and reports its address and local token.
2. It checks that the host's Kit speaks the same session protocol, and
   otherwise says which side to upgrade.
3. It forwards a free loopback port on this machine to the server and
   attaches through it.

`kit` must be on the host's `PATH` for commands run over SSH, which read fewer
shell startup files than a login does. If it isn't, Kit says so; add its
directory to `PATH` in a file such as `~/.zshenv` or `~/.bashrc`, above any
line in it that returns early for non-interactive shells. Some SSH servers run
commands without reading any startup file; on those hosts, install `kit` in or
link it into a directory already on the system `PATH`, such as
`/usr/local/bin`.

`ssh` may ask for a password, passphrase, or host key confirmation while Kit
connects, before the terminal client starts; a password login asks twice, once
for the status check and once for the port forward. When the connection drops
or the host's server restarts, Kit connects again without prompting, which
needs an SSH agent, a key without a passphrase, or connection sharing
(`ControlMaster`). Otherwise it reports the failure. Reconnecting never starts
the server, so a server stopped on its host stays stopped.

`kit servers` checks SSH servers without starting them or prompting. Pairing
another device through an SSH server needs a URL that reaches it:
`kit pair NAME --url URL`, or `kit server pair` on the host.

### In Kit for Mac

Kit for Mac reaches saved SSH servers itself, with your system `ssh`; it
doesn't need `kit` installed on the Mac. Add Server takes `user@host` or an
`ssh://` URL and starts the host's Kit server if it isn't running. The app
keeps one connection per host for every window and connects saved servers
when it opens.

When ssh asks something, such as a new host key, a password, a key
passphrase, or a one-time code, the window you're using shows it as a sheet.
A connection running in the background never asks: the server shows **Sign
in to connect** until you open it. Saving a password puts it in the
Keychain; saving a passphrase hands it to `ssh-add --apple-use-keychain`;
one-time codes are never saved. A changed host key is never accepted from
Kit: the tab shows the saved and new fingerprints and the `ssh-keygen -R`
command to run in Terminal. When the host's Kit server isn't running, the tab
offers **Start Server**; reconnecting never starts it.

## Saved servers

`kit servers` lists this home's server and the saved servers with their status:
`running`, `not running`, `needs pairing`, `incompatible`, or `unavailable`,
followed by the reason where one applies. `kit servers add NAME LOCATION`
saves a Kit home's server or an SSH destination, or pairs with a link and
saves its server. A location that is not a link, a URL, or a path to an
existing directory is an SSH destination.
`kit servers remove NAME` forgets a server and its device token. Names are 1
to 32 lowercase letters, digits, `.`, `-`, or `_`, starting with a letter or
digit.

Saved servers are stored in `$KIT_HOME/servers.json`, which holds no secrets:

```json
{
  "servers": [
    { "name": "dev", "kind": "home", "home": "/Users/me/.kit-dev" },
    { "name": "devbox", "kind": "ssh", "destination": "me@devbox" },
    { "name": "vps", "kind": "url", "url": "https://kit.example.ts.net" }
  ]
}
```

Device tokens are stored by server name in `$KIT_HOME/server-tokens.json`,
which only its owner can read and which is separate from provider
credentials. Kit refuses to read it if other users can. Each token is bound
to the URL that issued it and is only sent to that URL.

The attach picker offers this home's server, which starts on attach, and the
saved servers that are running. Others are listed with why they cannot be
picked.

## In the terminal client

The footer names the attached server before the working directory, as
`dev · ~/src/app (main*)`. The directory shortens first; the name is kept
while anything else fits, including when a plugin hides the location.

Provider logins store credentials on the attached server. Settings and themes
always come from the client's own home.
