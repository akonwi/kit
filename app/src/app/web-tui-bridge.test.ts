import { describe, expect, test } from "bun:test";
import {
	clampTuiSize,
	type TuiOutputSink,
	type TuiRendererControl,
	WebTuiBridge,
} from "./web-tui-bridge";

function collectingSink(): TuiOutputSink & { chunks: Uint8Array[] } {
	const chunks: Uint8Array[] = [];
	return {
		chunks,
		send: (bytes) => {
			chunks.push(bytes);
		},
	};
}

function rendererStub(): TuiRendererControl & { calls: string[] } {
	const calls: string[] = [];
	return {
		calls,
		resize: (width, height) => calls.push(`resize:${width}x${height}`),
		suspend: () => calls.push("suspend"),
		resume: () => calls.push("resume"),
		destroy: () => calls.push("destroy"),
	};
}

describe("clampTuiSize", () => {
	test("clamps and floors dimensions", () => {
		expect(clampTuiSize(0, 0)).toEqual({ cols: 20, rows: 5 });
		expect(clampTuiSize(10_000, 10_000)).toEqual({ cols: 500, rows: 300 });
		expect(clampTuiSize(120.9, 40.2)).toEqual({ cols: 120, rows: 40 });
	});
});

describe("WebTuiBridge", () => {
	test("forwards stdout writes to the attached sink only", async () => {
		const bridge = new WebTuiBridge();
		const sink = collectingSink();
		bridge.terminal.stdout.write("dropped before attach");
		bridge.attach(sink, 100, 30);
		await new Promise<void>((resolve) => {
			bridge.terminal.stdout.write("hello", () => resolve());
		});
		const text = Buffer.concat(sink.chunks).toString("utf8");
		expect(text).toBe("hello");
	});

	test("updates stdout geometry from attach and resize", () => {
		const bridge = new WebTuiBridge();
		const terminal = bridge.terminal;
		bridge.attach(collectingSink(), 132, 45);
		expect(terminal.stdout.columns).toBe(132);
		expect(terminal.stdout.rows).toBe(45);
		bridge.resize(90, 28);
		expect(terminal.stdout.columns).toBe(90);
		expect(terminal.stdout.rows).toBe(28);
	});

	test("delivers input bytes through the virtual stdin", async () => {
		const bridge = new WebTuiBridge();
		const received: Buffer[] = [];
		bridge.terminal.stdin.on("data", (chunk: Buffer) => received.push(chunk));
		expect(bridge.input(new TextEncoder().encode("abc"))).toBe(true);
		await new Promise((resolve) => setImmediate(resolve));
		expect(Buffer.concat(received).toString("utf8")).toBe("abc");
	});

	test("bounds input queued before the renderer is ready", () => {
		const bridge = new WebTuiBridge();
		expect(bridge.input(new Uint8Array(64 * 1024))).toBe(true);
		expect(bridge.input(new Uint8Array([1]))).toBe(false);
	});

	test("suspends on detach and resumes with resize on reattach", () => {
		const bridge = new WebTuiBridge();
		const renderer = rendererStub();
		const sink = collectingSink();
		bridge.attach(sink, 100, 30);
		bridge.terminal.onRendererReady?.(renderer);
		expect(renderer.calls).toEqual([]);

		bridge.detach(sink);
		expect(renderer.calls).toEqual(["suspend"]);

		const nextSink = collectingSink();
		bridge.attach(nextSink, 110, 32);
		expect(renderer.calls).toEqual(["suspend", "resume", "resize:110x32"]);
	});

	test("ignores detach from a stale sink", () => {
		const bridge = new WebTuiBridge();
		const renderer = rendererStub();
		const active = collectingSink();
		bridge.attach(active, 100, 30);
		bridge.terminal.onRendererReady?.(renderer);
		bridge.detach(collectingSink());
		expect(renderer.calls).toEqual([]);
	});

	test("parks the renderer when it becomes ready without a client", () => {
		const bridge = new WebTuiBridge();
		const renderer = rendererStub();
		bridge.terminal.onRendererReady?.(renderer);
		expect(renderer.calls).toEqual(["suspend"]);
		bridge.attach(collectingSink(), 100, 30);
		expect(renderer.calls).toEqual(["suspend", "resume", "resize:100x30"]);
	});

	test("live resize reaches the renderer, suspended resize does not", () => {
		const bridge = new WebTuiBridge();
		const renderer = rendererStub();
		const sink = collectingSink();
		bridge.attach(sink, 100, 30);
		bridge.terminal.onRendererReady?.(renderer);
		bridge.resize(80, 24);
		expect(renderer.calls).toEqual(["resize:80x24"]);
		bridge.detach(sink);
		bridge.resize(60, 20);
		expect(renderer.calls).toEqual(["resize:80x24", "suspend"]);
	});

	test("destroys a renderer that becomes ready after shutdown starts", () => {
		const bridge = new WebTuiBridge();
		expect(bridge.shutdown()).toBe(false);
		const renderer = rendererStub();
		bridge.terminal.onRendererReady?.(renderer);
		expect(renderer.calls).toEqual(["destroy"]);
	});

	test("shutdown resumes a suspended renderer before destroying it", () => {
		const bridge = new WebTuiBridge();
		const renderer = rendererStub();
		bridge.terminal.onRendererReady?.(renderer);
		expect(bridge.shutdown()).toBe(true);
		expect(bridge.shutdown()).toBe(false);
		expect(renderer.calls).toEqual(["suspend", "resume", "destroy"]);
	});
});
