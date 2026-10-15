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

The new session's model and thinking level follow the server's
[new-session defaults](server-settings.md#new-sessions): `defaultModel` when
usable, otherwise Kit's default model, at the model's default thinking level.
The model cannot
choose either through this tool or create a temporary session.
