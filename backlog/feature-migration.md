# Feature Migration Backlog

These are features to port or redesign from the current extension-based `pi-kit`.

## File references [done]

- [x] Lazy file scanner with `.gitignore` and `.pi-ignore` support
- [x] Fuzzy scoring (exact > prefix > substring > subsequence)
- [x] `@` trigger in composer opens filterable file picker
- [x] File index auto-invalidates every 5 tool completions
- [x] Backspace re-trigger prevention (growth-only + suppression after dismiss)

## Thread references [done]

- [x] `@@` trigger in composer opens filterable thread picker
- [x] `[[thread:id]]` token inserted on selection
- [x] Token expansion on submit — reads referenced session context, injects formatted reference block
- [x] Thread index invalidates on session changes

## Bash execution

- [x] `!command` prefix runs a shell command and adds output to session context (as a user-initiated tool result)
- [x] `!!command` prefix runs a shell command but does NOT add output to session context (fire-and-forget)
- [x] Detect prefix in composer submit path (before normal message handling)
- [x] Display command output in transcript for `!` commands
- [x] Display command output transiently (or in a panel) for `!!` commands

## Pager

- [ ] Define pager screen contract in the new shell
- [ ] Port long-form section splitting logic
- [ ] Port note/review interactions where still useful

## Wizard / questionnaire

- [ ] Define wizard dock/screen model
- [ ] Port questionnaire normalization behavior
- [ ] Support guided question flows in the custom shell

## Handoff

- [ ] Port handoff summary builders
- [ ] Support child thread creation / continue-in-new-thread flows


