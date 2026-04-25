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

Type `#` in the composer.
