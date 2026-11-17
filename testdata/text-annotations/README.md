The shared fixtures for comments on selected text (ADR 0070). Every client
that reads sent comments is held to them: the Apple clients through
`KitTranscript`'s `TextAnnotationText`, the terminal client and the browser
client through their own parsers.

Each case is two files:

- `<case>.txt` is a whole message as sent, followed by one newline that isn't
  part of it.
- `<case>.json` is what a client reads from it: `text`, the message's own
  text, and `comments`, each with its `number`, `comment`, `source`, `quote`
  and whether the quote was `cut`. A message whose blocks are malformed,
  aren't at its end, number more than 16, or hold a quote over 4 KiB comes
  back whole with no comments.

When `written` is true, `input` lists the comments a client adds, with their
nonces, and a client that writes comments must produce `<case>.txt` from
`text` and `input`.

A client writes every kind of line break in a comment (`\r\n`, `\r`,
U+0085, U+2028, U+2029) as a new, indented line, and cuts a quote over 4 KiB
at a Unicode scalar boundary.
