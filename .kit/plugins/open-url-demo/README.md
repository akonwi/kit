# Open URL demo

Run Kit from this worktree and use `/reload` to discover the plugin.

- `/open-url-demo.open <url>` — asks the session's attached clients to open the
  URL. The macOS app opens it as a browser tab beside the conversation; the TUI
  ignores the request. A toast reports whether Kit accepted the request or
  rejected it, for example for a non-web address or after more than five
  requests in ten seconds.

Requires Python 3.
