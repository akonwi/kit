/**
 * Infer a tree-sitter filetype from a file path.
 * Only returns filetypes that have parsers registered in OpenTUI core
 * or added by Kit in bootstrap. Returns undefined for unsupported
 * filetypes so the code component falls back to plain text.
 */
export function inferFiletype(filePath: string): string | undefined {
	const normalized = filePath.toLowerCase();
	if (normalized.endsWith(".ard")) return "ard";
	if (normalized.endsWith(".ts")) return "typescript";
	if (normalized.endsWith(".tsx")) return "tsx";
	if (normalized.endsWith(".jsx")) return "jsx";
	if (
		normalized.endsWith(".js") ||
		normalized.endsWith(".mjs") ||
		normalized.endsWith(".cjs")
	) {
		return "javascript";
	}
	if (normalized.endsWith(".md") || normalized.endsWith(".mdx"))
		return "markdown";
	if (normalized.endsWith(".zig")) return "zig";
	if (normalized.endsWith(".json") || normalized.endsWith(".jsonc"))
		return "json";
	if (normalized.endsWith(".toml")) return "toml";
	if (normalized.endsWith(".rb") || normalized.endsWith(".gemspec"))
		return "ruby";
	if (normalized.endsWith(".sh") || normalized.endsWith(".bash")) return "bash";
	if (normalized.endsWith(".yml") || normalized.endsWith(".yaml"))
		return "yaml";
	if (normalized.endsWith(".css")) return "css";
	if (normalized.endsWith(".html") || normalized.endsWith(".htm"))
		return "html";
	if (normalized.endsWith(".rs")) return "rust";
	if (normalized.endsWith(".go")) return "go";
	if (normalized.endsWith(".py")) return "python";
	return undefined;
}
