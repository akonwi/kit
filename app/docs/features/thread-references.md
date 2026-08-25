# Thread References

Kit lets users reference other sessions or threads directly from the composer.

Current behavior:

1. type `#`
2. a filterable thread picker opens
3. select a thread
4. the composer inserts a thread reference token
5. on submit, that token is expanded into a bounded thread reference block before the message is sent to the agent

Thread references are backed by a cached session index.

Current behavior details:

- the picker excludes the active session
- the initial `#` is a provisional picker trigger and is not retained in the composer
- typing `##` during the grace period cancels the pending picker and leaves one literal `#`; the same escape works after the picker opens
- opening has a 250ms escape grace period, measured from the trigger press, so a quick `##` never flashes the picker; suggestion loading happens concurrently and text typed during that period becomes the initial filter
- whitespace restores the provisional `#` as literal text instead of opening the picker
- changing focus or opening another picker cancels the pending reference cleanly
- the double-trigger escape also works while thread suggestions are still loading
- inserted references use the `#[thread:id:name]` form in the composer
- submitted references are expanded by resolving the referenced session from Kit storage
- expansion currently produces metadata-only context rather than sampled thread transcript content

Expanded thread reference content currently includes:

- thread id
- title
- storage path
- cwd
- updated timestamp
- turn count
- message count

## How to access it

Type `#` in the composer. Type `##` to cancel the reference interaction.
