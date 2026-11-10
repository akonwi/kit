// Exercise Kit's select, input, confirm, and toast plugin APIs with the
// TypeScript helper. A real plugin copies kit-plugin.ts; this fixture links
// to the repository's copy.
import { type Kit, runPlugin, type ToastVariant } from "./kit-plugin.ts";

export async function setup(kit: Kit) {
  await kit.commands.register({
    id: "ui-demo",
    description: "Exercise plugin select, input, confirm, and toast APIs",
    argName: "initial note",
    category: "plugins",
    // Dialogs opened through context.ui belong to this command, so Kit closes
    // them if the command is cancelled.
    async execute(args, { ui }) {
      const cancelled = (subtitle: string) =>
        kit.ui.toast({ title: "Plugin UI demo cancelled", subtitle, variant: "warning" });
      kit.ui.toast({ title: "Plugin UI demo", subtitle: "Starting select → input → confirm flow." });

      const target = await ui.select({
        title: "Plugin UI demo: string select",
        message: "Pick a target scope. Escape cancels.",
        options: ["Current file", "Open session", "Whole project"].map((scope) => ({ label: scope, value: scope })),
        placeholder: "Filter scopes...",
      });
      if (!target) return cancelled("No target selected.");

      const variant = await ui.select({
        title: "Plugin UI demo: value select",
        message: "Pick the toast variant to show at the end.",
        options: [
          { label: "Info", value: "info", description: "Normal notification" },
          { label: "Warning", value: "warning", description: "Cautionary notification" },
          { label: "Error", value: "error", description: "Failure notification" },
        ] as const,
        placeholder: "Filter variants...",
      });
      if (!variant) return cancelled("No toast variant selected.");

      const note = await ui.input({
        title: "Plugin UI demo: input",
        message: `Target: ${target.value}. Type a note for the final toast.`,
        placeholder: "Demo note...",
        initialValue: args,
      });
      if (note === null) return cancelled("Input was cancelled.");

      const show = await ui.confirm({
        title: "Plugin UI demo: confirm",
        message: `Show a ${variant.value} toast for ${target.value}?`,
        confirmLabel: "Show toast",
        cancelLabel: "Cancel",
        defaultValue: true,
      });
      if (!show) return cancelled("Confirmation returned false.");

      kit.ui.toast({
        title: "UI demo complete",
        subtitle: `${target.value} · ${variant.value} · note=${note || "(empty)"}`,
        variant: variant.value satisfies ToastVariant,
      });
    },
  });
}

if (import.meta.main) runPlugin(setup);
