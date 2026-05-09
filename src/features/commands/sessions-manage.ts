import type { SessionSummary } from "../../session";
import type { PickerContext } from "../../state/picker";
import type { Command } from "./types";
import { formatSessionOption } from "./utils";

export const sessionsManageCommand: Command = {
	name: "sessions",
	description: "Browse, switch, or delete sessions",
	async execute({ runtime, picker, toast }) {
		const sessions = await runtime.listAllSessions();
		if (sessions.length === 0) {
			toast({
				title: "Sessions",
				lines: ["No saved sessions found."],
				variant: "info",
			});
			return;
		}

		const currentSessionId = runtime.getSession().id;
		let visibleSessions = [...sessions].sort(
			(a, b) =>
				new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
		);

		function buildOptions(list: SessionSummary[]) {
			return list.map((session) => {
				const { label, description } = formatSessionOption(session);
				const isCurrent = session.id === currentSessionId;
				return {
					name: isCurrent ? `${label} ✓` : label,
					description,
					value: session,
					action: async (ctx: PickerContext) => {
						if (isCurrent) {
							ctx.dismiss();
							return;
						}
						try {
							await runtime.switchSession(session.id);
						} catch (error) {
							toast({
								title: "Session switch failed",
								lines: [String(error)],
								variant: "error",
							});
						}
						ctx.dismiss();
					},
				};
			});
		}

		function open() {
			picker.show(
				{
					options: buildOptions(visibleSessions),
					filterable: true,
					hint: "Enter switch · Ctrl+D delete · Esc close",
				},
				{
					"ctrl+d": async (option, ctx) => {
						const session = option.value as SessionSummary;
						if (session.id === currentSessionId) {
							toast({
								title: "Sessions",
								lines: ["Cannot delete the active session."],
								variant: "info",
							});
							return;
						}
						try {
							await runtime.deleteSession(session.id);
							visibleSessions = visibleSessions.filter(
								(s) => s.id !== session.id,
							);
							ctx.dismiss();
							if (visibleSessions.length === 0) {
								toast({
									title: "Sessions",
									lines: ["No saved sessions remaining."],
									variant: "info",
								});
								return;
							}
							open();
						} catch (error) {
							toast({
								title: "Session delete failed",
								lines: [String(error)],
								variant: "error",
							});
						}
					},
				},
			);
		}

		open();
	},
};
