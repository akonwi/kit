The shared fixtures for comments on selected text (ADR 0070). Every client
that reads sent comments is held to them: the Apple clients through
`KitTranscript`'s `TextAnnotationText`, the terminal client and the browser
client through their own parsers.

Each case is two files:

- `<case>.txt` is a whole message as sent, followed by one newline that isn't
  part of it.
- `<case>.json` is what a client reads from it: `text`, the message's own
  text, and `comments`, each with its `number`, `comment`, `source`, `quote`
  and whether the quote was `cut`. A message whose blocks are malformed, or
  aren't at its end, comes back whole with no comments.

When `written` is true, `input` lists the comments a client adds, with their
nonces, and a client that writes comments must produce `<case>.txt` from
`text` and `input`.
