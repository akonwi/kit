import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import type { PluginContext } from "./types";

export abstract class Plugin {
	private readonly disposers: Array<() => void> = [];

	constructor(protected readonly ctx: PluginContext) {}

	async initialize(): Promise<void> {}

	protected subscribeRuntime(
		handler: (event: AgentRuntimeEvent) => void | Promise<void>,
	): void {
		const unsubscribe = this.ctx.runtime.subscribe((event) => {
			void handler(event);
		});
		this.disposers.push(unsubscribe);
	}

	protected registerCommand(
		command: Parameters<PluginContext["commands"]["register"]>[0],
	): void {
		const unregister = this.ctx.commands.register(command);
		this.disposers.push(unregister);
	}

	protected addDisposer(disposer: () => void): void {
		this.disposers.push(disposer);
	}

	async dispose(): Promise<void> {
		for (const disposer of this.disposers.splice(0).reverse()) {
			disposer();
		}
	}
}
