import { describe, expect, mock, test } from "bun:test";
import type { Session, SessionSummary } from "../session";
import { resolveHeadlessSession } from "./headless-session";

function session(id: string, cwd: string): Session {
	const timestamp = "2026-01-01T00:00:00.000Z";
	return {
		id,
		version: 2,
		cwd,
		createdAt: timestamp,
		updatedAt: timestamp,
		turns: [],
	};
}

function summary(value: Session): SessionSummary {
	return {
		id: value.id,
		cwd: value.cwd,
		createdAt: value.createdAt,
		updatedAt: value.updatedAt,
		messageCount: 0,
	};
}

describe("headless startup session selection", () => {
	test("resumes the most recent session for persistent startup", async () => {
		const recent = session("recent", "/project");
		const createSession = mock(async () => session("new", "/project"));
		const writeSession = mock(async () => {});

		const resolved = await resolveHeadlessSession(
			"/project",
			{ defaultPersistence: "persistent" },
			{
				createSession,
				findSessionById: async () => null,
				listSessionsForCwd: async () => [summary(recent)],
				readSession: async (id) => (id === recent.id ? recent : null),
				writeSession,
			},
		);

		expect(resolved).toEqual({ session: recent, persistSession: true });
		expect(createSession).not.toHaveBeenCalled();
		expect(writeSession).not.toHaveBeenCalled();
	});

	test("creates a new persistent session without resuming a recent one", async () => {
		const created = session("new", "/project");
		const listSessionsForCwd = mock(async () => [
			summary(session("recent", "/project")),
		]);
		const writeSession = mock(async () => {});

		const resolved = await resolveHeadlessSession(
			"/project",
			{ defaultPersistence: "persistent", newSession: true },
			{
				createSession: async () => created,
				findSessionById: async () => null,
				listSessionsForCwd,
				readSession: async () => null,
				writeSession,
			},
		);

		expect(resolved).toEqual({ session: created, persistSession: true });
		expect(listSessionsForCwd).not.toHaveBeenCalled();
		expect(writeSession).toHaveBeenCalledWith(created);
	});

	test("keeps no-session startup ephemeral without discovering persisted sessions", async () => {
		const listSessionsForCwd = mock(async () => [
			summary(session("recent", "/project")),
		]);

		const resolved = await resolveHeadlessSession(
			"/project",
			{ defaultPersistence: "ephemeral" },
			{
				createSession: async () => session("new", "/project"),
				findSessionById: async () => null,
				listSessionsForCwd,
				readSession: async () => null,
				writeSession: async () => {},
			},
		);

		expect(resolved.persistSession).toBe(false);
		expect(resolved.session.cwd).toBe("/project");
		expect(listSessionsForCwd).not.toHaveBeenCalled();
	});

	test("creates and persists a session when the cwd has no sessions", async () => {
		const created = session("new", "/project");
		const writeSession = mock(async () => {});

		const resolved = await resolveHeadlessSession(
			"/project",
			{ defaultPersistence: "persistent" },
			{
				createSession: async () => created,
				findSessionById: async () => null,
				listSessionsForCwd: async () => [],
				readSession: async () => null,
				writeSession,
			},
		);

		expect(resolved).toEqual({ session: created, persistSession: true });
		expect(writeSession).toHaveBeenCalledWith(created);
	});
});
