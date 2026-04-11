import { Plugin } from "../../plugins/Plugin";
import type { AgentRuntimeEvent } from "../../runtime/agent-runtime";
import { maybeAutoNameSession } from "./auto-name";

export class SessionNamingPlugin extends Plugin {
	override initialize(): void {
		this.subscribeRuntime(async (event: AgentRuntimeEvent) => {
			if (event.type !== "turn_complete") return;
			if (this.ctx.settings.settings.sessionNaming === false) return;
			await maybeAutoNameSession(
				this.ctx.runtime,
				this.ctx.runtime.getMessages(),
			);
		});
	}
}
