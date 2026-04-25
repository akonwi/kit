# Pager

The pager provides a focused reading surface for substantial assistant output.

Instead of reading long responses only in the transcript, the pager can open that content in a section-based view for more deliberate review.

Current behavior:

- `/pager` opens the pager for the last long assistant response
- if the pager is already open, `/pager` closes it
- Kit can also auto-open the pager after a turn completes when the assistant response is long enough
- auto-open respects the `pager` setting
- if there is no long assistant response to page through, Kit shows a warning instead of opening the pager

The pager is designed to:

- split long content into sections
- let the user move section by section
- support focused reading outside the normal transcript flow
- support structured feedback on paged content

## How to access it

Run:

```text
/pager
```

The pager may also open automatically after long assistant responses if pager auto-open is enabled in settings.
