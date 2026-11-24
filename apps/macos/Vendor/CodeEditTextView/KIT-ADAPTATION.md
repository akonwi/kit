# Kit editor layout adaptation

Source: https://github.com/CodeEditApp/CodeEditTextView at `d7ac3f11f22ec2e820187acce8f3a3fb7aa8ddec`.
The upstream license is retained. Only Sources and the license are vendored.
The minimal package manifest omits upstream tests and lint plugins.
Trailing whitespace in upstream comments has been removed.

Kit adds `blockHeights` (zero-based logical line index to vertical space after
that line) and `didLayoutBlocks` on TextLayoutManager. Line storage includes
that space; source storage, UTF-16 ranges, fragments, and line counts do not.
Kit owns hosted annotation views and gutter markers outside this package.
When blocks exist, TextView uses normal AppKit subview hit testing so hosted
inputs and buttons receive mouse clicks.
Changing blockHeights updates only lines whose block height changed.
TextView adds `drawBackground`, called from `draw(_:)` after `super.draw` and
before selections, so Kit can paint diff row tints beneath the line fragments.
Empty blockHeights preserves upstream behavior. Folding and editable documents
are outside the scope of this read-only adapter.

Typesetter ends each wrapped fragment at the suggested line break. Upstream
reads the break, an offset into the run, as a length from the fragment's start,
so every fragment after the first also drew the text of the rows below it and
ran past the wrap width.

A layout pass records the widest line it lays out, so without wrapping the
document is as wide as its longest line and scrolls horizontally to its end.
Upstream's `layoutLine` assigns that width to a local copy that shadows its
`inout` parameter, so the document stayed as wide as the viewport.

Without wrapping, Typesetter sets a line's text in pieces of at most 4,096
UTF-16 units, cut between composed characters, side by side in its one
fragment. CoreText finds an offset or a point in a CTLine in time linear in
its length, so selecting or clicking in a line of millions of characters took
seconds. Shaping doesn't continue across a cut. A composed character longer
than a piece is a piece of its own.

LineFragmentRenderer looks for invisible characters only in the text of the
CTLine it draws, not through the end of its fragment, and doesn't copy or
scan any text when no characters are configured to show.

TextView doesn't lay out lines when it is created. Before it is in a scroll
view its frame is the document's height, so upstream's layout there builds a
view for every line, which is quadratic in the line count. AppKit lays out
the visible lines once the view is shown.

A TextView that isn't editable doesn't offer its accessibility value as
settable and ignores a value set through accessibility. Upstream replaced the
text whatever `isEditable` said, so an accessibility client could change a
read-only editor.

When updating, reapply the changes in TextLayoutManager.swift and
TextLayoutManager+Layout.swift, TextView.swift, TextView+Layout.swift,
TextView+Accessibility.swift, Typesetter.swift, and LineFragmentRenderer.swift
and run Kit's editor geometry/selection tests and ReadOnlyScratchpadTests.
