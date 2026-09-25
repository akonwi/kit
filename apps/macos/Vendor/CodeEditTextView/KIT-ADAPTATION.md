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
Empty blockHeights preserves upstream behavior. Folding and editable documents
are outside the scope of this read-only adapter.

When updating, reapply the changes in TextLayoutManager.swift and
TextLayoutManager+Layout.swift, and TextView.swift and run Kit's editor geometry/selection tests.
