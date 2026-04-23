# Decision: Defer inline transcript image rendering

- Status: Accepted
- Date: 2026-04-23
- Scope: transcript rendering, image attachments, shell UI

## Summary

`kit` should **not** render image attachments inline in the transcript for now.

Instead:

- image attachments remain first-class multipart user message parts
- pending image attachments remain supported in the composer flow
- sent image attachments should render as compact transcript summary rows
- richer image display is deferred

## Why

An investigation into Pi's TUI implementation showed that its inline terminal image support depends on **renderer-level image handling**, not just an image component.

Pi's TUI layer includes support for:

- terminal image capability detection
- image-aware row reservation
- image-line detection
- skipping normal width checks for image lines
- avoiding normal reset/composition behavior on image lines

`kit`'s current shell stack does not provide an equivalent, proven rendering path.

A prior attempt to add inline transcript image previews destabilized:

- transcript layout
- scrolling
- composer/footer behavior

That makes inline transcript image rendering too risky relative to its current product value.

## Decision

Keep transcript image rendering minimal and stable.

Current policy:

- composer image attachments are supported
- transcript displays sent image attachments as compact summary entries
- the transcript does not attempt inline image preview rendering

## Consequences

### Near-term UX

Users can:

- paste or drag images into the composer
- send images as multipart user message content
- see stable transcript entries confirming image attachment presence

Users cannot currently:

- view image pixels inline in the transcript

### Implementation guidance

Do not reintroduce transcript inline image rendering unless we first have a shell rendering contract that safely supports image lines.

That likely requires one or more of:

- renderer-aware image line handling
- explicit row reservation for terminal image output
- safe width/scroll/composition behavior for image rows
- validated terminal capability handling

### Deferred work

Possible future directions remain open, but deferred:

- dedicated image viewer overlay
- isolated modal/pager-style image presentation
- renderer-level support for inline transcript image rows

## Notes from Pi investigation

Pi's interactive app appears to use inline image rendering primarily for **tool result images**, backed by its own TUI image infrastructure.

We did **not** find a ready-made Pi pattern for generic user image attachment rendering inside normal chat transcript message components.

So the useful lesson from Pi is architectural:

- inline transcript images require renderer support

not simply:

- attach an image component inside the transcript tree
