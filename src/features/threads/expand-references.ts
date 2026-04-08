import type { Session } from "../../session";
import { findSessionById } from "../../session";

function threadTitle(session: Session): string {
	return (session.name?.trim() || "Untitled thread").replace(/\s+/g, " ");
}

function buildReferenceBlock(session: Session): string {
	return [
		"[Thread Reference]",
		`id: ${session.id}`,
		`title: ${threadTitle(session)}`,
		`cwd: ${session.cwd || "(unknown)"}`,
		`updated: ${session.updatedAt}`,
		`turns: ${session.turns.length}`,
		`messages: ${session.turns.reduce((count, turn) => count + turn.messages.length, 0)}`,
	].join("\n");
}

export type ExpandResult = {
	text: string;
	expanded: number;
	errors: string[];
};

export async function expandThreadReferences(
	text: string,
	currentSessionId?: string,
): Promise<ExpandResult> {
	const matches = [...text.matchAll(/\[thread:([^:\]]+):([^\]]*)\]/gi)];
	if (matches.length === 0) {
		return { text, expanded: 0, errors: [] };
	}

	const uniqueMatches = Array.from(
		new Map(
			matches.map((match) => [
				match[0],
				{ placeholder: match[0], id: match[1]?.trim() ?? "" },
			]),
		).values(),
	);

	let transformed = text;
	let expanded = 0;
	const errors: string[] = [];

	for (const match of uniqueMatches) {
		if (!match.id) {
			errors.push(`${match.placeholder}: empty thread id`);
			continue;
		}

		const session = await findSessionById(match.id);
		if (!session) {
			errors.push(`${match.placeholder}: no thread found for '${match.id}'`);
			continue;
		}
		if (currentSessionId && session.id === currentSessionId) {
			errors.push(`${match.placeholder}: cannot reference the active thread`);
			continue;
		}

		const block = buildReferenceBlock(session);
		transformed = transformed.split(match.placeholder).join(`\n\n${block}\n\n`);
		expanded++;
	}

	return { text: transformed, expanded, errors };
}
