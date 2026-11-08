// How models and thinking levels are named in menus and forms.

export const THINKING_LABELS: Record<string, string> = {
  off: "Off",
  minimal: "Minimal",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "Extra high",
  max: "Max",
};

const PROVIDERS: Record<string, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  google: "Google",
  openrouter: "OpenRouter",
  xai: "xAI",
  mistral: "Mistral",
};

export function providerName(id: string): string {
  return PROVIDERS[id] ?? id.charAt(0).toUpperCase() + id.slice(1);
}
