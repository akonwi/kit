import {
	createSession,
	findSessionById,
	readSession,
	type Session,
	writeSession,
} from "../session";
import { createEphemeralSession } from "./headless-host";

export async function resolveHeadlessSession(
	cwd: string,
	options: {
		defaultPersistence: "ephemeral" | "persistent";
		sessionId?: string;
	},
): Promise<{ session: Session; persistSession: boolean }> {
	if (options.sessionId) {
		const session =
			(await findSessionById(options.sessionId)) ??
			(await readSession(options.sessionId));
		if (!session) throw new Error(`Session not found: ${options.sessionId}`);
		return { session, persistSession: true };
	}
	if (options.defaultPersistence === "ephemeral") {
		return { session: createEphemeralSession(cwd), persistSession: false };
	}
	const session = await createSession(cwd);
	await writeSession(session);
	return { session, persistSession: true };
}
