import type { SessionSummary } from "../../session";
import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";
import { formatSessionOption, formatTimeAgo } from "./utils";

export const sessionsManageCommand: Command = {
	name: "sessions",
	description: "Browse, switch, or delete sessions",
	async execute({ runtime, palette }) {
		const sessions = await runtime.listAllSessions();
		if (sessions.length === 0) {
			runtime.emitInfo("Sessions", ["No saved sessions found."]);
			return;
		}

		const currentSessionId = runtime.getSession().id;
		let visibleSessions = [...sessions].sort(
			(a, b) =>
				new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
		);

		const home = process.env.HOME || process.env.USERPROFILE || "";
		const widths = visibleSessions.reduce(
			(acc, session) => {
				const cwd = session.cwd.startsWith(home)
					? `~${session.cwd.slice(home.length)}`
					: session.cwd;
				const updatedAt = formatTimeAgo(new Date(session.updatedAt));
				const messageCount = `${session.messageCount} msgs`;
				return {
					cwd: Math.max(acc.cwd, cwd.length),
					updatedAt: Math.max(acc.updatedAt, updatedAt.length),
					messageCount: Math.max(acc.messageCount, messageCount.length),
				};
			},
			{ cwd: 0, updatedAt: 0, messageCount: 0 },
		);

		function buildOptions(list: SessionSummary[]) {
			return list.map((session) => {
				const { label, description } = formatSessionOption(session, widths);
				const isCurrent = session.id === currentSessionId;
				return {
					name: isCurrent ? `${label} ✓` : label,
					description,
					value: session,
					action: async (ctx: PaletteContext) => {
						if (isCurrent) {
							ctx.dismiss();
							return;
						}
						try {
							await runtime.switchSession(session.id);
						} catch (error) {
							runtime.emitError("Session switch failed", [String(error)]);
						}
						ctx.dismiss();
					},
				};
			});
		}

		function open() {
			palette.show(
				{
					options: buildOptions(visibleSessions),
					filterable: true,
					hint: "Enter switch · Ctrl+D delete · Esc close",
				},
				{
					"ctrl+d": async (option, ctx) => {
						const session = option.value as SessionSummary;
						if (session.id === currentSessionId) {
							runtime.emitInfo("Sessions", [
								"Cannot delete the active session.",
							]);
							return;
						}
						try {
							await runtime.deleteSession(session.id);
							visibleSessions = visibleSessions.filter(
								(s) => s.id !== session.id,
							);
							ctx.dismiss();
							if (visibleSessions.length === 0) {
								runtime.emitInfo("Sessions", ["No saved sessions remaining."]);
								return;
							}
							open();
						} catch (error) {
							runtime.emitError("Session delete failed", [String(error)]);
						}
					},
				},
			);
		}

		open();
	},
};
