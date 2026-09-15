# Suspend hidden workspace pane reconciliation and animation

Retained hidden workspace panes are excluded from layout updates, paint, hit
testing, focus, keyboard handling, and pane-specific polling. Their mounted
widget subtrees may still perform lightweight rebuild work or animation ticks.

A future optimization should add a visibility-aware suspension mechanism that:

- prevents hidden loading indicators and other animations from scheduling
  frames;
- avoids recomputing expensive pane presentation while hidden;
- reconciles current authoritative client data when the pane becomes visible;
- preserves pane-local state and the existing stable widget identity; and
- does not weaken close/session-replacement disposal or generation checks.

Implement this only with measurements or a pane whose hidden reconciliation is
materially expensive; do not replace retained state with remounting as a shortcut.
