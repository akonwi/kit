# Footer demo

Requires Python 3 on PATH. Reload a session in this repository, then use its
command palette in the TUI or macOS app:

- `footer-demo.set`: show styled text beside cwd/Git. Optional arguments supply
  the label; the macOS palette offers a separate argument field.
- `footer-demo.replace`: show the label and hide `kit.footer.location`.
- `footer-demo.clear`: remove this plugin's item and location-hide claim.

Reload removes all items and claims; initialization does not change the footer.
Changing cwd revokes this project plugin's contributions. Header and bottom-left
status remain untouched. Other plugins' hide claims are not removed by clear.
Long content uses the client's bounded footer overflow presentation. In the
macOS app, click the overflow button to see all footer content in a scrollable
popover. Plugin click and
URL actions are not yet supported. No files are modified or model turns started.
