import type { LoadedSettings } from "../compat/settings/load-settings";

export type AppShellProps = {
  settings: LoadedSettings;
};

const transcriptPreview = [
  "pi-kit v2 scaffold",
  "",
  "This shell is intentionally minimal.",
  "",
  "Near-term goals:",
  "- fixed bottom dock",
  "- independently scrollable transcript/screens",
  "- Pi session compatibility",
  "- feature migration from the old pi-kit extension",
];

export function AppShell({ settings }: AppShellProps) {
  return (
    <box width="100%" height="100%" flexDirection="column">
      <scrollbox
        flexGrow={1}
        scrollY
        stickyScroll
        stickyStart="bottom"
        viewportCulling
        border
        borderColor="#444444"
        padding={1}
        focused
      >
        <box flexDirection="column" gap={1} width="100%">
          {transcriptPreview.map((line) => (
            <text>{line}</text>
          ))}
        </box>
      </scrollbox>

      <box flexShrink={0} border borderColor="#666666" padding={1} flexDirection="column" gap={1}>
        <text>Dock placeholder</text>
        <text>Settings source: {settings.source}</text>
        <text>Pi root: {settings.paths.piAgentRoot}</text>
        <text>pi-kit settings: {settings.paths.piKitSettingsPath}</text>
      </box>
    </box>
  );
}
