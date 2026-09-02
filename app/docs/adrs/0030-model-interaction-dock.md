# 0030: Present model interactions in the composer dock

## Status

Accepted

## Context

Model and plugin interaction APIs can ask for confirmation, short input, a
selection, or a guided questionnaire after explaining the decision in the
transcript. Presenting those requests as centered modal dialogs dims and covers
the context the user needs to answer. It also prevents normal transcript
scrolling and encourages callers to duplicate their explanation inside the
dialog.

## Decision

The interactive TUI presents `confirm`, `input`, `select`, and guided-question
requests in an interaction dock that temporarily replaces the composer and its
pending-status slot.

The dock:

- spans the primary transcript column without a modal backdrop
- leaves the transcript visible and mouse-scrollable
- owns keyboard focus while the request is active
- uses a bounded share of terminal height and windows long option lists
- queues concurrent requests in request order
- restores the composer after resolution or cancellation

Public plugin interaction primitives use the same dock automatically. Custom
plugin UI remains a modal overlay because arbitrary components may require a
fully isolated surface. Headless transports retain their renderer-specific
interaction presentation.

## Consequences

Users can review the preceding conversation while answering without dismissing
the request. Model tools and plugins share one consistent interaction surface.
The shell now distinguishes queued interaction docks from modal custom overlays,
and must cancel both when the active session changes.
