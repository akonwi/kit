import { registerCustomCSSVariableTheme } from "@pierre/diffs";

const MICA_THEME_NAME = "kit-mica";

export const MICA_DIFF_THEME = {
	dark: MICA_THEME_NAME,
	light: MICA_THEME_NAME,
} as const;

registerCustomCSSVariableTheme(
	MICA_THEME_NAME,
	{
		foreground: "var(--color-text)",
		background: "var(--color-surface)",
		"token-comment": "var(--color-text-muted)",
		"token-constant": "var(--color-warn-text)",
		"token-keyword": "var(--color-accent)",
		"token-parameter": "var(--color-text)",
		"token-function": "var(--color-accent)",
		"token-string": "var(--color-success-text)",
		"token-string-expression": "var(--color-success-text)",
		"token-punctuation": "var(--color-text-muted)",
		"token-link": "var(--color-accent)",
		"token-inserted": "var(--color-success-text)",
		"token-deleted": "var(--color-danger-text)",
		"token-changed": "var(--color-warn-text)",
		"ansi-black": "var(--neutral-1)",
		"ansi-red": "var(--color-danger-text)",
		"ansi-green": "var(--color-success-text)",
		"ansi-yellow": "var(--color-warn-text)",
		"ansi-blue": "var(--color-accent)",
		"ansi-magenta": "var(--color-accent)",
		"ansi-cyan": "var(--color-accent)",
		"ansi-white": "var(--color-text)",
		"ansi-bright-black": "var(--color-text-muted)",
		"ansi-bright-red": "var(--color-danger-text)",
		"ansi-bright-green": "var(--color-success-text)",
		"ansi-bright-yellow": "var(--color-warn-text)",
		"ansi-bright-blue": "var(--color-accent)",
		"ansi-bright-magenta": "var(--color-accent)",
		"ansi-bright-cyan": "var(--color-accent)",
		"ansi-bright-white": "var(--color-text)",
	},
	false,
);

export const MICA_DIFF_CSS = `
:host {
  --diffs-font-family: var(--font-mono);
  --diffs-light-bg: var(--color-surface);
  --diffs-dark-bg: var(--color-surface);
  --diffs-light: var(--color-text);
  --diffs-dark: var(--color-text);
  --diffs-bg-buffer-override: var(--neutral-2);
  --diffs-bg-hover-override: var(--neutral-3);
  --diffs-bg-context-override: var(--color-surface-raised);
  --diffs-bg-context-number-override: var(--neutral-3);
  --diffs-bg-separator-override: var(--neutral-3);
  --diffs-fg-number-override: var(--color-text-muted);
  --diffs-fg-number-addition-override: var(--color-success-text);
  --diffs-fg-number-deletion-override: var(--color-danger-text);
  --diffs-addition-color-override: var(--color-success);
  --diffs-deletion-color-override: var(--color-danger);
  --diffs-modified-color-override: var(--color-accent);
  --diffs-bg-addition-override: var(--color-success-surface);
  --diffs-bg-addition-number-override: var(--color-success-surface);
  --diffs-bg-addition-hover-override: color-mix(
    in oklch,
    var(--color-success-surface) 82%,
    var(--color-success-border)
  );
  --diffs-bg-addition-emphasis-override: color-mix(
    in oklch,
    var(--color-success) 20%,
    transparent
  );
  --diffs-bg-deletion-override: var(--color-danger-surface);
  --diffs-bg-deletion-number-override: var(--color-danger-surface);
  --diffs-bg-deletion-hover-override: color-mix(
    in oklch,
    var(--color-danger-surface) 82%,
    var(--color-danger-border)
  );
  --diffs-bg-deletion-emphasis-override: color-mix(
    in oklch,
    var(--color-danger) 20%,
    transparent
  );
  --diffs-selection-color-override: var(--color-accent);
  --diffs-bg-selection-override: color-mix(
    in oklch,
    var(--color-surface) 72%,
    var(--color-accent)
  );
  --diffs-bg-selection-number-override: color-mix(
    in oklch,
    var(--color-surface) 58%,
    var(--color-accent)
  );
}
[data-separator='line-info'] [data-separator-wrapper],
[data-separator='line-info'] [data-separator-content],
[data-separator='line-info-basic'] [data-separator-wrapper],
[data-separator='line-info-basic'] [data-separator-content] {
  border-radius: 0 !important;
}
`;
