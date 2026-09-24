import { describe, expect, test } from "bun:test";
import { createMcpWorkspaceController } from "./workspace-controller";

describe("MCP workspace controller", () => {
	test("bridges plugin data and open requests to the shell", () => {
		const controller = createMcpWorkspaceController();
		expect(controller.open()).toBeFalse();
		let opens = 0;
		let dataChanges = 0;
		const stopOpening = controller.onOpenRequest(() => opens++);
		controller.subscribe(() => dataChanges++);

		const data = {
			getStates: () => [],
			getConfig: () => null,
			hasOAuthSession: () => false,
			subscribeToChanges: () => () => {},
		};
		controller.setData(data);

		expect(controller.open()).toBeTrue();
		expect(controller.data()).toBe(data);
		expect(dataChanges).toBe(1);
		expect(opens).toBe(1);
		stopOpening();
		expect(controller.open()).toBeFalse();

		controller.setData(null);
		expect(controller.data()).toBeNull();
		expect(dataChanges).toBe(2);
	});
});
