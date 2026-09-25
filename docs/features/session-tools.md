# Session tools

Persistent top-level sessions can discover and communicate with peers through
`peer_session` and create new peers through `create_session`.

## Creating a session

`create_session` requires an existing absolute working directory and a name. It
optionally accepts an initial prompt:

```json
{
  "cwd": "/absolute/path/to/project",
  "name": "Investigate API",
  "prompt": "Inspect the API boundary and report your findings."
}
```

The new session is persistent and independent. Creating it does not switch the
current client. If `prompt` is supplied, Kit admits it asynchronously and
returns the run ID. The session can then be discovered or contacted with
`peer_session`.

Kit always uses `defaultModel` from settings when it is available, followed by
Kit's default available model. Thinking uses the selected model's default. The
model cannot override either setting through this tool or create a temporary
session.
