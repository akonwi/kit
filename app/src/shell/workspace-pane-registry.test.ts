import { describe, expect, test } from "bun:test";
import {
	WORKSPACE_PANE_DEFINITIONS,
	type WorkspacePane,
	workspacePaneClosable,
	workspacePaneIdentity,
	workspacePaneLabel,
	workspacePaneMinColumns,
} from "./workspace-pane-registry";

const paneKinds: WorkspacePane["kind"][] = [
	"activity",
	"mermaid",
	"review",
	"releases",
	"scratchpad",
	"subagents",
];

describe("workspace pane registry", () => {
	test("defines every workspace pane kind", () => {
		expect(Object.keys(WORKSPACE_PANE_DEFINITIONS).sort()).toEqual(
			paneKinds.toSorted(),
		);
		for (const kind of paneKinds) {
			expect(WORKSPACE_PANE_DEFINITIONS[kind].minColumns).toBeGreaterThan(0);
		}
	});

	test("uses singleton identities except for source-specific diagrams", () => {
		expect(
			workspacePaneIdentity({
				kind: "activity",
				source: { kind: "single-item", itemId: "one" },
			}),
		).toBe("activity");
		expect(workspacePaneIdentity({ kind: "review" })).toBe("review");
		expect(workspacePaneIdentity({ kind: "mermaid", source: "graph TD" })).toBe(
			"mermaid:graph TD",
		);
	});

	test("owns tab labels, close policy, and minimum widths", () => {
		const diagrams: { pane: WorkspacePane }[] = [
			{ pane: { kind: "mermaid", source: "first" } },
			{ pane: { kind: "mermaid", source: "second" } },
		];
		expect(workspacePaneLabel(diagrams[0].pane, diagrams)).toBe("Diagram 1");
		expect(workspacePaneLabel(diagrams[1].pane, diagrams)).toBe("Diagram 2");
		expect(workspacePaneClosable({ kind: "review" })).toBeFalse();
		expect(workspacePaneClosable({ kind: "scratchpad" })).toBeFalse();
		expect(
			workspacePaneClosable({
				kind: "activity",
				source: { kind: "single-item", itemId: "one" },
			}),
		).toBeTrue();
		expect(workspacePaneMinColumns({ kind: "review" })).toBeGreaterThan(
			workspacePaneMinColumns({
				kind: "activity",
				source: { kind: "single-item", itemId: "one" },
			}),
		);
	});
});
