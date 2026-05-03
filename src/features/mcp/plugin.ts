import type { AgentTool } from "@mariozechner/pi-agent-core";
import { Plugin } from "../../plugins/Plugin";
import { openExternal } from "../../shell/open-external";
import type { CommandContext } from "../commands/types";
import { loadMcpConfig } from "./config";
import { McpManager } from "./manager";
import { loadMcpMetadataCache, saveMcpMetadataCache } from "./metadata-cache";
import { startMcpOAuthCallbackServer } from "./oauth-callback";
import { loadMcpOAuthStore, saveMcpOAuthStore } from "./oauth-store";
import { createMcpProxyTool, MCP_PROXY_POLICY } from "./proxy-tool";
import type { LoadMcpConfigResult } from "./types";

export class McpPlugin extends Plugin {
	private manager: McpManager | null = null;
	private unregisterTool: (() => void) | null = null;
	private removePolicy: (() => void) | null = null;
	private clearDebugSection: (() => void) | null = null;
	private lastConfig: LoadMcpConfigResult | null = null;
	private saveCachePromise = Promise.resolve();
	private saveAuthPromise = Promise.resolve();

	override initialize(): void {
		this.subscribeRuntimeEvent("session.active.changed", async () => {
			await this.refresh();
		});

		this.registerCommand({
			name: "mcp-status",
			description: "Show a short MCP server status summary",
			execute: async (ctx: CommandContext) => {
				ctx.toast({
					title: "MCP status",
					lines: this.getStatusSummary(),
					variant: "info",
				});
			},
		});

		this.registerCommand({
			name: "mcp-reload",
			description: "Reload MCP config and rebuild the MCP plugin state",
			execute: async (ctx: CommandContext) => {
				await this.refresh();
				ctx.toast({
					title: "MCP reloaded",
					lines: this.getStatusSummary(),
					variant: "info",
				});
			},
		});

		this.registerCommand({
			name: "mcp-logout",
			argName: "server",
			description: "Clear Kit's saved OAuth state for one MCP server",
			execute: async (ctx: CommandContext) => {
				const serverName = ctx.args.trim();
				if (!serverName) {
					ctx.toast({
						title: "MCP logout",
						lines: [
							"Provide a server name, for example: /mcp-logout my-server",
						],
						variant: "warning",
					});
					return;
				}
				if (!this.manager) {
					ctx.toast({
						title: "MCP logout",
						lines: ["No MCP servers are currently configured."],
						variant: "warning",
					});
					return;
				}
				const definition = this.manager.getDefinition(serverName);
				if (!definition) {
					ctx.toast({
						title: "MCP logout",
						lines: [`Unknown MCP server: ${serverName}`],
						variant: "warning",
					});
					return;
				}
				if (definition.type !== "http" || definition.auth?.type !== "oauth") {
					ctx.toast({
						title: "MCP logout",
						lines: [`${serverName} does not have Kit-managed OAuth state.`],
						variant: "warning",
					});
					return;
				}
				const hadSession = this.manager.hasOAuthSession(serverName);
				await this.manager.clearOAuthSession(serverName);
				this.updateDebugSection();
				ctx.toast({
					title: "MCP logout",
					lines: hadSession
						? [
								`Cleared saved OAuth state for ${serverName}.`,
								"Kit will re-authorize automatically on next use.",
							]
						: [`No saved OAuth state existed for ${serverName}.`],
					variant: "info",
				});
			},
		});

		void this.refresh();
	}

	override dispose(): void {
		this.unregisterTool?.();
		this.unregisterTool = null;
		this.removePolicy?.();
		this.removePolicy = null;
		this.clearDebugSection?.();
		this.clearDebugSection = null;
		void this.manager?.dispose();
		this.manager = null;
		super.dispose();
	}

	private async refresh(): Promise<void> {
		const cwd = this.ctx.runtime.getSession().cwd;
		const [config, cache, oauthStore] = await Promise.all([
			loadMcpConfig(cwd),
			loadMcpMetadataCache(),
			loadMcpOAuthStore(),
		]);
		this.lastConfig = config;

		this.unregisterTool?.();
		this.unregisterTool = null;
		this.removePolicy?.();
		this.removePolicy = null;
		await this.manager?.dispose();

		const enabledServers = config.servers.filter((server) => !server.disabled);
		this.manager = new McpManager(config.servers, cache, oauthStore, {
			onStateChange: () => {
				this.updateDebugSection();
				void this.persistCache();
				void this.persistAuth();
			},
			authorizeOAuthServer: (serverName, authorizationUrl) =>
				this.authorizeOAuthServer(serverName, authorizationUrl),
		});
		if (enabledServers.length > 0) {
			const tool = createMcpProxyTool(this.manager);
			this.unregisterTool = this.ctx.runtime.addTool(tool as AgentTool);
			this.removePolicy = this.ctx.runtime.addSystemPromptAddition(
				MCP_PROXY_POLICY(),
			);
		}

		this.updateDebugSection();
		await Promise.all([this.persistCache(), this.persistAuth()]);
	}

