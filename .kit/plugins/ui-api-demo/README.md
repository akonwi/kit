# UI API demo

Requires Python 3 on `PATH`. Kit discovers this project plugin automatically
when the session cwd is this repository. With the native Go build, reload the
session after changing plugin files.

Run `/ui-api-demo.ui-demo an optional initial note` from the command palette.
The flow is:

1. Choose a target from the option list.
2. Choose a toast variant.
3. Edit the initial note; submitting an empty string is valid.
4. Confirm with **Show toast**, or choose **Cancel**.
5. Observe the plugin's final toast.

The command does not start a model turn or edit files. Existing composer content
is preserved. Dialogs use the full-width interaction dock with plugin provenance;
select/input support Enter, confirm supports Tab then Enter, and Escape cancels.

A pending dialog belongs to the session, not one client. Another attached client
can answer it; the first valid answer wins. Session reload/deletion or plugin
failure revokes it. Temporary client absence is not noninteractive mode.

The automated daemon suite runs this flow, including cancellation, concurrent
answers, reload, and deletion. The user also confirmed the dialog flow
works in the TUI on macOS.
The [native profile](../../../app/docs/plugin-protocol/native-v2.md) describes
supported methods, bounds, and remaining conformance work.

The macOS app now lists plugin commands in its command palette, with a separate
argument field. Plugin notifications use the app’s existing session alerts, so
the final notification appears there rather than in a separate toast surface.
The user verified plugin notification delivery through macOS session alerts.
