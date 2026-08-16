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
- `minColumns` — its minimum useful width before the layout becomes narrow or
  snaps to the collapsed rail
- `available` — an optional runtime availability check
- `render` — the retained pane body

`WorkspacePaneHost` continues to own wide/narrow layout, tab navigation,
collapse behavior, and retention. The tab strip owns pane titles. Pane bodies may
add a `WorkspacePanelHeader` only for contextual scope or live metadata; panes
without that context omit the row. Pane bodies also own their content, focus
handling, and feature-specific actions.

## Adding a pane

1. Add a plain-data variant to `WorkspacePane` in
   `src/shell/workspace-pane-registry.tsx`.
2. Add the corresponding typed entry to `WORKSPACE_PANE_DEFINITIONS`. The mapped
   registry type makes a missing definition a type error.
3. Add any feature controllers needed by the body to
   `WorkspacePaneRenderContext` and provide them from `AppShell`.
4. Open the pane through the workspace controller and focus the returned tab.
5. Add registry contract tests for identity, label, close policy, and minimum
   width.

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
    minColumns: 36,
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
