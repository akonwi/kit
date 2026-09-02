import { describe, expect, test } from "bun:test";
import { createGuidedQuestionsController } from "./controller";

const question = (id: string) => ({
	id,
	kind: "text" as const,
	label: `Question ${id}`,
	required: true,
});

describe("guided questions controller lifecycle", () => {
	test("serializes concurrent activations without losing either resolver", async () => {
		const controller = createGuidedQuestionsController();
		const first = controller.activate({ questions: [question("first")] });
		const second = controller.activate({ questions: [question("second")] });

		expect(controller.questions[0]?.id).toBe("first");
		controller.cancel();
		expect((await first).cancelled).toBe(true);
		await Promise.resolve();
		expect(controller.questions[0]?.id).toBe("second");
		controller.cancel();
		expect((await second).cancelled).toBe(true);
	});

	test("cancels active and queued activations when aborted", async () => {
		const controller = createGuidedQuestionsController();
		const activeAbort = new AbortController();
		const queuedAbort = new AbortController();
		const active = controller.activate(
			{ questions: [question("active")] },
			activeAbort.signal,
		);
		const queued = controller.activate(
			{ questions: [question("queued")] },
			queuedAbort.signal,
		);

		queuedAbort.abort();
		activeAbort.abort();

		expect((await queued).cancelled).toBe(true);
		expect((await active).cancelled).toBe(true);
		expect(controller.active).toBe(false);
	});
});
