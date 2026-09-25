/**
 * Thread (session) index for # references.
 * Fetches session list from the runtime, caches it, provides scored suggestions.
 */

import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { SessionSummary } from "../../session";
import { formatTimeAgo } from "../commands/utils";
import { scoreMatch } from "../files/score";

export type ThreadSuggestion = {
	name: string;
	description: string;
	/** Short id (first 8 chars) used in #id references */
	value: string;
};

function threadTitle(session: SessionSummary): string {
	const head = (
		session.name?.trim() ||
		session.firstMessage?.trim() ||
		"Untitled thread"
	).replace(/\s+/g, " ");
	return head.length <= 80 ? head : `${head.slice(0, 79)}…`;
}

export function createThreadIndex(runtime: AgentRuntime) {
	let cached: SessionSummary[] | null = null;
	let fetching: Promise<SessionSummary[]> | null = null;
	let completedTurnCount = 0;

	const unsubscribe = runtime.subscribe((event) => {
		switch (event.type) {
			case "agent.turn.completed":
				completedTurnCount += 1;
				if (completedTurnCount >= 5) {
					completedTurnCount = 0;
					invalidate();
				}
				break;
			case "session.active.changed":
				completedTurnCount = 0;
				invalidate();
				break;
		}
	});

	async function doFetch(): Promise<SessionSummary[]> {
		const all = await runtime.listAllSessions();
		const currentId = runtime.getSession().id;
		return all
			.filter((session) => session.id !== currentId)
			.sort(
				(a, b) =>
					new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
			);
	}

	async function ensureLoaded(): Promise<SessionSummary[]> {
		if (cached) return cached;
		if (!fetching) {
			fetching = doFetch().then((sessions) => {
				cached = sessions;
				fetching = null;
				return sessions;
			});
		}
		return fetching;
	}

	async function suggest(query: string): Promise<ThreadSuggestion[]> {
		const sessions = await ensureLoaded();

		return sessions
			.map((session) => {
				const title = threadTitle(session);
				const id8 = session.id.slice(0, 8);
				const haystack = `${title} ${id8} ${session.cwd}`;
				const score = query ? scoreMatch(haystack, query) : 1;
				return { session, title, id8, score };
			})
			.filter((entry) => entry.score > 0)
			.sort(
				(a, b) =>
					b.score - a.score ||
					new Date(b.session.updatedAt).getTime() -
						new Date(a.session.updatedAt).getTime(),
			)
			.map((entry) => ({
				name: entry.title,
				description: `${entry.id8}  ·  ${formatTimeAgo(new Date(entry.session.updatedAt))}`,
				value: entry.id8,
			}));
	}

	function invalidate() {
		cached = null;
		fetching = null;
	}

	function dispose() {
		unsubscribe();
	}

	return { suggest, ensureLoaded, dispose };
}

export type ThreadIndex = ReturnType<typeof createThreadIndex>;
