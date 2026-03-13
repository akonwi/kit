import { createCliRenderer } from "@opentui/core";
import { render } from "@opentui/solid";
import { loadSettings } from "../compat/settings/load-settings";
import { App } from "./App";

export async function bootstrap(): Promise<void> {
  const settings = await loadSettings();
  const renderer = await createCliRenderer();

  render(() => <App settings={settings} />, renderer);
}
