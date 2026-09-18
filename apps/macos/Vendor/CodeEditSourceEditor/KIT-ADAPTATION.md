# Kit package integration

CodeEditSourceEditor 0.15.2, upstream commit `424453d2232c9912933a3b5a1f3d3df669404ed0`.
The upstream license is retained. MinimapView.hitTest respects hidden ancestors
so the disabled minimap cannot intercept document controls. TextViewController
exposes a gutterWidthOverride for the native old/new diff rail; it changes only
text insets. Its embedded scroll view can forward vertical wheel events to a
parent scroll view, allowing native editors to form one continuous diff surface
while retaining horizontal scrolling. Other sources are unchanged apart from
trailing whitespace cleanup. Reapply these adaptations when updating the package.
The minimal manifest points to ../CodeEditTextView so SwiftPM resolves the
maintained block-layout adaptation without conflicting remote/local identities.
Development-only upstream tests and lint plugins are omitted.
