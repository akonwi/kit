import { Plugin } from "../../plugins/Plugin";
import { maybeAutoNameSession } from "./auto-name";

export class SessionNamingPlugin extends Plugin {
	override initialize(): void {
		this.subscribeRuntimeEvent("agent.turn.completed", async () => {
			if (this.ctx.settings.settings.sessionNaming === false) return;
			await maybeAutoNameSession(
				this.ctx.runtime,
				this.ctx.runtime.getMessages(),
			);
		});
	}
}
