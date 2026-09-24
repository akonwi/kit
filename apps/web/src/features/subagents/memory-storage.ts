import { randomUUID } from "node:crypto";
import {
	SESSION_VERSION,
	type Session,
	type SessionEntry,
	type SubagentSessionHeader,
} from "../../session";
import type { AppendableSessionEntry } from "../../storage/session-storage";
import type { CreateSubagentSessionOptions } from "../../storage/subagent-session-storage";
import type { SubagentParentStorage, SubagentSessionStorage } from "./state";

export function createMemorySubagentParentStorage(): SubagentParentStorage {
	const entriesBySession = new Map<string, SessionEntry[]>();
	return {
		async appendEntries(
			session: Session,
			entries: AppendableSessionEntry[],
		): Promise<SessionEntry[]> {
			const stored = entriesBySession.get(session.id) ?? [];
			let parentId = stored.at(-1)?.id ?? null;
			const appended = entries.map((entry) => {
				const prepared = {
					...entry,
					id: randomUUID(),
					parentId,
				} as SessionEntry;
				parentId = prepared.id;
				return prepared;
			});
			stored.push(...appended);
			entriesBySession.set(session.id, stored);
			return appended;
		},
		async readEntries(id: string): Promise<SessionEntry[]> {
			return [...(entriesBySession.get(id) ?? [])];
		},
	};
}

type MemorySubagentSession = {
	header: SubagentSessionHeader;
	entries: SessionEntry[];
};

export function createMemorySubagentSessionStorage(): SubagentSessionStorage {
	const sessions = new Map<string, MemorySubagentSession>();

	return {
		async create(options: CreateSubagentSessionOptions): Promise<void> {
			if (sessions.has(options.id)) return;
			sessions.set(options.id, {
				header: {
					type: "session",
					version: SESSION_VERSION,
					kind: "subagent",
					id: options.id,
					createdAt: new Date().toISOString(),
					cwd: options.cwd,
					ownerSessionId: options.ownerSessionId,
					agentName: options.agentName,
					description: options.description,
					source: options.source,
					model: options.model,
					thinkingLevel: options.thinkingLevel,
				},
				entries: [],
			});
		},
		async readHeader(id: string): Promise<SubagentSessionHeader | null> {
			return sessions.get(id)?.header ?? null;
		},
		async readEntries(id: string): Promise<SessionEntry[]> {
			return [...(sessions.get(id)?.entries ?? [])];
		},
		async appendEntries(
			id: string,
			entries: AppendableSessionEntry[],
		): Promise<SessionEntry[]> {
			const session = sessions.get(id);
			if (!session) throw new Error(`Sub-agent session not found: ${id}`);

			let parentId = session.entries.at(-1)?.id ?? null;
			let appended = entries.map((entry) => {
				const prepared = {
					...entry,
					id: randomUUID(),
					parentId,
				} as SessionEntry;
				parentId = prepared.id;
				return prepared;
			});
			const compactionIndex = appended.findLastIndex(
				(entry) => entry.type === "subagent_compaction",
			);
			if (compactionIndex >= 0) {
				parentId = null;
				appended = appended.slice(compactionIndex).map((entry) => {
					const reparented = { ...entry, parentId } as SessionEntry;
					parentId = reparented.id;
					return reparented;
				});
				session.entries = appended;
				return appended;
			}
			session.entries.push(...appended);
			return appended;
		},
		async delete(id: string): Promise<void> {
			sessions.delete(id);
		},
	};
}
