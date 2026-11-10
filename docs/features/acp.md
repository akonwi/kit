# Agent Client Protocol

`kit acp` lets an editor that speaks the [Agent Client
Protocol](https://agentclientprotocol.com) (ACP) use Kit as its agent. The
editor starts `kit acp` itself and talks to it over stdin and stdout
([ADR 0063](../adrs/0063-serve-the-agent-client-protocol-from-kit-acp.md)).

```sh
kit acp          # persisted sessions
kit acp --temp   # temporary sessions, discarded when closed or when kit acp exits
```

An editor's threads are ordinary Kit sessions on this machine's server, which
`kit acp` starts when it isn't running. A thread started in the editor shows
up in the TUI and the macOS app, and a session started there can be opened in
the editor. Kit's own tools, plugins, skills, prompt commands, and models are
available, and Kit's tools read and write files on disk.

## Zed

Add Kit as a custom agent in Zed's `settings.json`:

```json
{
  "agent_servers": {
    "Kit": {
      "type": "custom",
      "command": "/opt/homebrew/bin/kit",
      "args": ["acp"]
    }
  }
}
```

Use the path `which kit` prints. To run against a Kit home other than
`~/.kit`, set it in the agent's environment:

```json
"env": { "KIT_HOME": "/Users/me/.kit-dev" }
```

Then pick **Kit** in the Agent Panel's agent selector. Other ACP clients are
configured the same way: run `kit acp` as the agent command.

## What the editor sees

- **Messages and thinking** stream as the model writes them. Each tool call
  shows its status, the files it touches, and its output; an edit or write
  shows its change as a diff.
- **Model and thinking level** are the session's config options. Changing one
  changes the Kit session, as choosing a model in the TUI does.
- **Commands**: the session's prompt commands and `/compact` are offered as
  slash commands.
- **History**: the editor's thread list shows this server's persisted
  sessions, and opening one replays its transcript.
- **Other clients' turns**: when the TUI or another client runs a turn on an
  open session, the editor shows it too. A prompt sent from the editor while
  that turn runs is refused; send it again when the turn ends.
- **Questions**: a confirmation the model asks for becomes the editor's
  permission prompt. Text questions, choices, and multi-question forms become
  the editor's forms when it supports them. A question the editor cannot
  show, and an MCP server's authorization request, wait for another Kit
  client, such as the TUI, to answer.
- **Attachments**: images and embedded files are attached to the prompt. A
  model that doesn't accept images receives a note that the image was left
  out. Linked files are passed as paths for the model to read.

## Before first use

Provider logins belong to the Kit server. If no provider is logged in, the
editor shows an error asking you to log in; run `kit` and use `/login`, or
`kit auth login <provider>`, then start a new thread.

## Limitations

- **MCP servers configured in the editor are not used.** Kit uses the MCP
  servers configured in Kit. When an editor passes MCP servers for a session,
  `kit acp` writes a line to stderr naming the ignored servers and, when the
  editor supports notices, shows the editor a notice naming them.
- `kit acp` connects only to this machine's server, not to servers saved with
  `kit servers add`.
- Threads cannot be deleted from the editor; use `kit sessions` or another
  Kit client.
- Kit doesn't use the editor's terminals or unsaved buffers: commands run in
  Kit's tool environment and edits go to files on disk.

## Diagnostics

Stdout carries only protocol messages; logs and diagnostics go to stderr. In
Zed, **dev: open acp logs** in the command palette shows the messages Zed and
Kit exchange.
