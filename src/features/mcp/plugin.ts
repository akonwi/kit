import type { AgentTool } from "@mariozechner/pi-agent-core";
import { createComponent } from "solid-js";
import type { PluginAPI } from "../../plugins";
import { loadMcpConfig } from "./config";
import { McpStatusModal } from "./McpStatusModal";
import { McpManager } from "./manager";
import { loadMcpMetadataCache, saveMcpMetadataCache } from "./metadata-cache";
import { startMcpOAuthCallbackServer } from "./oauth-callback";
import { loadMcpOAuthStore, saveMcpOAuthStore } from "./oauth-store";
import { createMcpProxyTool, MCP_PROXY_POLICY } from "./proxy-tool";
import type { LoadMcpConfigResult } from "./types";

export function McpPlugin(kit: PluginAPI): () => void {
	let manager: McpManager | null = null;
	let unregisterTool: (() => void) | null = null;
	let removePolicy: (() => void) | null = null;
	let clearDebugSection: (() => void) | null = null;
	let lastConfig: LoadMcpConfigResult | null = null;
	let saveCachePromise = Promise.resolve();
	let saveAuthPromise = Promise.resolve();
	let disposed = false;

	async function authorizeOAuthServer(
		serverName: string,
		authorizationUrl: URL,
	): Promise<string> {
		let callbackServer: Awaited<
			ReturnType<typeof startMcpOAuthCallbackServer>
		> | null = null;
		let timeoutId: ReturnType<typeof setTimeout> | null = null;
		try {
			callbackServer = await startMcpOAuthCallbackServer();
			try {
				await kit.system.open(authorizationUrl);
				kit.ui.toast({
					title: "MCP login required",
					lines: [
						`Complete login for ${serverName} in your browser.`,
						"Kit will continue automatically when authorization finishes.",
					],
					variant: "info",
				});
			} catch {
				kit.ui.toast({
					title: "MCP login required",
					lines: [
						`Open this authorization URL for ${serverName}:`,
						authorizationUrl.toString(),
					],
					variant: "warning",
				});
			}
			const code = await Promise.race([
				callbackServer.waitForCode(),
				new Promise<string>((_resolve, reject) => {
					timeoutId = setTimeout(() => {
						reject(
							new Error(
								`Timed out waiting for MCP authorization for ${serverName} after 1 minute.`,
							),
						);
					}, 60_000);
				}),
			]);
			kit.ui.toast({
				title: "MCP authorized",
				lines: [`Authorization complete for ${serverName}.`],
				variant: "info",
			});
			return code;
		} finally {
			if (timeoutId) clearTimeout(timeoutId);
			await callbackServer?.close().catch(() => undefined);
		}
	}

	async function persistCache(): Promise<void> {
		if (!manager) return;
		const snapshot = manager.getPersistentCache();
		saveCachePromise = saveCachePromise
			.catch(() => undefined)
			.then(() => saveMcpMetadataCache(snapshot));
		await saveCachePromise;
	}

	async function persistAuth(): Promise<void> {
		if (!manager) return;
		const snapshot = manager.getPersistentOAuthStore();
		saveAuthPromise = saveAuthPromise
			.catch(() => undefined)
			.then(() => saveMcpOAuthStore(snapshot));
		await saveAuthPromise;
	}

	function updateDebugSection(): void {
		clearDebugSection?.();
		const lines: string[] = [];
		const files = lastConfig?.files ?? [];
		if (files.length > 0) {
			lines.push("Files:");
			for (const file of files) {
				lines.push(
					`- ${file.loaded ? "loaded" : "missing"} · ${file.source} · ${file.filePath}`,
				);
			}
		}
		const warnings = lastConfig?.warnings ?? [];
		if (warnings.length > 0) {
			lines.push("Warnings:");
			for (const warning of warnings) lines.push(`- ${warning}`);
		}
		const states = manager?.getRuntimeStates() ?? [];
		if (states.length > 0) {
			lines.push("Servers:");
			for (const state of states) {
				const oauth = manager?.hasOAuthSession(state.name)
					? " · oauth saved"
					: "";
				lines.push(
					`- ${state.name} · ${state.status} · ${state.type} · ${state.toolCount} tools${state.cached ? " · cached" : ""}${oauth}${state.lastError ? ` · ${state.lastError}` : ""}`,
				);
				const authorizationUrl = manager?.getPendingAuthorizationUrl(
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
		clearDebugSection = kit.addDebugSection("MCP", lines);
	}

	async function refresh(): Promise<void> {
		const cwd = kit.session.get().cwd;
		const [config, cache, oauthStore] = await Promise.all([
			loadMcpConfig(cwd),
			loadMcpMetadataCache(),
			loadMcpOAuthStore(),
		]);
		if (disposed) return;
		lastConfig = config;

		unregisterTool?.();
		unregisterTool = null;
		removePolicy?.();
		removePolicy = null;
		await manager?.dispose();
		if (disposed) return;

		const enabledServers = config.servers.filter((server) => !server.disabled);
		manager = new McpManager(config.servers, cache, oauthStore, {
			onStateChange: () => {
				updateDebugSection();
				void persistCache();
				void persistAuth();
			},
			authorizeOAuthServer: (serverName, authorizationUrl) =>
				authorizeOAuthServer(serverName, authorizationUrl),
			onRecoverableAuthError: (serverName, message) => {
				kit.ui.toast({
					title: `MCP reauthorizing: ${serverName}`,
					lines: [message],
					variant: "warning",
				});
			},
		});
		if (enabledServers.length > 0) {
			const tool = createMcpProxyTool(manager);
			unregisterTool = kit.registerTool(tool as AgentTool);
			removePolicy = kit.addSystemPrompt(MCP_PROXY_POLICY());
		}

		updateDebugSection();
		await Promise.all([persistCache(), persistAuth()]);
	}

	kit.on("session.active.changed", async () => {
		await refresh();
	});

	kit.registerCommand(
		"mcp-status",
		{ description: "Open a modal showing configured MCP server status" },
		async (ctx) => {
			await ctx.ui.custom<void>((props) =>
				createComponent(McpStatusModal, {
					surfaceProps: props.surfaceProps,
					states: manager?.getRuntimeStates() ?? [],
					config: lastConfig,
					hasOAuthSession: (serverName: string) =>
						manager?.hasOAuthSession(serverName) ?? false,
					onClose: () => props.done(undefined),
				}),
			);
		},
	);

	kit.registerCommand(
		"mcp-logout",
		{
			description: "Clear Kit's saved OAuth state for one MCP server",
			argName: "server",
		},
		async (ctx) => {
			const serverName = ctx.args.trim();
			if (!serverName) {
				ctx.ui.toast({
					title: "MCP logout",
					lines: ["Provide a server name, for example: /mcp-logout my-server"],
					variant: "warning",
				});
				return;
			}
			if (!manager) {
				ctx.ui.toast({
					title: "MCP logout",
					lines: ["No MCP servers are currently configured."],
					variant: "warning",
				});
				return;
			}
			const definition = manager.getDefinition(serverName);
			if (!definition) {
				ctx.ui.toast({
					title: "MCP logout",
					lines: [`Unknown MCP server: ${serverName}`],
					variant: "warning",
				});
				return;
			}
			if (definition.type !== "http" || definition.auth?.type !== "oauth") {
				ctx.ui.toast({
					title: "MCP logout",
					lines: [`${serverName} does not have Kit-managed OAuth state.`],
					variant: "warning",
				});
				return;
			}
			const hadSession = manager.hasOAuthSession(serverName);
			await manager.clearOAuthSession(serverName);
			updateDebugSection();
			ctx.ui.toast({
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
	);

	void refresh();

	return () => {
		disposed = true;
		unregisterTool?.();
		unregisterTool = null;
		removePolicy?.();
		removePolicy = null;
		clearDebugSection?.();
		clearDebugSection = null;
		void manager?.dispose();
		manager = null;
	};
}
