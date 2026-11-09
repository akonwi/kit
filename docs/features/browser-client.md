# Browser client

A Kit server serves a browser client on the same address as its session API
([ADR 0059](../adrs/0059-serve-a-browser-client-from-the-kit-server.md)).
It is for an Android phone, or any browser on a machine without Kit
installed. It needs no install: open a pairing link from the server and pair.

## Opening it

The browser reaches the server the same way any other device does: on a
tailnet, through a reverse proxy or tunnel, or through an SSH port forward
([Remote deployment](remote-deployment.md)).

1. On the server, run `kit server pair`, or use **Pair a Device** in the Mac
   app's server settings or the **pair** command in the TUI.
2. Open the link it prints on the phone, or scan its QR code. The link opens
   the browser client's pairing page.
3. Name the browser and choose **Pair**.

The pairing page removes the code from the address bar and history. A link
works once and expires after 5 minutes ([Pairing devices](pairing.md)).
Opening the server's address without a link shows how to get one.

On a phone, **Add to Home Screen** opens the client without browser
controls. It needs the server to be reachable; it does not work offline.

## What it does

- **Sessions** lists the server's sessions: those waiting on you first, the
  longest-waiting at the top, then running ones, then the rest, most recently
  updated first. **Needs you** shows only sessions waiting on a
  confirmation, a question, or a sign-in. **New session** starts one in a
  folder, with a model, thinking level, and name.
- **A session** shows its transcript as it runs: prompts, replies rendered as
  Markdown, tool calls grouped under each reply, and a line for a turn that
  stopped or failed. The composer sends a prompt; while a turn runs it queues
  follow-ups, which can be edited, cleared, or sent at once, and **Stop** ends
  the turn. A provider retry shows when the next attempt starts.
- When a session needs an answer, the composer gives way to it: approve or
  deny, choose an option, type a value, complete a sign-in, or step through
  guided questions.
- The session menu shows the context used and changes the model and thinking
  level. The model can change only between turns. It also compacts, renames,
  forks, and deletes the session.
- The composer attaches photos and files, and the transcript shows what was
  sent and the images Kit shows. Tap one to view it full screen.
- A message starting with `!` runs a shell command in the session's folder,
  and `!!` keeps its output from the agent. **Stop** ends a running command.
- Replies draw GitHub callouts (`> [!NOTE]`), code blocks with **Copy**,
  colored by the server when it knows their language, data figures (see
  [Figures](../figures.md)), and Mermaid flowcharts and sequence diagrams,
  which open full screen to pan and zoom. A figure or diagram that can't be
  drawn shows its source with the reason.

- **Changes**, in the session menu, shows the session folder's uncommitted
  changes, its branch, or a commit, file by file, colored by the server.
  Tap line numbers to comment on those lines; comments go with the next
  message, as on the Mac, and can be edited or deleted. A comment whose
  lines changed must be moved to new lines or deleted before the message is
  sent.
- **Open File…**, in the session menu, searches the session folder's files.
  A file also opens from a Read or Edit tool call, a row in Changes, and a
  comment. It shows the source, colored by the server, or Markdown as a
  preview, and takes comments on its lines like Changes.
- **Scratchpad**, in the session menu, edits the Markdown notes a session
  shares with its forks and the agent. It saves a moment after you stop
  typing and when you leave. If the shared copy changes while you have
  unsaved edits, your draft stays, and **Review changes** shows both so you
  can keep yours or take the shared copy.
- When the agent starts subagents, a strip under the session's title lists
  Main and each subagent with its status: failed or interrupted ones first,
  then working or queued ones, then idle ones. Tap one, or the agent's name on
  a **Start agent** call, to see its conversation as it works. Its results go
  to the main agent, so a subagent's view has no composer. Its menu changes
  its thinking level, its model once its work finishes, and dismisses it.

When a page returns from the background or the network comes back, it takes
a fresh snapshot of the session. If the server restarts with a different
build, the page reloads once to load the matching client.

## Signing out and security

The browser keeps its device token in the site's local storage. **Sign out**
in the Sessions menu revokes the device on the server and forgets the token.
Revoking the device from elsewhere (`kit server devices revoke NAME`) signs
the browser out on its next request. A device token expires 30 days after its
last use.

The client's pages run under a strict Content Security Policy: scripts, styles,
and fonts come only from the server, and the page connects only to it. Model
output is rendered as text and elements, never as HTML. Links open only for
`http`, `https`, and `mailto`, in a new tab, and images in replies are not
fetched; their description, or else their address, is shown instead. A
Mermaid diagram is drawn as SVG kept to drawing elements, with its labels as
text and without links or images.

Use `https`, or `http` only on a private network such as a tailnet: over plain
`http` to another machine, the pairing code and device token travel
unencrypted.
