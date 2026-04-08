import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";
import { formatSessionOption, formatTimeAgo } from "./utils";

export const switchCommand: Command = {
	name: "switch",
	description: "Switch to another session",
	async execute({ runtime, palette }) {
		const sessions = await runtime.listAllSessions();
		if (sessions.length === 0) return;

		const sorted = [...sessions].sort(
			(a, b) =>
				new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
		);

		const home = process.env.HOME || process.env.USERPROFILE || "";
		const widths = sorted.reduce(
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

		palette.show({
			filterable: true,
			hint: "Select a session",
			options: sorted.map((session) => {
				const { label, description } = formatSessionOption(session, widths);
				const isCurrent = session.id === runtime.getSession().id;
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
			}),
		});
	},
};
