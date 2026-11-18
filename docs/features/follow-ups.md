# Follow-ups

A prompt sent while a turn runs is a follow-up. It waits in the session's
queue and starts its own turn once the turn before it ends, in the order it
was sent. The composer shows the queue; a client can restore it to the
composer for editing, or send it at once, which steers every queued prompt
into the running turn.

A follow-up is resolved when it is sent: its attachments are read and its
annotations are captured then, and those annotations cannot be edited while
the follow-up waits. Removing an attachment afterwards does not change it.

## Restarts

Follow-ups wait in the session droid's durable queue
([ADR 0073](../adrs/0073-layer-the-session-runtime-into-public-packages.md)),
so they survive a restart of the Kit server. After a restart, a session's
queue starts when the server next loads the session, after the session
resumes any turn the restart interrupted. Interrupted turns resume the same
way.

The server loads a session the first time anything needs it, not only when a
person opens it: a client fetching it, a peer query from another session, a
subagent result arriving, an ACP client, or a plugin reading its state. Any of
these can start a restored queue.

A turn that ends in a provider policy stop leaves its queue waiting until a
client restores it.
