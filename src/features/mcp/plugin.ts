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
				const summary = this.getStatusSummary();
				ctx.toast({
					title: "MCP status",
					lines: summary,
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
			name: "mcp-connect",
			argName: "server",
			description: "Connect to one configured MCP server and load its tools",
			execute: async (ctx: CommandContext) => {
				const serverName = ctx.args.trim();
				if (!serverName) {
					ctx.toast({
						title: "MCP connect",
						lines: [
							"Provide a server name, for example: /mcp-connect chrome-devtools",
						],
						variant: "warning",
					});
					return;
				}
				if (!this.manager) {
					ctx.toast({
						title: "MCP connect",
						lines: ["No MCP servers are currently configured."],
						variant: "warning",
					});
					return;
				}
				try {
					await this.manager.connectServer(serverName);
					const tools = await this.manager.ensureTools(serverName);
					this.updateDebugSection();
					ctx.toast({
						title: "MCP connected",
						lines: [
							`${serverName} connected.`,
							`Loaded ${tools.length} tools.`,
						],
						variant: "info",
					});
				} catch (error) {
					ctx.toast({
						title: "MCP connect failed",
						lines: [error instanceof Error ? error.message : String(error)],
						variant: "error",
					});
				}
			},
		});

		this.registerCommand({
			name: "mcp-login",
			argName: "server",
			description: "Authorize one OAuth-protected HTTP MCP server",
			execute: async (ctx: CommandContext) => {
				const serverName = ctx.args.trim();
				if (!serverName) {
					ctx.toast({
						title: "MCP login",
						lines: ["Provide a server name, for example: /mcp-login my-server"],
						variant: "warning",
					});
					return;
				}
				if (!this.manager) {
					ctx.toast({
						title: "MCP login",
						lines: ["No MCP servers are currently configured."],
						variant: "warning",
					});
					return;
				}

				const definition = this.manager.getDefinition(serverName);
				if (!definition) {
					ctx.toast({
						title: "MCP login",
						lines: [`Unknown MCP server: ${serverName}`],
						variant: "warning",
					});
					return;
				}
				if (definition.type !== "http" || definition.auth?.type !== "oauth") {
					ctx.toast({
						title: "MCP login",
						lines: [
							`${serverName} is not configured as an OAuth-protected HTTP MCP server.`,
						],
						variant: "warning",
					});
					return;
				}

				let callbackServer: Awaited<
					ReturnType<typeof startMcpOAuthCallbackServer>
				> | null = null;
				let authorizationUrl: string | null = null;
				try {
					callbackServer = await startMcpOAuthCallbackServer();
					const activeCallbackServer = callbackServer;
					const state = await this.manager.connectServer(serverName, {
						onAuthorizationUrl: async (url) => {
							authorizationUrl = url.toString();
							try {
								await openExternal(authorizationUrl);
							} catch {
								ctx.toast({
									title: "MCP login",
									lines: [
										"Open this authorization URL in your browser:",
										authorizationUrl,
									],
									variant: "warning",
								});
							}
						},
						getAuthorizationCode: () => activeCallbackServer.waitForCode(),
					});
					const tools = await this.manager
						.ensureTools(serverName)
						.catch(() => []);
					this.updateDebugSection();
					ctx.toast({
						title: "MCP authorized",
						lines: [
							state.status === "connected"
								? `${serverName} connected.`
								: `${serverName} is ${state.status}.`,
							authorizationUrl
								? "OAuth authorization completed."
								: "Existing OAuth session reused.",
							`Loaded ${tools.length} tools.`,
						],
						variant: "info",
					});
				} catch (error) {
					ctx.toast({
						title: "MCP login failed",
						lines: [
							error instanceof Error ? error.message : String(error),
							...(authorizationUrl
								? ["Authorization URL:", authorizationUrl]
								: []),
						],
						variant: "error",
					});
				} finally {
					await callbackServer?.close().catch(() => undefined);
				}
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
		this.manager = new McpManager(config.servers, cache, oauthStore, () => {
			this.updateDebugSection();
			void this.persistCache();
			void this.persistAuth();
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
		if (states.length === 0)
			return ["No MCP servers are currently configured."];
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
			return `${prefix} ${state.name} (${state.type})${state.toolCount > 0 ? ` · ${state.toolCount} tools` : ""}${state.cached ? " · cached" : ""}${state.lastError ? ` · ${state.lastError}` : ""}`;
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
				lines.push(
					`- ${state.name} · ${state.status} · ${state.type} · ${state.toolCount} tools${state.cached ? " · cached" : ""}${state.lastError ? ` · ${state.lastError}` : ""}`,
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
