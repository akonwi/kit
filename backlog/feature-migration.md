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

## Inline composer picker [deferred]

- [ ] Redesign `@` and `@@` to work inline in the composer (like v1 extension)
- [ ] Typing stays in composer, picker is an overlay that updates on each keystroke
- [ ] `@@` seamlessly replaces `@` picker without requiring escape

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


