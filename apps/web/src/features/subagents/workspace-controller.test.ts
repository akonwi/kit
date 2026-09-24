import { describe, expect, test } from "bun:test";
import {
	createSubagentsWorkspaceController,
	type SubagentsPanelData,
} from "./workspace-controller";

function panelData(): SubagentsPanelData {
	return {
		getAgents: () => [],
		getActiveConversations: () => [],
		readConversationEntries: async () => [],
		subscribeToChanges: () => () => {},
		dismissConversation: async () => true,
	};
}

describe("subagents workspace controller", () => {
	test("notifies open requests until unsubscribed", () => {
		const controller = createSubagentsWorkspaceController();
		let opens = 0;
		const unsubscribe = controller.onOpenRequest(() => {
			opens += 1;
		});

		controller.open();
		expect(opens).toBe(1);

		unsubscribe();
		controller.open();
		expect(opens).toBe(1);
	});

	test("registers and clears panel data with change notifications", () => {
		const controller = createSubagentsWorkspaceController();
		expect(controller.data()).toBeNull();

		let changes = 0;
		const unsubscribe = controller.subscribe(() => {
			changes += 1;
		});

		const data = panelData();
		controller.setData(data);
		expect(controller.data()).toBe(data);
		expect(changes).toBe(1);

		// Plugin reload: clear then re-register fresh providers.
		controller.setData(null);
		expect(controller.data()).toBeNull();
		expect(changes).toBe(2);

		unsubscribe();
		controller.setData(panelData());
		expect(changes).toBe(2);
	});
});
