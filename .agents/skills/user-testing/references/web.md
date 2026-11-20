# User testing in a browser

Read this when the target runs in a browser. Activate `browser-use` for the
mechanics: which browser surface to use, and how to navigate, read the page,
interact, capture, and collect diagnostics. This file covers what to do with
them during a user test.

## Session and tabs

- Before opening anything, look for a tab already on the target app. The user's open, logged-in tab is usually the only way into authenticated surfaces, since you must not sign in yourself.
- To reach another route in the same app, navigate that tab rather than opening a fresh one, which may not share its session.
- Leave unrelated tabs alone. Close only tabs you opened, and only once their evidence is saved.

## Reading and acting

- Read the page through the surface's accessibility snapshot or outline first. Use screenshots to confirm visual state, not to find elements.
- Element references from a snapshot go stale after any change. Re-read after each action and act only on fresh references.
- Don't add fixed waits after navigation or clicks. Wait again only when a fresh read shows the page is still loading.

## Diagnostics

When the surface exposes them, start collecting console errors, uncaught page errors, and failed network requests before running the flow, then report what was collected. A failed request that matches the step where the UI went wrong belongs in the finding.

## Visual checks

For theming, spacing, or alignment findings, read computed styles (color, background, border radius, font, size) or element bounds, and report the values next to the screenshot. Don't judge a color or size from pixels alone.

## Screenshots

- Bring the subject into view before capturing it. Use an element capture when the rest of the page is noise, and a full-page capture when page context matters.
- For long pages, capture a numbered sequence that covers the whole surface.

## Responsive checks

Only when asked, or when the criteria name a size. Default set unless the brief gives one:

| Name | Viewport |
|---|---|
| desktop | 1280 × 720 |
| tablet | 768 × 1024 |
| mobile | 375 × 812 |

Resizing the user's real tab is visible to them. Record the sizes actually
tested, restore a normal desktop size afterwards, and say which sizes the
surface couldn't set rather than claiming coverage.
