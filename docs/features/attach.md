# Attaching to other servers

`kit` runs the terminal client for the server of its own Kit home, starting the
server when needed. `kit attach` runs it for another server instead: the
server of another Kit home on this machine, such as a development worktree's
isolated home, or a server served at a URL on another machine.

```sh
kit attach ~/.kit-dev                         # a Kit home path
kit servers add dev ~/.kit-dev                # save it under a name
kit servers add vps https://kit.example.ts.net/pair#a3k9…   # pair and save
kit attach vps --cwd ~/src/app                # attach by name
kit attach                                    # pick from the saved servers
```

Attaching never starts, stops, or replaces a server. When a home's server is
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

## Saved servers

`kit servers` lists this home's server and the saved servers with their status:
`running`, `not running`, `needs pairing`, `incompatible`, or `unavailable`,
followed by the reason where one applies. `kit servers add NAME HOME-OR-LINK`
saves a Kit home's server, or pairs with a link and saves its server.
`kit servers remove NAME` forgets a server and its device token. Names are 1
to 32 lowercase letters, digits, `.`, `-`, or `_`, starting with a letter or
digit.

Saved servers are stored in `$KIT_HOME/servers.json`, which holds no secrets:

```json
{
  "servers": [
    { "name": "dev", "kind": "home", "home": "/Users/me/.kit-dev" },
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
