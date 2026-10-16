# MCP authorization

An MCP server configured with OAuth needs the user to approve access in a
browser the first time Kit connects to it, and again if its saved credential
is rejected. Kit asks through the client attached to the session, so the
browser opens on the user's machine whether the Kit server runs there or
elsewhere.

## Flow

1. A tool call uses the MCP server, and the server asks for authorization.
2. The session shows an **Authorize the _name_ MCP server** request in place of
   the composer, with the page to open. The tool call shows that it is waiting.
3. Open the page: press Return with the field empty in the terminal client, or
   choose **Open** (the link is also clickable).
4. Approve access. The page redirects the browser to
   `http://127.0.0.1:53173/mcp/callback`.
   - The terminal client listens on that address on its own machine and
     answers with the redirect, so Kit continues on its own.
   - If it can't listen there (another program has the port), or in the macOS
     app, the browser shows an error page. Copy its address and paste it into
     the field, then submit. Paste the whole address, not only the code.
   - If another terminal client on this machine is waiting for its own
     authorization, it holds that address, and the page says it is not the
     redirect Kit is waiting for. Copy the page's address and paste it.
5. Kit exchanges the code on the server and saves the credential in the
   server's home. The tool call continues.

Cancelling the request fails the tool call. Kit checks that a received or
pasted redirect belongs to this authorization before using it.

## Limits

- The request waits up to five minutes, including while no client is
  attached. A client that attaches in that time shows it. After that the tool
  call fails, and using the server again starts a new authorization.
- Only the session's own conversation can ask. A subagent using its session's
  MCP servers fails with an explanation, and keeps any saved credential; use
  the server from the session first.
- [Print mode](print-mode.md) cannot answer the request.
- The server never opens a browser and never listens for the redirect.
- The page must be https.
