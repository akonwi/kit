/**
 * Argument parsing and substitution for prompt templates.
 *
 * Supports:
 * - $1, $2, ... for positional args
 * - $@ and $ARGUMENTS for all args joined
 * - ${@:N} for args from Nth onwards (1-indexed, bash-style)
 * - ${@:N:L} for L args starting from Nth
 */

/**
 * Parse command arguments respecting quoted strings (bash-style).
 */
export function parseCommandArgs(argsString: string): string[] {
	const args: string[] = [];
	let current = "";
	let inQuote: string | null = null;

	for (let i = 0; i < argsString.length; i++) {
		const char = argsString[i];

		if (inQuote) {
			if (char === inQuote) {
				inQuote = null;
			} else {
				current += char;
			}
		} else if (char === '"' || char === "'") {
			inQuote = char;
		} else if (char === " " || char === "\t") {
			if (current) {
				args.push(current);
				current = "";
			}
		} else {
			current += char;
		}
	}

	if (current) args.push(current);
	return args;
}

/**
 * Substitute argument placeholders in template content.
 *
 * Positional args ($1, $2) are replaced first, then wildcards ($@, $ARGUMENTS,
 * ${@:N}, ${@:N:L}). This prevents recursive substitution if argument values
 * happen to contain placeholder-like patterns.
 */
export function substituteArgs(content: string, args: string[]): string {
	let result = content;

	// Positional: $1, $2, etc. (1-indexed)
	result = result.replace(/\$(\d+)/g, (_, num) => {
		const index = Number.parseInt(num, 10) - 1;
		return args[index] ?? "";
	});

	// Sliced: ${@:start} or ${@:start:length} (1-indexed)
	result = result.replace(
		/\$\{@:(\d+)(?::(\d+))?\}/g,
		(_, startStr, lengthStr) => {
			let start = Number.parseInt(startStr, 10) - 1;
			if (start < 0) start = 0;
			if (lengthStr) {
				const length = Number.parseInt(lengthStr, 10);
				return args.slice(start, start + length).join(" ");
			}
			return args.slice(start).join(" ");
		},
	);

	const allArgs = args.join(" ");

	// $ARGUMENTS — all args joined
	result = result.replace(/\$ARGUMENTS/g, allArgs);

	// $@ — all args joined
	result = result.replace(/\$@/g, allArgs);

	return result;
}
