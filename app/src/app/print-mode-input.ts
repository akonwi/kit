export function buildPrintModePrompt(
	stdin: string | undefined,
	positionals: string[],
): string {
	const positionalPrompt = positionals.join(" ");
	if (!stdin) return positionalPrompt;
	const separator = stdin.endsWith("\n") || !positionalPrompt ? "" : "\n";
	return `${stdin}${separator}${positionalPrompt}`;
}
