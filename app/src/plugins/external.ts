import { type Dirent, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import type { TSchema } from "typebox";
import { Check } from "typebox/schema";
import manifestSchema from "../../docs/plugin-protocol/manifest.schema.json";
import { getKitPaths } from "../paths";
import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import { ExternalPluginClient, publicTurn } from "./external-client";
import type { JsonValue } from "./json-rpc";
import type { PluginContext } from "./types";

export type ExternalPluginSource = "project" | "user";

export type ExternalPluginFailurePhase =
	| "duplicate"
	| "initialize"
	| "launch"
	| "manifest"
	| "protocol"
	| "runtime"
	| "shutdown";

export type ExternalPluginFailure = {
	source: ExternalPluginSource;
	phase: ExternalPluginFailurePhase;
	pluginId?: string;
	manifestPath: string;
	otherManifestPath?: string;
	message: string;
	exitCode?: number | null;
	exitSignal?: NodeJS.Signals | null;
	stderr?: string;
};

export type PluginManifestDocument = {
	$schema?: string;
	manifestVersion: 1;
	id: string;
	name?: string;
	transport: {
		type: "stdio";
		command: string;
		args?: string[];
	};
};

export type ExternalPluginManifest = {
	source: ExternalPluginSource;
	installationName: string;
	root: string;
	manifestPath: string;
	manifest: PluginManifestDocument;
};

export type DiscoverExternalPluginsOptions = {
	home?: string;
	includeUser?: boolean;
	includeProject?: boolean;
	existingManifests?: ExternalPluginManifest[];
};

export type DiscoverExternalPluginsResult = {
	manifests: ExternalPluginManifest[];
	failures: ExternalPluginFailure[];
};

const pluginManifestSchema = manifestSchema as TSchema;

function scanInstallations(
	directory: string,
	source: ExternalPluginSource,
): Array<
	Pick<
		ExternalPluginManifest,
		"installationName" | "manifestPath" | "root" | "source"
	>
> {
	let entries: Dirent[];
	try {
		entries = readdirSync(directory, { withFileTypes: true });
	} catch {
		return [];
	}

	const installations = [];
	for (const entry of entries.sort((a, b) =>
		a.name < b.name ? -1 : a.name > b.name ? 1 : 0,
	)) {
		const root = path.join(directory, entry.name);
		let directoryEntry = entry.isDirectory();
		if (entry.isSymbolicLink()) {
			try {
				directoryEntry = statSync(root).isDirectory();
			} catch {
				continue;
			}
		}
		if (!directoryEntry) continue;
		const manifestPath = path.join(root, "plugin.json");
		try {
			if (!statSync(manifestPath).isFile()) continue;
		} catch {
			continue;
		}
		installations.push({
			source,
			installationName: entry.name,
			root,
			manifestPath,
		});
	}
	return installations;
}

function parseManifest(
	installation: Pick<
		ExternalPluginManifest,
		"installationName" | "manifestPath" | "root" | "source"
	>,
): ExternalPluginManifest | ExternalPluginFailure {
	let value: unknown;
	try {
		value = JSON.parse(readFileSync(installation.manifestPath, "utf8"));
	} catch (error) {
		return {
			...installation,
			phase: "manifest",
			message: `Could not parse plugin.json: ${error instanceof Error ? error.message : String(error)}`,
		};
	}
	if (!Check(pluginManifestSchema, value)) {
		return {
			...installation,
			phase: "manifest",
			pluginId:
				typeof (value as { id?: unknown })?.id === "string"
					? String((value as { id: string }).id)
					: undefined,
			message: "plugin.json does not match the Kit manifest v1 schema",
		};
	}
	return { ...installation, manifest: value as PluginManifestDocument };
}

function isFailure(
	value: ExternalPluginManifest | ExternalPluginFailure,
): value is ExternalPluginFailure {
	return "phase" in value;
}

export function discoverExternalPluginManifests(
	cwd: string,
	options: DiscoverExternalPluginsOptions = {},
): DiscoverExternalPluginsResult {
	const includeUser = options.includeUser ?? true;
	const includeProject = options.includeProject ?? true;
	const kitRoot = getKitPaths(options.home).kitRoot;
	const installations = [
		...(includeUser
			? scanInstallations(path.join(kitRoot, "plugins"), "user")
			: []),
		...(includeProject
			? scanInstallations(path.resolve(cwd, ".kit", "plugins"), "project")
			: []),
	];
	const failures: ExternalPluginFailure[] = [];
	const parsed: ExternalPluginManifest[] = [];
	for (const installation of installations) {
		const result = parseManifest(installation);
		if (isFailure(result)) failures.push(result);
		else parsed.push(result);
	}

	const owners = new Map<string, ExternalPluginManifest>();
	for (const manifest of options.existingManifests ?? []) {
		owners.set(manifest.manifest.id, manifest);
	}
	const manifests: ExternalPluginManifest[] = [];
	for (const candidate of parsed) {
		const id = candidate.manifest.id;
		const owner = owners.get(id);
		if (owner) {
			failures.push({
				source: candidate.source,
				phase: "duplicate",
				pluginId: id,
				manifestPath: candidate.manifestPath,
				otherManifestPath: owner.manifestPath,
				message: `Duplicate plugin id ${id}; ${owner.manifestPath} was discovered first`,
			});
			continue;
		}
		owners.set(id, candidate);
		manifests.push(candidate);
	}
	return { manifests, failures };
}

function failureSubtitle(failure: ExternalPluginFailure): string {
	const details = [
		`${failure.phase}: ${failure.message}`,
		failure.otherManifestPath
			? `${failure.otherManifestPath} · ${failure.manifestPath}`
			: failure.manifestPath,
		failure.exitCode != null ? `exit ${failure.exitCode}` : undefined,
		failure.exitSignal ? `signal ${failure.exitSignal}` : undefined,
		failure.stderr ? `stderr: ${failure.stderr}` : undefined,
	].filter((value): value is string => Boolean(value));
	return details.join(" · ");
}

export class ExternalPluginManager {
	private readonly context: PluginContext;
	private readonly home?: string;
	private readonly onFailure?: (failure: ExternalPluginFailure) => void;
	private userManifests: ExternalPluginManifest[] = [];
	private userClients: ExternalPluginClient[] = [];
	private projectClients: ExternalPluginClient[] = [];
	private allClients: ExternalPluginClient[] = [];
	private currentCwd: string;
	private unsubscribeRuntime: (() => void) | null = null;
	private lifecycleGeneration = 0;
	private projectGeneration = 0;
	private projectTransition: Promise<void> = Promise.resolve();
	private disposed = false;

	constructor(
		context: PluginContext,
		options: {
			home?: string;
			onFailure?: (failure: ExternalPluginFailure) => void;
		} = {},
	) {
		this.context = context;
		this.home = options.home;
		this.onFailure = options.onFailure;
		this.currentCwd = context.runtime.getSession().cwd;
	}

	async initialize(): Promise<void> {
		if (this.disposed) return;
		this.subscribeToRuntime();
		const lifecycleGeneration = ++this.lifecycleGeneration;
		const projectGeneration = ++this.projectGeneration;
		this.currentCwd = this.context.runtime.getSession().cwd;
		const discovered = discoverExternalPluginManifests(this.currentCwd, {
			home: this.home,
		});
		for (const failure of discovered.failures) this.reportFailure(failure);
		this.userManifests = discovered.manifests.filter(
			(manifest) => manifest.source === "user",
		);
		for (const manifest of discovered.manifests) {
			if (lifecycleGeneration !== this.lifecycleGeneration || this.disposed) {
				return;
			}
			if (
				manifest.source === "project" &&
				projectGeneration !== this.projectGeneration
			) {
				return;
			}
			await this.startManifest(
				manifest,
				lifecycleGeneration,
				projectGeneration,
			);
		}
	}

	async reload(): Promise<void> {
		if (this.disposed) return;
		const lifecycleGeneration = ++this.lifecycleGeneration;
		const projectGeneration = ++this.projectGeneration;
		await this.projectTransition;
		const clients = this.allClients.splice(0).reverse();
		this.projectClients = [];
		this.userClients = [];
		await this.stopClients(clients);
		if (lifecycleGeneration !== this.lifecycleGeneration || this.disposed) {
			return;
		}
		this.userManifests = [];
		await this.initializeCurrentGeneration(
			lifecycleGeneration,
			projectGeneration,
		);
	}

	retargetProject(cwd: string): void {
		if (this.disposed || cwd === this.currentCwd) return;
		const generation = ++this.projectGeneration;
		const lifecycleGeneration = this.lifecycleGeneration;
		const oldProjectClients = this.allClients
			.filter((client) => client.manifest.source === "project")
			.reverse();
		this.allClients = this.allClients.filter(
			(client) => client.manifest.source !== "project",
		);
		this.projectClients = [];
		this.currentCwd = cwd;
		this.projectTransition = this.projectTransition.then(async () => {
			await this.stopClients(oldProjectClients);
			if (
				this.disposed ||
				generation !== this.projectGeneration ||
				lifecycleGeneration !== this.lifecycleGeneration
			) {
				return;
			}
			this.notifyClients(this.userClients, "kit/events/project.changed", {
				cwd,
				git: this.context.runtime.vcsInfo,
			});

			const discovered = discoverExternalPluginManifests(cwd, {
				home: this.home,
				includeUser: false,
				includeProject: true,
				existingManifests: this.userManifests,
			});
			for (const failure of discovered.failures) this.reportFailure(failure);
			await this.startProjectManifests(
				discovered.manifests,
				lifecycleGeneration,
				generation,
			);
		});
	}

	async dispose(): Promise<void> {
		if (this.disposed) return;
		this.disposed = true;
		this.lifecycleGeneration += 1;
		this.projectGeneration += 1;
		this.unsubscribeRuntime?.();
		this.unsubscribeRuntime = null;
		await this.projectTransition;
		const clients = this.allClients.splice(0).reverse();
		this.projectClients = [];
		this.userClients = [];
		await this.stopClients(clients);
	}

	private async stopClients(clients: ExternalPluginClient[]): Promise<void> {
		for (const client of clients) await client.stop();
	}

	private async initializeCurrentGeneration(
		lifecycleGeneration: number,
		projectGeneration: number,
	): Promise<void> {
		this.currentCwd = this.context.runtime.getSession().cwd;
		const discovered = discoverExternalPluginManifests(this.currentCwd, {
			home: this.home,
		});
		for (const failure of discovered.failures) this.reportFailure(failure);
		this.userManifests = discovered.manifests.filter(
			(manifest) => manifest.source === "user",
		);
		for (const manifest of discovered.manifests) {
			if (lifecycleGeneration !== this.lifecycleGeneration || this.disposed) {
				return;
			}
			if (
				manifest.source === "project" &&
				projectGeneration !== this.projectGeneration
			) {
				return;
			}
			await this.startManifest(
				manifest,
				lifecycleGeneration,
				projectGeneration,
			);
		}
	}

	private async startProjectManifests(
		manifests: ExternalPluginManifest[],
		lifecycleGeneration: number,
		projectGeneration: number,
	): Promise<void> {
		for (const manifest of manifests) {
			if (
				lifecycleGeneration !== this.lifecycleGeneration ||
				projectGeneration !== this.projectGeneration ||
				this.disposed
			) {
				return;
			}
			await this.startManifest(
				manifest,
				lifecycleGeneration,
				projectGeneration,
			);
		}
	}

	private async startManifest(
		manifest: ExternalPluginManifest,
		lifecycleGeneration: number,
		projectGeneration: number,
	): Promise<void> {
		const client = new ExternalPluginClient({
			manifest,
			context: this.context,
			onFailure: (failure) => this.reportFailure(failure),
		});
		this.allClients.push(client);
		try {
			await client.start();
		} catch {
			this.allClients = this.allClients.filter(
				(candidate) => candidate !== client,
			);
			return;
		}
		if (
			lifecycleGeneration !== this.lifecycleGeneration ||
			(manifest.source === "project" &&
				projectGeneration !== this.projectGeneration) ||
			this.disposed
		) {
			this.allClients = this.allClients.filter(
				(candidate) => candidate !== client,
			);
			await client.stop();
			return;
		}
		if (manifest.source === "user") this.userClients.push(client);
		else this.projectClients.push(client);
	}

	private subscribeToRuntime(): void {
		if (this.unsubscribeRuntime) return;
		this.unsubscribeRuntime = this.context.runtime.subscribe((event) =>
			this.handleRuntimeEvent(event),
		);
	}

	private handleRuntimeEvent(event: AgentRuntimeEvent): void {
		switch (event.type) {
			case "vcs.updated":
				this.notifyAll("kit/events/git.changed", { git: event.vcs });
				break;
			case "session.active.changed":
				if (event.session.cwd !== this.currentCwd) {
					this.retargetProject(event.session.cwd);
				}
				this.notifyAll("kit/events/session.changed", {
					id: event.session.id,
					name: event.session.name ?? null,
				});
				break;
			case "session.name.changed":
				this.notifyAll("kit/events/session.changed", {
					id: event.session.id,
					name: event.name ?? null,
				});
				break;
			case "agent.turn.started":
				this.notifyAll("kit/events/agent.turn.started", {
					sessionId: this.context.runtime.getSession().id,
					turnId: event.turn.id,
				});
				break;
			case "agent.turn.completed":
				if (event.turn) {
					this.notifyAll("kit/events/agent.turn.completed", {
						sessionId: this.context.runtime.getSession().id,
						turn: publicTurn(event.turn),
					});
				}
				break;
		}
	}

	private notifyAll(method: string, params: JsonValue): void {
		this.notifyClients(
			[...this.userClients, ...this.projectClients],
			method,
			params,
		);
	}

	private notifyClients(
		clients: ExternalPluginClient[],
		method: string,
		params: JsonValue,
	): void {
		for (const client of clients) client.notify(method, params);
	}

	private reportFailure(failure: ExternalPluginFailure): void {
		this.onFailure?.(failure);
		if (this.onFailure) return;
		this.context.ui.toast({
			title: failure.pluginId
				? `Plugin ${failure.pluginId} failed`
				: "Plugin failed",
			subtitle: failureSubtitle(failure),
			variant: "error",
			persistent: true,
		});
	}
}