	private async authorizeOAuthServer(
		serverName: string,
		authorizationUrl: URL,
	): Promise<string> {
		let callbackServer: Awaited<
			ReturnType<typeof startMcpOAuthCallbackServer>
		> | null = null;
		try {
			callbackServer = await startMcpOAuthCallbackServer();
			try {
				await openExternal(authorizationUrl.toString());
				this.ctx.ui.toast({
					title: "MCP login required",
					lines: [
						`Complete login for ${serverName} in your browser.`,
						"Kit will continue automatically when authorization finishes.",
					],
					variant: "info",
				});
			} catch {
				this.ctx.ui.toast({
					title: "MCP login required",
					lines: [
						`Open this authorization URL for ${serverName}:`,
						authorizationUrl.toString(),
					],
					variant: "warning",
				});
			}
			const code = await callbackServer.waitForCode();
			this.ctx.ui.toast({
				title: "MCP authorized",
				lines: [`Authorization complete for ${serverName}.`],
				variant: "info",
			});
			return code;
		} finally {
			await callbackServer?.close().catch(() => undefined);
		}
	}

	private async persistCache(): Promise<void> {
		if (!this.manager) return;
		const snapshot = this.manager.getPersistentCache();
		this.saveCachePromise = this.saveCachePromise
			.catch(() => undefined)
			.then(() => saveMcpMetadataCache(snapshot));
		await this.saveCachePromise;
	}

	private async persistAuth(): Promise<void> {
		if (!this.manager) return;
		const snapshot = this.manager.getPersistentOAuthStore();
		this.saveAuthPromise = this.saveAuthPromise
			.catch(() => undefined)
			.then(() => saveMcpOAuthStore(snapshot));
		await this.saveAuthPromise;
	}

	private getStatusSummary(): string[] {
		if (!this.manager) return ["No MCP servers are currently configured."];
		const states = this.manager.getRuntimeStates();
		if (states.length === 0) {
			return ["No MCP servers are currently configured."];
		}
		return states.map((state) => {
			const prefix =
				state.status === "connected"
					? "✓"
					: state.status === "connecting"
						? "◌"
						: state.status === "error"
							? "✗"
							: state.status === "disabled"
								? "⊘"
								: "○";
			const oauth = this.manager?.hasOAuthSession(state.name)
				? " · oauth saved"
				: "";
			return `${prefix} ${state.name} (${state.type})${state.toolCount > 0 ? ` · ${state.toolCount} tools` : ""}${state.cached ? " · cached" : ""}${oauth}${state.lastError ? ` · ${state.lastError}` : ""}`;
		});
	}

	private updateDebugSection(): void {
		this.clearDebugSection?.();
		const lines: string[] = [];
		const files = this.lastConfig?.files ?? [];
		if (files.length > 0) {
			lines.push("Files:");
			for (const file of files) {
				lines.push(
					`- ${file.loaded ? "loaded" : "missing"} · ${file.source} · ${file.filePath}`,
				);
			}
		}
		const warnings = this.lastConfig?.warnings ?? [];
		if (warnings.length > 0) {
			lines.push("Warnings:");
			for (const warning of warnings) lines.push(`- ${warning}`);
		}
		const states = this.manager?.getRuntimeStates() ?? [];
		if (states.length > 0) {
			lines.push("Servers:");
			for (const state of states) {
				const oauth = this.manager?.hasOAuthSession(state.name)
					? " · oauth saved"
					: "";
				lines.push(
					`- ${state.name} · ${state.status} · ${state.type} · ${state.toolCount} tools${state.cached ? " · cached" : ""}${oauth}${state.lastError ? ` · ${state.lastError}` : ""}`,
				);
				const authorizationUrl = this.manager?.getPendingAuthorizationUrl(
					state.name,
				);
				if (authorizationUrl) {
					lines.push(`  auth url · ${authorizationUrl}`);
				}
			}
		}
		if (lines.length === 0) {
			lines.push("(no MCP config found)");
		}
		this.clearDebugSection = this.ctx.runtime.setDebugSection("MCP", lines);
	}
}
