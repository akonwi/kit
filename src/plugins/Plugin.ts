import type { AgentTool } from "@mariozechner/pi-agent-core";
import type { Command } from "../features/commands/types";
import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import type { PluginContext } from "./types";

export abstract class Plugin {
	private readonly disposers: Array<() => void> = [];

	constructor(protected readonly ctx: PluginContext) {}

	initialize(): void {}

	protected subscribeRuntime(
		handler: (event: AgentRuntimeEvent) => void | Promise<void>,
	): void {
		const unsubscribe = this.ctx.runtime.subscribe((event) => {
			void handler(event);
		});
		this.disposers.push(unsubscribe);
	}

	protected registerCommand(command: Command): void {
		const unregister = this.ctx.commands.register(command);
		this.disposers.push(unregister);
	}

	protected registerTool(tool: AgentTool<any>): void {
		this.ctx.runtime.addTool(tool);
		// Note: tools registered this way persist for the session lifetime
		// If unregistration is needed, we'd need a different mechanism
	}

	protected addDisposer(disposer: () => void): void {
		this.disposers.push(disposer);
	}

	dispose(): void {
		for (const disposer of this.disposers.splice(0).reverse()) {
			disposer();
		}
	}
}
