# Workspace pane definitions

The TUI workspace keeps pane state separate from rendering. A pane stored in
workspace state is plain data from the `WorkspacePane` discriminated union. The
`WORKSPACE_PANE_DEFINITIONS` registry supplies the UI and metadata for each pane
kind.

Each definition owns:

- `identity` — whether opening a descriptor reuses an existing tab or creates a
  distinct tab
- `label` — the title shown in the workspace tab strip
- `closable` — whether the tab exposes and accepts an explicit close action
- `available` — an optional runtime availability check
- `render` — the retained pane body

`WorkspacePaneHost` continues to own wide/narrow layout, tab navigation,
collapse behavior, and retention. The workspace layout defines one minimum useful
width for the secondary panel; individual pane definitions do not influence the
responsive breakpoint. The split layout requires a terminal width of at least 125
columns. The tab strip owns pane titles. Pane bodies may add a
`WorkspacePanelHeader` only for contextual scope or live metadata; panes without
that context omit the row. Pane bodies also own their content, focus handling,
and feature-specific actions.

Code Review and Scratchpad are persistent singleton panes. Each renderer makes
them available in that order when its workspace is created and restores them
when the active session changes. The secondary surface starts minimized, with
Code Review selected for the next expansion; focus and the narrow-layout
selection remain on the transcript/composer. Default pane bodies initialize only
when first selected, then remain mounted to preserve local state. Other panes
open on demand and may be closed.

The TUI also supports closable full-file panes identified by repository root and
relative path. Reopening the same file selects its existing tab. `/` opens the
workspace file finder from any focused secondary pane; the same action remains
available through the command palette from other contexts. File panes render the current working-tree file with line numbers and syntax
highlighting. Their context header owns a collapsed-by-default repository-tree
drawer toggle, with no redundant content header below it. File panes share Code
Review's feedback workflow: `Enter` or a line click edits a line note,
`Ctrl+Enter` starts a range, `f` edits a whole-file note, `x` clears the selected
line note, and `s` submits. Saved notes use file-specific session drafts, project into file-feedback
attachments, and remain until removed or consumed. They do not share Code
Review's diff-coordinate draft namespace. Each draft is pinned to the file
revision where commenting began; if the working file changes, its comments are
marked stale, its attachment is hidden, and submission is blocked until those
notes are cleared and recreated.
Code Review remains focused on its changed-file tree and diff presentation. Its
context header uses the shared sidebar toggle to collapse or expand the changes
tree; wide layouts split the tree and diff, while narrow layouts switch between
them at full width. It is scoped to the active session's Git repository and
remounts when a cwd change
moves the session to a different repository; per-repository drafts remain in the
session draft controller and are restored when that repository is revisited. A
long-form review feed is deferred until the workspace has a reliable
virtualization primitive.

## Adding a pane

1. Add a plain-data variant to `WorkspacePane` in
   `src/shell/workspace-pane-registry.tsx`.
2. Add the corresponding typed entry to `WORKSPACE_PANE_DEFINITIONS`. The mapped
   registry type makes a missing definition a type error.
3. Add any feature controllers needed by the body to
   `WorkspacePaneRenderContext` and provide them from `AppShell`.
4. Open the pane through the workspace controller and focus the returned tab.
5. Add registry contract tests for identity, label, and close policy.

For example:

```tsx
type WorkspacePane =
  | ExistingPaneVariants
  | { kind: "files"; root: string };

const WORKSPACE_PANE_DEFINITIONS = {
  // Existing definitions...
  files: {
    kind: "files",
    identity: (pane) => `files:${pane.root}`,
    label: () => "Files",
    closable: true,
    render: (pane, context) => (
      <FilesPanel
        root={pane.root}
        active={context.active()}
        onClose={context.onLeave}
        onFocusRequest={context.onFocusRequest}
      />
    ),
  },
} satisfies WorkspacePaneDefinitionMap;
```

Open it with:

```ts
const tabId = workspace.openSecondary(
  { kind: "files", root },
  { focus: "secondary" },
);
focusSecondarySurface(tabId);
```

Use a constant identity such as `"files"` for a singleton pane. If a singleton's
data changes while it is already open, call `updateSecondary` and then select
its existing tab, as Activity does.

Do not store JSX, component instances, or callbacks in `WorkspacePane`. Keeping
pane descriptors as data preserves the boundary between workspace state and the
TUI renderer and leaves room for other presentations to consume the same state.

## Web presentation

The browser follows the same state/presentation boundary with web-specific pane
descriptors in `src/web/workspace-panes.ts`. `WorkspaceProvider` owns ordered
process-lifetime state and opening actions; `WorkspacePaneHost` projects that
state as an expanded drawer, collapsed rail, or narrow top-level tabs. Pane
components remain mounted while hidden so drafts, selections, and scroll state
survive tab switches and responsive layout changes.

The browser does not import TUI render definitions. Each renderer owns its pane
bodies and metadata while reusing the renderer-neutral workspace state
controller. Activity, Code Review, and Scratchpad currently have browser pane
implementations; other TUI pane kinds remain explicit follow-up work.
