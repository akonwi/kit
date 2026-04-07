// @ts-nocheck — disabled pending rewrite
/**
 * Thread (session) index for @@ references.
 * Fetches session list from the runtime, caches it, provides scored suggestions.
 */

import type { SessionInfo } from "@mariozechner/pi-coding-agent";
import type { AgentRuntime } from "../../backend";
import { scoreMatch } from "../files/score";

export type ThreadSuggestion = {
	name: string;
	description: string;
	/** Short id (first 8 chars) used in [[thread:id]] tokens */
	value: string;
};

function threadTitle(s: SessionInfo): string {
	const head = (
		s.name?.trim() ||
		s.firstMessage?.trim() ||
		"Untitled thread"
	).replace(/\s+/g, " ");
	return head.length <= 80 ? head : `${head.slice(0, 79)}…`;
}

function formatDate(d: Date): string {
	return d.toISOString().replace("T", " ").slice(0, 16);
}

export function createThreadIndex(runtime: AgentRuntime) {
	let cached: SessionInfo[] | null = null;
	let fetching: Promise<SessionInfo[]> | null = null;

	async function doFetch(): Promise<SessionInfo[]> {
		const all = await runtime.listAllSessions();
		// Exclude the current session and sort by most recent first
		const currentPath = runtime.getSession().sessionFile;
		return all
			.filter((s) => s.path !== currentPath)
			.sort((a, b) => b.modified.getTime() - a.modified.getTime());
	}

	async function ensureLoaded(): Promise<SessionInfo[]> {
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

	/**
	 * Get thread suggestions matching a query.
	 * Triggers a fetch on first call (lazy).
	 */
	async function suggest(query: string): Promise<ThreadSuggestion[]> {
		const sessions = await ensureLoaded();

		return sessions
			.map((s) => {
				const title = threadTitle(s);
				const id8 = s.id.slice(0, 8);
				const haystack = `${title} ${id8} ${s.cwd}`;
				const score = query ? scoreMatch(haystack, query) : 1;
				return { s, title, id8, score };
			})
			.filter((x) => x.score > 0)
			.sort(
				(a, b) =>
					b.score - a.score || b.s.modified.getTime() - a.s.modified.getTime(),
			)
			.map((x) => ({
				name: `${x.title}`,
				description: `${x.id8}  ·  ${formatDate(x.s.modified)}`,
				value: x.id8,
			}));
	}

	/** Force a re-fetch (e.g. after session changes) */
	function invalidate() {
		cached = null;
		fetching = null;
	}

	return { suggest, invalidate, ensureLoaded };
}

export type ThreadIndex = ReturnType<typeof createThreadIndex>;
