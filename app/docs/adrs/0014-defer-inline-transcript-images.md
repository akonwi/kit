# 0014: Defer inline transcript image rendering

## Status
Accepted

## Context

Kit supports image attachments in the composer and in multipart user messages, but inline transcript image rendering requires stronger rendering guarantees than the current shell stack provides.

Without a safe rendering contract, inline image rows can destabilize transcript layout, scrolling, and surrounding shell behavior.

## Decision

Do not render image attachments inline in the transcript for now.

Current policy:

- composer image attachments remain supported
- sent image attachments render as compact transcript summary rows
- the transcript does not attempt inline image preview rendering

## Rationale

Inline transcript image rendering should not be reintroduced until the shell has a safe rendering contract for image output.

That likely requires one or more of:

- renderer-aware image line handling
- explicit row reservation for terminal image output
- safe width, scroll, and composition behavior for image rows
- validated terminal capability handling

## Consequences

### Near-term UX

Users can:

- paste or drag images into the composer
- send images as multipart user message content
- see stable transcript entries confirming image attachment presence

Users cannot currently:

- view image pixels inline in the transcript

### Implementation guidance

Do not reintroduce inline transcript image rendering until the rendering contract above exists.

### Deferred directions

Possible future directions remain open, including:

- dedicated image viewer overlay
- isolated modal or pager-style image presentation
- renderer-level support for inline transcript image rows

## Related

- `docs/adrs/0003-custom-shell.md`
