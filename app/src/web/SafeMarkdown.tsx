/** @jsxImportSource solid-js */
import DOMPurify from "dompurify";
import { marked } from "marked";
import { createMemo, type JSX } from "solid-js";

const FORBIDDEN_MARKDOWN_TAGS = [
	"audio",
	"button",
	"embed",
	"form",
	"iframe",
	"img",
	"input",
	"object",
	"style",
	"video",
];

function safeMarkdownHtml(
	markdown: string,
	profile: "document" | "interaction",
): string {
	const parsed = marked.parse(markdown, { async: false }) as string;
	const sanitized = DOMPurify.sanitize(parsed, {
		ALLOW_ARIA_ATTR: false,
		ALLOW_DATA_ATTR: false,
		ALLOWED_ATTR: profile === "document" ? ["href", "title"] : [],
		FORBID_TAGS:
			profile === "document"
				? FORBIDDEN_MARKDOWN_TAGS
				: [...FORBIDDEN_MARKDOWN_TAGS, "a", "h1", "h2", "h3", "h4", "h5", "h6"],
		USE_PROFILES: { html: true },
	});
	const template = document.createElement("template");
	template.innerHTML = sanitized;
	for (const link of Array.from(template.content.querySelectorAll("a"))) {
		const href = link.getAttribute("href");
		if (!href) continue;
		try {
			const url = new URL(href, window.location.href);
			if (url.protocol !== "http:" && url.protocol !== "https:") {
				link.removeAttribute("href");
				continue;
			}
			link.target = "_blank";
			link.rel = "noopener noreferrer";
		} catch {
			link.removeAttribute("href");
		}
	}
	return template.innerHTML;
}

export function SafeMarkdown(props: {
	content: string;
	class?: string;
	id?: string;
	profile?: "document" | "interaction";
}): JSX.Element {
	const html = createMemo(() =>
		safeMarkdownHtml(props.content, props.profile ?? "document"),
	);
	return (
		<div
			id={props.id}
			class={props.class ?? "markdown-content"}
			innerHTML={html()}
		/>
	);
}
