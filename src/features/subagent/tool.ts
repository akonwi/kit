// @ts-nocheck — disabled pending rewrite
/**
 * Subagent tool — registered with the agent session so the model
 * can delegate tasks to specialized subagents with isolated context.
 */

import type { Api, Model } from "@mariozechner/pi-ai";
import type { ModelRegistry } from "@mariozechner/pi-coding-agent";
import { Type } from "@sinclair/typebox";
import { type AgentScope, discoverAgents } from "./agents";
import {
	getFinalOutput,
	mapWithConcurrencyLimit,
	type OnUpdateCallback,
	runSingleAgent,
	type SingleResult,
} from "./run-agent";

const MAX_PARALLEL_TASKS = 8;
const MAX_CONCURRENCY = 4;

// ── Details type ─────────────────────────────────────────────────

interface SubagentDetails {
	mode: "single" | "parallel" | "chain";
	agentScope: AgentScope;
	projectAgentsDir: string | null;
	results: SingleResult[];
}

// ── Tool factory ─────────────────────────────────────────────────

/**
 * Dependencies are resolved lazily (via getters) to break the circular
 * dependency between runtime creation and tool registration.
 */
export interface SubagentToolDeps {
	getModelRegistry: () => ModelRegistry;
	getParentModel: () => Model<Api> | undefined;
	getCwd: () => string;
}

