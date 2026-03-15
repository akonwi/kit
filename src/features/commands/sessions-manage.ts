import type { Command } from "./types";
import { formatSessionOption } from "./utils";

type SessionItem = {
	path: string;
	id: string;
	name: string | undefined;
	cwd: string;
	modified: Date;
	firstMessage: string;
};

export const sessionsManageCommand: Command = {
	name: "/sessions:manage",
	description: "Rename or delete sessions",
	async execute({ runtime, palette }) {
		const sessions = await runtime.listAllSessions();
		if (sessions.length === 0) return;

		const sorted = [...sessions].sort(
			(a, b) => b.modified.getTime() - a.modified.getTime(),
		);
		const currentSessionId = runtime.getSession().sessionId;
		let manageSessions: SessionItem[] = sorted.map((s) => ({
			path: s.path,
			id: s.id,
			name: s.name,
			cwd: s.cwd,
			modified: s.modified,
			firstMessage: s.firstMessage,
		}));

		function buildOptions() {
			return manageSessions.map((s) => {
				const { label, description } = formatSessionOption(s);
				return {
					name: label,
					description,
					value: s,
					action: () => {},
				};
			});
		}

		function refresh() {
			palette.pop();
			palette.show(
				{
					options: buildOptions(),
					filterable: true,
					hint: "Ctrl+R rename · Ctrl+D delete · Esc close",
				},
				{
					"ctrl+r": (option, _ctx) => {
						const session = option.value as SessionItem;
						palette.show({
							mode: "input",
							label: "Rename session",
							inputValue: session.name || "",
							onSubmit: (value, inputCtx) => {
								try {
									runtime.renameSession(session.path, value);
									session.name = value;
								} catch (error) {
									console.error(error);
								}
								inputCtx.dismiss();
								refresh();
							},
						});
					},
					"ctrl+d": async (option, _ctx) => {
						const session = option.value as SessionItem;
						if (session.id === currentSessionId) {
							return;
						}
						try {
							await runtime.deleteSession(session.path);
							manageSessions = manageSessions.filter(
								(s) => s.id !== session.id,
							);
							refresh();
						} catch (error) {
							console.error(error);
						}
					},
				},
			);
		}

		refresh();
	},
};
