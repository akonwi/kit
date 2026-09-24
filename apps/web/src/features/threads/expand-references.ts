import { homedir } from "node:os";
import { join } from "node:path";
import type { Session } from "../../session";
import { findSessionById } from "../../session";

function threadTitle(session: Session): string {
	return (session.name?.trim() || "Untitled thread").replace(/\s+/g, " ");
}

function sessionPath(id: string): string {
	const fullPath = join(homedir(), ".kit", "sessions", `${id}.jsonl`);
	const home = homedir();
	return fullPath.startsWith(home)
		? `~${fullPath.slice(home.length)}`
		: fullPath;
}

function buildReferenceLink(session: Session): string {
	return `[thread:${session.id}:${threadTitle(session)}](${sessionPath(session.id)})`;
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
	// Match #[thread:id:label] — the # prefix is inserted by the composer
	const matches = [...text.matchAll(/#\[thread:([^:\]]+):([^\]]*)\]/gi)];
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

	let result = text;
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

		result = result.split(match.placeholder).join(buildReferenceLink(session));
		expanded++;
	}

	return { text: result, expanded, errors };
}
