import { describe, expect, test } from "bun:test";
import {
	DEFAULT_WORKSPACE_PANES,
	WORKSPACE_PANE_DEFINITIONS,
	type WorkspacePane,
	workspacePaneClosable,
	workspacePaneIdentity,
	workspacePaneLabel,
} from "./workspace-pane-registry";
import { createWorkspaceStateController } from "./workspace-state";

const paneKinds: WorkspacePane["kind"][] = [
	"activity",
	"file",
	"mermaid",
	"mcp",
	"review",
	"releases",
	"scratchpad",
	"subagents",
	"subagent",
];

describe("workspace pane registry", () => {
	test("defines every workspace pane kind", () => {
		expect(Object.keys(WORKSPACE_PANE_DEFINITIONS).sort()).toEqual(
			paneKinds.toSorted(),
		);
	});

	test("makes persistent singleton panes available by default", () => {
		expect(DEFAULT_WORKSPACE_PANES).toEqual([
			{ kind: "review" },
			{ kind: "scratchpad" },
		]);
		expect(
			DEFAULT_WORKSPACE_PANES.every((pane) => !workspacePaneClosable(pane)),
		).toBeTrue();
		expect(
			new Set(DEFAULT_WORKSPACE_PANES.map(workspacePaneIdentity)).size,
		).toBe(DEFAULT_WORKSPACE_PANES.length);
	});

	test("uses descriptor-specific identities for diagrams and agents", () => {
		expect(
			workspacePaneIdentity({
				kind: "activity",
				source: { kind: "single-item", itemId: "one" },
			}),
		).toBe("activity");
		expect(workspacePaneIdentity({ kind: "mcp" })).toBe("mcp");
		expect(workspacePaneIdentity({ kind: "review" })).toBe("review");
		expect(
			workspacePaneIdentity({
				kind: "file",
				repoRoot: "/repo",
				path: "src/index.ts",
			}),
		).toBe("file:/repo/src/index.ts");
		expect(workspacePaneIdentity({ kind: "mermaid", source: "graph TD" })).toBe(
			"mermaid:graph TD",
		);
		expect(
			workspacePaneIdentity({ kind: "subagent", agentName: "code-reviewer" }),
		).toBe("subagent:code-reviewer");
	});

	test("reuses one transcript tab per agent name", () => {
		const workspace = createWorkspaceStateController<WorkspacePane>({
			identityOf: workspacePaneIdentity,
		});
		const first = workspace.openSecondary({
			kind: "subagent",
			agentName: "code-reviewer",
		});
		const reopened = workspace.openSecondary({
			kind: "subagent",
			agentName: "code-reviewer",
		});
		const other = workspace.openSecondary({
			kind: "subagent",
			agentName: "designer",
		});

		expect(reopened).toBe(first);
		expect(other).not.toBe(first);
	});

	test("owns tab labels and close policy", () => {
		const diagrams: { pane: WorkspacePane }[] = [
			{ pane: { kind: "mermaid", source: "first" } },
			{ pane: { kind: "mermaid", source: "second" } },
		];
		expect(workspacePaneLabel(diagrams[0].pane, diagrams)).toBe("Diagram 1");
		expect(workspacePaneLabel(diagrams[1].pane, diagrams)).toBe("Diagram 2");
		expect(
			workspacePaneLabel(
				{ kind: "subagent", agentName: "code-reviewer" },
				diagrams,
			),
		).toBe("code-reviewer");
		expect(workspacePaneLabel({ kind: "mcp" }, diagrams)).toBe("MCP");
		expect(
			workspacePaneLabel(
				{ kind: "file", repoRoot: "/repo", path: "src/index.ts" },
				diagrams,
			),
		).toBe("index.ts");
		const duplicateFiles: { pane: WorkspacePane }[] = [
			{ pane: { kind: "file", repoRoot: "/repo", path: "src/index.ts" } },
			{ pane: { kind: "file", repoRoot: "/repo", path: "test/index.ts" } },
		];
		expect(workspacePaneLabel(duplicateFiles[0].pane, duplicateFiles)).toBe(
			"src/index.ts",
		);
		expect(
			workspacePaneClosable({
				kind: "file",
				repoRoot: "/repo",
				path: "src/index.ts",
			}),
		).toBeTrue();
		expect(workspacePaneClosable({ kind: "mcp" })).toBeTrue();
		expect(workspacePaneClosable({ kind: "review" })).toBeFalse();
		expect(workspacePaneClosable({ kind: "scratchpad" })).toBeFalse();
		expect(
			workspacePaneClosable({
				kind: "activity",
				source: { kind: "single-item", itemId: "one" },
			}),
		).toBeTrue();
	});
});