export function createSubagentTool(deps: SubagentToolDeps) {
	const TaskItem = Type.Object({
		agent: Type.String({ description: "Name of the agent to invoke" }),
		task: Type.String({ description: "Task to delegate to the agent" }),
		cwd: Type.Optional(
			Type.String({ description: "Working directory for the agent process" }),
		),
	});

	const ChainItem = Type.Object({
		agent: Type.String({ description: "Name of the agent to invoke" }),
		task: Type.String({
			description: "Task with optional {previous} placeholder for prior output",
		}),
		cwd: Type.Optional(
			Type.String({ description: "Working directory for the agent process" }),
		),
	});

	const SubagentParams = Type.Object({
		agent: Type.Optional(
			Type.String({
				description: "Name of the agent to invoke (for single mode)",
			}),
		),
		task: Type.Optional(
			Type.String({ description: "Task to delegate (for single mode)" }),
		),
		tasks: Type.Optional(
			Type.Array(TaskItem, {
				description: "Array of {agent, task} for parallel execution",
			}),
		),
		chain: Type.Optional(
			Type.Array(ChainItem, {
				description: "Array of {agent, task} for sequential execution",
			}),
		),
		agentScope: Type.Optional(
			Type.String({
				description:
					'Which agent directories to use. Default: "user". Use "both" to include project-local agents.',
				enum: ["user", "project", "both"],
				default: "user",
			}),
		),
		confirmProjectAgents: Type.Optional(
			Type.Boolean({
				description:
					"Prompt before running project-local agents. Default: true.",
				default: true,
			}),
		),
		cwd: Type.Optional(
			Type.String({
				description: "Working directory for the agent process (single mode)",
			}),
		),
	});

	return {
		name: "subagent",
		label: "Subagent",
		description: [
			"Delegate tasks to specialized subagents with isolated context.",
			"Modes: single (agent + task), parallel (tasks array), chain (sequential with {previous} placeholder).",
			'Default agent scope is "user" (from ~/.pi/agent/agents).',
			'To enable project-local agents in .pi/agents, set agentScope: "both" (or "project").',
		].join(" "),
		get promptSnippet() {
			const agents = discoverAgents(deps.getCwd(), "both").agents;
			const agentList =
				agents.length > 0
					? agents.map((a) => `- ${a.name}: ${a.description}`).join("\n")
					: "(none discovered)";
			return (
				"Delegate tasks to specialized subagents with isolated context. " +
				"Modes: single (agent + task), parallel (tasks array), chain (sequential with {previous} placeholder). " +
				'Default agent scope is "user" (from ~/.pi/agent/agents). ' +
				'To enable project-local agents in .pi/agents, set agentScope: "both" (or "project").\n\n' +
				"Available agents:\n" +
				agentList +
				"\n\n" +
				"When the user references an agent with @name (e.g. @summarizer), delegate the task to that agent using this tool."
			);
		},
		parameters: SubagentParams,

		async execute(
			_toolCallId: string,
			params: any,
			signal: AbortSignal | undefined,
			onUpdate: OnUpdateCallback | undefined,
			_ctx: any,
		) {
			const cwd = deps.getCwd();
			const agentScope: AgentScope = params.agentScope ?? "user";
			const discovery = discoverAgents(cwd, agentScope);
			const agents = discovery.agents;

			const hasChain = (params.chain?.length ?? 0) > 0;
			const hasTasks = (params.tasks?.length ?? 0) > 0;
			const hasSingle = Boolean(params.agent && params.task);
			const modeCount = Number(hasChain) + Number(hasTasks) + Number(hasSingle);

			const makeDetails =
				(mode: "single" | "parallel" | "chain") =>
				(results: SingleResult[]): SubagentDetails => ({
					mode,
					agentScope,
					projectAgentsDir: discovery.projectAgentsDir,
					results,
				});

			if (modeCount !== 1) {
				const available =
					agents.map((a) => `${a.name} (${a.source})`).join(", ") || "none";
				return {
					content: [
						{
							type: "text",
							text: `Invalid parameters. Provide exactly one mode.\nAvailable agents: ${available}`,
						},
					],
					details: makeDetails("single")([]),
				};
			}

			// ── Chain mode ───────────────────────────────────────────

			if (params.chain && params.chain.length > 0) {
				const results: SingleResult[] = [];
				let previousOutput = "";

				for (let i = 0; i < params.chain.length; i++) {
					const step = params.chain[i];
					const taskWithContext = step.task.replace(
						/\{previous\}/g,
						previousOutput,
					);

					const chainUpdate: OnUpdateCallback | undefined = onUpdate
						? (partial) => {
								const currentResult = (partial.details as any)?.results?.[0];
								if (currentResult) {
									const allResults = [...results, currentResult];
									onUpdate({
										content: partial.content,
										details: makeDetails("chain")(allResults),
									});
								}
							}
						: undefined;

					const result = await runSingleAgent({
						cwd,
						agents,
						agentName: step.agent,
						task: taskWithContext,
						taskCwd: step.cwd,
						step: i + 1,
						signal,
						onUpdate: chainUpdate,
						makeDetails: makeDetails("chain"),
						modelRegistry: deps.getModelRegistry(),
						parentModel: deps.getParentModel(),
					});
					results.push(result);

					const isError =
						result.exitCode !== 0 ||
						result.stopReason === "error" ||
						result.stopReason === "aborted";
					if (isError) {
						const errorMsg =
							result.errorMessage ||
							getFinalOutput(result.messages) ||
							"(no output)";
						return {
							content: [
								{
									type: "text",
									text: `Chain stopped at step ${i + 1} (${step.agent}): ${errorMsg}`,
								},
							],
							details: makeDetails("chain")(results),
							isError: true,
						};
					}
					previousOutput = getFinalOutput(result.messages);
				}

				return {
					content: [
						{
							type: "text",
							text:
								getFinalOutput(results[results.length - 1].messages) ||
								"(no output)",
						},
					],
					details: makeDetails("chain")(results),
				};
			}

			// ── Parallel mode ────────────────────────────────────────

			if (params.tasks && params.tasks.length > 0) {
				if (params.tasks.length > MAX_PARALLEL_TASKS) {
					return {
						content: [
							{
								type: "text",
								text: `Too many parallel tasks (${params.tasks.length}). Max is ${MAX_PARALLEL_TASKS}.`,
							},
						],
						details: makeDetails("parallel")([]),
					};
				}

				const allResults: SingleResult[] = new Array(params.tasks.length);

				// Initialize placeholder results
				for (let i = 0; i < params.tasks.length; i++) {
					allResults[i] = {
						agent: params.tasks[i].agent,
						agentSource: "unknown",
						task: params.tasks[i].task,
						exitCode: -1, // -1 = still running
						messages: [],
						usage: {
							input: 0,
							output: 0,
							cacheRead: 0,
							cacheWrite: 0,
							cost: 0,
							contextTokens: 0,
							turns: 0,
						},
					};
				}

				const emitParallelUpdate = () => {
					if (onUpdate) {
						const running = allResults.filter((r) => r.exitCode === -1).length;
						const done = allResults.filter((r) => r.exitCode !== -1).length;
						onUpdate({
							content: [
								{
									type: "text",
									text: `Parallel: ${done}/${allResults.length} done, ${running} running...`,
								},
							],
							details: makeDetails("parallel")([...allResults]),
						});
					}
				};

				const results = await mapWithConcurrencyLimit(
					params.tasks,
					MAX_CONCURRENCY,
					async (t: any, index: number) => {
						const result = await runSingleAgent({
							cwd,
							agents,
							agentName: t.agent,
							task: t.task,
							taskCwd: t.cwd,
							signal,
							onUpdate: (partial) => {
								if ((partial.details as any)?.results?.[0]) {
									allResults[index] = (partial.details as any).results[0];
									emitParallelUpdate();
								}
							},
							makeDetails: makeDetails("parallel"),
							modelRegistry: deps.getModelRegistry(),
							parentModel: deps.getParentModel(),
						});
						allResults[index] = result;
						emitParallelUpdate();
						return result;
					},
				);

				const successCount = results.filter((r) => r.exitCode === 0).length;
				const summaries = results.map((r) => {
					const output = getFinalOutput(r.messages);
					const preview =
						output.slice(0, 100) + (output.length > 100 ? "..." : "");
					return `[${r.agent}] ${r.exitCode === 0 ? "completed" : "failed"}: ${preview || "(no output)"}`;
				});

				return {
					content: [
						{
							type: "text",
							text: `Parallel: ${successCount}/${results.length} succeeded\n\n${summaries.join("\n\n")}`,
						},
					],
					details: makeDetails("parallel")(results),
				};
			}

			// ── Single mode ──────────────────────────────────────────

			if (params.agent && params.task) {
				const result = await runSingleAgent({
					cwd,
					agents,
					agentName: params.agent,
					task: params.task,
					taskCwd: params.cwd,
					signal,
					onUpdate,
					makeDetails: makeDetails("single"),
					modelRegistry: deps.getModelRegistry(),
					parentModel: deps.getParentModel(),
				});

				const isError =
					result.exitCode !== 0 ||
					result.stopReason === "error" ||
					result.stopReason === "aborted";
				if (isError) {
					const errorMsg =
						result.errorMessage ||
						getFinalOutput(result.messages) ||
						"(no output)";
					return {
						content: [
							{
								type: "text",
								text: `Agent ${result.stopReason || "failed"}: ${errorMsg}`,
							},
						],
						details: makeDetails("single")([result]),
						isError: true,
					};
				}

				return {
					content: [
						{
							type: "text",
							text: getFinalOutput(result.messages) || "(no output)",
						},
					],
					details: makeDetails("single")([result]),
				};
			}

			// ── Fallback ─────────────────────────────────────────────

			const available =
				agents.map((a) => `${a.name} (${a.source})`).join(", ") || "none";
			return {
				content: [
					{
						type: "text",
						text: `Invalid parameters. Available agents: ${available}`,
					},
				],
				details: makeDetails("single")([]),
			};
		},
	};
}
