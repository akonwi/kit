// Tests the repository's ui-api-demo plugin with the testing helper, as a
// plugin's own tests would.
import { expect, test } from "bun:test";
import { setup } from "../../../.kit/plugins/ui-api-demo/plugin.ts";
import { startTestPlugin } from "../kit-plugin-testing.ts";

test("the demo walks select, select, input, and confirm under its command", async () => {
  const kit = await startTestPlugin(setup, {
    pluginId: "ui-api-demo",
    dialogs: (dialog) => {
      if (dialog.method === "kit/ui/select") return { value: dialog.params.options[1]?.value };
      if (dialog.method === "kit/ui/input") return `${dialog.params.initialValue}!`;
      return true;
    },
  });
  await kit.runCommand("ui-demo", "remember");
  expect(kit.dialogs.map(({ method, params }) => [method, params.title, params.parentId])).toEqual([
    ["kit/ui/select", "Plugin UI demo: string select", "kit-2"],
    ["kit/ui/select", "Plugin UI demo: value select", "kit-2"],
    ["kit/ui/input", "Plugin UI demo: input", "kit-2"],
    ["kit/ui/confirm", "Plugin UI demo: confirm", "kit-2"],
  ]);
  expect(kit.toasts.at(-1)).toEqual({
    title: "UI demo complete",
    subtitle: "Open session · warning · note=remember!",
    variant: "warning",
  });
});

test("dismissing the first dialog ends the demo with a warning", async () => {
  const kit = await startTestPlugin(setup);
  await kit.runCommand("ui-demo");
  expect(kit.dialogs).toHaveLength(1);
  expect(kit.toasts.at(-1)).toEqual({
    title: "Plugin UI demo cancelled",
    subtitle: "No target selected.",
    variant: "warning",
  });
});
