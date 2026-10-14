# Attaching to other servers

`kit` runs the terminal client for the server of its own Kit home, starting the
server when needed. `kit attach` runs it for another server instead, such as the
server of a development worktree's isolated home.

```sh
kit attach ~/.kit-dev              # a Kit home path
kit servers add dev ~/.kit-dev     # save it under a name
kit attach dev                     # attach by name
kit attach                         # pick from the saved servers
kit attach dev --cwd ~/src/app     # the usual session flags apply
```

Attaching never starts, stops, or replaces a server. When the server is not
running, `kit attach` reports how to start it, as with
`KIT_HOME=~/.kit-dev kit server start`. An incompatible server is refused; the
client and server must speak the same session protocol version. The client
follows the server across restarts, and fails rather than talking to a
different server that appears at the same home.

`local` names this home's own server; `kit attach local` behaves like `kit`.

## Saved servers

`kit servers` lists this home's server and the saved servers with their status:
`running`, `not running`, `incompatible`, or `unavailable`, followed by the
reason for the last two. `kit servers add NAME HOME` saves a server and
`kit servers remove NAME` forgets one. Names are 1 to 32 lowercase letters,
digits, `.`, `-`, or `_`, starting with a letter or digit.

Saved servers are stored in `$KIT_HOME/servers.json`, which holds no secrets:

```json
{
  "servers": [
    { "name": "dev", "home": "/Users/me/.kit-dev" }
  ]
}
```

The attach picker offers this home's server, which starts on attach, and the
saved servers that are running. Others are listed with why they cannot be
picked.

## In the terminal client

The footer names the attached server before the working directory, as
`dev · ~/src/app (main*)`. The directory shortens first; the name is kept
while anything else fits, including when a plugin hides the location.

A server attached by Kit home runs on this machine, so new sessions use the
shell's working directory, or `--cwd`. Provider login stores credentials in
the attached server's home. Settings and themes come from the client's own
home.
