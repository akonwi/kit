import type { Command } from "./types";
import type { NotificationConfig } from "../notification-config";

let configRef: { current: NotificationConfig } | null = null;
let onChangeCallback: ((config: NotificationConfig) => void) | null = null;

export function setNotificationConfigRef(
  ref: { current: NotificationConfig },
  onChange?: (config: NotificationConfig) => void,
) {
  configRef = ref;
  onChangeCallback = onChange ?? null;
}

export const bellsCommand: Command = {
  name: "bells",
  description: "Toggle bell notifications on/off",
  execute({ palette }) {
    if (!configRef) return;
    const config = configRef.current;

    palette.show({
      filterable: false,
      options: [
        {
          name: config.bells.enabled ? "Turn off" : "Turn on",
          description: "",
          value: "toggle",
          action: (ctx) => {
            config.bells.enabled = !config.bells.enabled;
            onChangeCallback?.(config);
            ctx.dismiss();
          },
        },
      ],
    });
  },
};

export const speechCommand: Command = {
  name: "speech",
  description: "Toggle speech notifications on/off",
  execute({ palette }) {
    if (!configRef) return;
    const config = configRef.current;

    palette.show({
      filterable: false,
      options: [
        {
          name: config.speech.enabled ? "Turn off" : "Turn on",
          description: "",
          value: "toggle",
          action: (ctx) => {
            config.speech.enabled = !config.speech.enabled;
            onChangeCallback?.(config);
            ctx.dismiss();
          },
        },
      ],
    });
  },
};
