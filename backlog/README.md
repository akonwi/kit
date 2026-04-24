# Backlog

This directory tracks outstanding work for the standalone `pi-kit` app.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items

## Ideas
- [ ] fix: regression in composedock
  - session name and `cwd`, `branch` were ontop of the bottom border of the composer border but now are below the composer
  - i think i like it better this way, but need to figure a new place for those items. `cwd` and `git` info can be in the footer on right.
    - what about session name though?
- [ ] refactor: /code-review doesn't need `token` for web ui. it should simply use `sessionId` query key for establishing connection
- [ ] can opentui's [TabSelect](https://opentui.com/docs/components/tab-select/) be used for the pickers?
- [ ] retryable errors should be retryed with a backoff like Pi does.
  - example error: `Codex error: {"type":"error","error":{"type":"server_error","code":"server_error","message":"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID b284c0e7-126b-4163-9e5b-ac7299b50c93 in your message.","param":null},"sequence_number":7}`
