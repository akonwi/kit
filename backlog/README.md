# Backlog

This directory tracks outstanding work for the standalone `pi-kit` app.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items

## Ideas
- [ ] can opentui's [TabSelect](https://opentui.com/docs/components/tab-select/) be used for the pickers?
- [ ] retryable errors should be retryed with a backoff like Pi does.
  - example error: `Codex error: {"type":"error","error":{"type":"server_error","code":"server_error","message":"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID b284c0e7-126b-4163-9e5b-ac7299b50c93 in your message.","param":null},"sequence_number":7}`
- [ ] model thining level doesn't seem to restore correctly when reopening a session
- [ ] Design language refresh or refinement
- [ ] theming
- [ ] fix: using custom prompts during a turn should do normal follow-up/quue behavior
  - current error: `Error: agent is already processing a prompt. Use steer() or followUp() to queue message, or wait for completion.`
- [ ] bash commands should enter the transcript immediately and show loading state - like tool calls
