---
name: cooper-ui
description: Build and refactor Cooper/CUI terminal UI components. Use for Cooper component state, async updates, dialogs, pickers, focus, virtual lists, keyboard bindings, or CUI rendering issues.
---

# Cooper UI

Use this skill for work under `apps/cli/tui` that changes Cooper/CUI behavior.
Read the relevant Cooper source in `/Users/akonwi/Developer/agent/cooper` when framework behavior is uncertain.

## State ownership and rendering

- A component owns its local presentation state: loading, pending, errors,
  input text, selection, hover, list position, and focus lifecycle.
- Shell owns attached-session state and root modal selection. Child components
  report completed domain results upward; they do not mutate Shell fields.
- A callback that changes Shell-owned state must schedule that mutation through
  Shell's stored `cui::Context.dispatch`. This invalidates Shell's subtree.

```ard
fn mut apply_result(result: Result) {
  match self.context {
    context => {
      let _ = context.dispatch(fn() {
        self.session = result.session
        self.active_modal = ActiveModal::none
      })
    },
    _ => (),
  }
}
```

- `Context.dispatch` invalidates only the owning context's subtree. If a child
  callback changes a parent, dispatch through the parent context, not the
  child's context.
- Treat `Context.invalidate_root` as an escape hatch reserved for truly shared
  or ancestor state without an owning context. Do not use it as routine async
  refresh machinery.
- Do not rely on a later keystroke to render state changed by a callback.

## Async work

- Capture cancellation/context before starting async work; never access mutable
  component state directly from the worker.
- Deliver all completion state through `Context.dispatch` for the owning
  component.
- Use a generation counter when a later request, dismissal, or unmount can
  make an older completion stale.
- A child owns transport loading/pending/error state for requests it initiates.
  Pass only stable dependencies and a result callback from Shell.
- On success, apply the authoritative response and close the owning modal via
  its owner callback. On failure, retain the modal and show its local error.

## Dialogs and focus

- A dialog must have a focus owner on mount. Passive dialog panels use
  `autofocus: true`; input dialogs autofocus their input.
- Replacing one modal with another must remount a distinct focus owner. Ensure
  stable keys distinguish modal kinds when necessary.
- Escape must work immediately on first render, without a pointer click.
- Use a component's own context dispatch when an action initiated in a nested
  shared picker changes the parent dialog's state.

## Shared components and modules

- Put reusable interaction lifecycle in the shared component that owns it, not
  in each feature module. Shared behavior includes focus, query/editing state,
  selection, scrolling, paging, mount timing, and common navigation.
- Feature modules provide domain data, feature-specific labels/bindings, and
  activation behavior; they should not duplicate component lifecycle logic.
- Document a shared component's ownership, props, focus behavior, async
  boundary, and any lower-level escape hatch in its module comments.

### `palette::Picker`

- Use the stateful `palette::Picker` for ordinary searchable selection dialogs.
  It owns query, selection, hover, virtual-list ref, paging, initial reveal,
  and standard navigation.
- `initial_selection` means both selected **and revealed** after the virtual
  list mounts. Do not assume a list ref exists during initial render.
- Keep command palette on its lower-level API when it needs command-argument
  query semantics.

## Keybinding hints

Follow `.agents/skills/design/SKILL.md`:

- Show only novel, feature-specific bindings in footer hints.
- Do not show arrows, Enter, or Escape merely to narrate standard terminal
  interaction.
- Register a custom binding in the owning dialog/component and test that its
  first press changes the expected state.

## Validation checklist

For a changed component, add or update a focused presentation/interaction test
when practical. Verify the behavior that caused the change, especially:

- first custom keypress renders its intended surface;
- initial picker selection is visible;
- Escape closes a newly opened dialog immediately;
- async success updates the owning shell and closes the dialog;
- async failure remains visible in the initiating component;
- stale completion cannot overwrite newer state.

Run from `apps/cli`:

```sh
ard format --check .
ard check main.ard
ard test
go vet ./...
```
