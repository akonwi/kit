import {
	createSession,
	findSessionById,
	listSessionsForCwd,
	readSession,
	type Session,
	writeSession,
} from "../session";
import { createEphemeralSession } from "./headless-host";

type HeadlessSessionStorage = {
	createSession: typeof createSession;
	findSessionById: typeof findSessionById;
	listSessionsForCwd: typeof listSessionsForCwd;
	readSession: typeof readSession;
	writeSession: typeof writeSession;
};

const defaultStorage: HeadlessSessionStorage = {
	createSession,
	findSessionById,
	listSessionsForCwd,
	readSession,
	writeSession,
};

export async function resolveHeadlessSession(
	cwd: string,
	options: {
		defaultPersistence: "ephemeral" | "persistent";
		newSession?: boolean;
		sessionId?: string;
	},
	storage: HeadlessSessionStorage = defaultStorage,
): Promise<{ session: Session; persistSession: boolean }> {
	if (options.sessionId) {
		const session =
			(await storage.findSessionById(options.sessionId)) ??
			(await storage.readSession(options.sessionId));
		if (!session) throw new Error(`Session not found: ${options.sessionId}`);
		return { session, persistSession: true };
	}
	if (options.defaultPersistence === "ephemeral") {
		return { session: createEphemeralSession(cwd), persistSession: false };
	}
	if (options.newSession) {
		const session = await storage.createSession(cwd);
		await storage.writeSession(session);
		return { session, persistSession: true };
	}
	const recent = await storage.listSessionsForCwd(cwd);
	for (const summary of recent) {
		const session = await storage.readSession(summary.id);
		if (session) return { session, persistSession: true };
	}
	const session = await storage.createSession(cwd);
	await storage.writeSession(session);
	return { session, persistSession: true };
}
