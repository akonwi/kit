import type { Page } from "@playwright/test";
import { expect, test } from "./web-tui.fixture";

type WebSocketFrame = { opcode: number; payloadData: string };
type TerminalSizeControl = {
	type: "init" | "resize";
	cols: number;
	rows: number;
};

async function waitForTerminal(page: Page): Promise<void> {
	await expect(page.locator("#status")).toBeHidden({ timeout: 30_000 });
	await expect(page.locator("#terminal canvas")).toHaveCount(1);
	await page.waitForFunction(() => {
		const canvas =
			document.querySelector<HTMLCanvasElement>("#terminal canvas");
		return Boolean(canvas && canvas.width > 100 && canvas.height > 100);
	});
	await expect
		.poll(() =>
			page.evaluate(() =>
				getComputedStyle(document.documentElement)
					.getPropertyValue("--kit-terminal-bg")
					.trim(),
			),
		)
		.toBe("#fdf6e3");
}

function binaryText(frames: WebSocketFrame[], offset = 0): string {
	return Buffer.concat(
		frames
			.slice(offset)
			.filter((frame) => frame.opcode === 2)
			.map((frame) => Buffer.from(frame.payloadData, "base64")),
	).toString("utf8");
}

function binaryBytes(frames: WebSocketFrame[], offset = 0): number {
	return frames
		.slice(offset)
		.filter((frame) => frame.opcode === 2)
		.reduce(
			(total, frame) =>
				total + Buffer.from(frame.payloadData, "base64").byteLength,
			0,
		);
}

function terminalSizeControls(
	frames: WebSocketFrame[],
	offset = 0,
): TerminalSizeControl[] {
	return frames
		.slice(offset)
		.filter((frame) => frame.opcode === 1)
		.flatMap((frame) => {
			try {
				const value = JSON.parse(frame.payloadData) as Record<string, unknown>;
				if (
					(value.type === "init" || value.type === "resize") &&
					typeof value.cols === "number" &&
					typeof value.rows === "number"
				) {
					return [value as TerminalSizeControl];
				}
			} catch {}
			return [];
		});
}

test("boots the compiled ghostty client and renders the custom theme", async ({
	webTuiPage,
}) => {
	const { diagnostics, page, server, url } = webTuiPage;
	const responses = new Map<string, number>();
	page.on("response", (response) => {
		responses.set(new URL(response.url()).pathname, response.status());
	});

	const documentResponse = await page.goto(url);
	expect(documentResponse?.status()).toBe(200);
	await waitForTerminal(page);

	expect(responses.get("/assets/tui-client.js")).toBe(200);
	expect(responses.get("/assets/ghostty-vt.wasm")).toBe(200);
	const csp = documentResponse?.headers()["content-security-policy"] ?? "";
	expect(csp).toContain("default-src 'self'");
	expect(csp).toContain("connect-src 'self'");
	expect(csp).toContain("object-src 'none'");
	expect(csp).toContain("frame-ancestors 'none'");
	expect(csp).toContain("script-src 'self' 'wasm-unsafe-eval'");
	expect(await page.locator("#terminal textarea").count()).toBe(1);
	expect(
		await page.evaluate(
			() => getComputedStyle(document.documentElement).colorScheme,
		),
	).toBe("light");

	// CSS theme control can arrive just before ghostty paints OpenTUI's first
	// themed frame; wait for the Canvas rather than racing its render loop.
	await expect
		.poll(() =>
			page.locator("#terminal canvas").evaluate((canvas) => {
				const element = canvas as HTMLCanvasElement;
				const context = element.getContext("2d");
				if (!context) return 0;
				const data = context.getImageData(
					0,
					0,
					element.width,
					element.height,
				).data;
				let background = 0;
				let samples = 0;
				for (let index = 0; index < data.length; index += 256) {
					if (
						data[index] === 253 &&
						data[index + 1] === 246 &&
						data[index + 2] === 227
					) {
						background += 1;
					}
					samples += 1;
				}
				return background / samples;
			}),
		)
		.toBeGreaterThan(0.5);

	const pixels = await page.locator("#terminal canvas").evaluate((canvas) => {
		const element = canvas as HTMLCanvasElement;
		const context = element.getContext("2d");
		if (!context) throw new Error("Canvas 2D context is unavailable");
		const data = context.getImageData(0, 0, element.width, element.height).data;
		const colors = new Set<string>();
		let background = 0;
		let hardcodedDark = 0;
		let samples = 0;
		for (let y = 0; y < element.height; y += 8) {
			for (let x = 0; x < element.width; x += 8) {
				const index = (y * element.width + x) * 4;
				const red = data[index] ?? 0;
				const green = data[index + 1] ?? 0;
				const blue = data[index + 2] ?? 0;
				colors.add(`${red},${green},${blue}`);
				if (red === 253 && green === 246 && blue === 227) background += 1;
				if (red === 10 && green === 10 && blue === 10) hardcodedDark += 1;
				samples += 1;
			}
		}
		return {
			width: element.width,
			height: element.height,
			uniqueColors: colors.size,
			background,
			hardcodedDark,
			samples,
		};
	});
	expect(pixels.width).toBeGreaterThan(100);
	expect(pixels.height).toBeGreaterThan(100);
	expect(pixels.uniqueColors).toBeGreaterThan(3);
	expect(pixels.background / pixels.samples).toBeGreaterThan(0.5);
	expect(pixels.hardcodedDark).toBe(0);
	expect(diagnostics.consoleErrors).toEqual([]);
	expect(diagnostics.pageErrors).toEqual([]);
	expect(diagnostics.failedRequests).toEqual([]);
	const crossOriginProbe = `http://localhost:${server.port}/api/health`;
	const externalConnectionBlocked = await page.evaluate(async (probe) => {
		try {
			await fetch(probe);
			return false;
		} catch {
			return true;
		}
	}, crossOriginProbe);
	expect(externalConnectionBlocked).toBe(true);
	await expect
		.poll(() =>
			diagnostics.consoleErrors.some(
				(message) =>
					message.includes("Content Security Policy") &&
					message.includes(crossOriginProbe),
			),
		)
		.toBe(true);
});

test("encodes keyboard, mouse, wheel, and resize through the real browser", async ({
	webTuiPage,
}) => {
	const { diagnostics, page, server, url } = webTuiPage;
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("Network.enable");
	const sentFrames: WebSocketFrame[] = [];
	const receivedFrames: WebSocketFrame[] = [];
	cdp.on("Network.webSocketFrameSent", (event: { response: WebSocketFrame }) =>
		sentFrames.push(event.response),
	);
	cdp.on(
		"Network.webSocketFrameReceived",
		(event: { response: WebSocketFrame }) =>
			receivedFrames.push(event.response),
	);
	await page.goto(url);
	await waitForTerminal(page);

	const keyboardOffset = sentFrames.length;
	await page.keyboard.press("Escape");
	await page.keyboard.press("ArrowUp");
	await expect
		.poll(() => binaryText(sentFrames, keyboardOffset))
		.toContain("\x1b");
	await expect
		.poll(() => binaryText(sentFrames, keyboardOffset))
		.toContain("\x1b[A");

	const canvas = page.locator("#terminal canvas");
	const bounds = await canvas.boundingBox();
	if (!bounds) throw new Error("Terminal Canvas has no bounds");
	const mouseOffset = sentFrames.length;
	const x = bounds.x + bounds.width / 2;
	const y = bounds.y + bounds.height / 2;
	await page.mouse.move(x - 10, y - 5);
	await page.mouse.down();
	await page.mouse.move(x + 10, y + 5);
	await page.mouse.up();
	// Dispatch a browser WheelEvent directly on the terminal surface; Chromium's
	// headless native wheel routing targets the page after ghostty focuses its
	// hidden textarea and does not deterministically exercise this adapter.
	await canvas.dispatchEvent("wheel", {
		bubbles: true,
		cancelable: true,
		clientX: x,
		clientY: y,
		deltaY: 100,
	});
	const sgrPrefix = `${String.fromCharCode(27)}\\[<`;
	await expect
		.poll(() => binaryText(sentFrames, mouseOffset))
		.toMatch(new RegExp(`${sgrPrefix}0;\\d+;\\d+M`));
	await expect
		.poll(() => binaryText(sentFrames, mouseOffset))
		.toMatch(new RegExp(`${sgrPrefix}0;\\d+;\\d+m`));
	await expect
		.poll(() => binaryText(sentFrames, mouseOffset))
		.toMatch(new RegExp(`${sgrPrefix}(?:32|35);\\d+;\\d+M`));
	await expect
		.poll(() => binaryText(sentFrames, mouseOffset))
		.toMatch(new RegExp(`${sgrPrefix}65;\\d+;\\d+M`));

	await expect
		.poll(() => terminalSizeControls(sentFrames).length)
		.toBeGreaterThan(0);
	const initialSize = terminalSizeControls(sentFrames).at(-1);
	if (!initialSize) throw new Error("Terminal did not send its initial size");
	const initialCanvasSize = await canvas.evaluate((element) => ({
		height: (element as HTMLCanvasElement).height,
		width: (element as HTMLCanvasElement).width,
	}));
	const resizeOffset = sentFrames.length;
	const resizeOutputOffset = receivedFrames.length;
	await page.setViewportSize({ width: 840, height: 560 });
	await expect
		.poll(() =>
			terminalSizeControls(sentFrames, resizeOffset).some(
				(control) =>
					control.cols < initialSize.cols && control.rows < initialSize.rows,
			),
		)
		.toBe(true);
	await expect
		.poll(() => binaryBytes(receivedFrames, resizeOutputOffset))
		.toBeGreaterThan(100);
	await expect
		.poll(async () => {
			const resized = await canvas.evaluate((element) => ({
				height: (element as HTMLCanvasElement).height,
				width: (element as HTMLCanvasElement).width,
			}));
			return (
				resized.height < initialCanvasSize.height &&
				resized.width < initialCanvasSize.width
			);
		})
		.toBe(true);
	await expect(canvas).toHaveCount(1);
	expect(diagnostics.consoleErrors).toEqual([]);
	expect(diagnostics.pageErrors).toEqual([]);
	expect(diagnostics.failedRequests).toEqual([]);

	// Ctrl+C is an empty-composer quit shortcut in the real app, so assert it
	// last and permit the resulting orderly zero exit for this test only.
	server.allowExitCode(0);
	const interruptOffset = sentFrames.length;
	await page.keyboard.press("Control+C");
	await expect
		.poll(() => binaryText(sentFrames, interruptOffset))
		.toContain("\x03");
});

test("reconnects after network loss and reloads without duplicate browser state", async ({
	webTuiPage,
}) => {
	const { diagnostics, page, url } = webTuiPage;
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("Network.enable");
	const receivedFrames: WebSocketFrame[] = [];
	cdp.on(
		"Network.webSocketFrameReceived",
		(event: { response: WebSocketFrame }) =>
			receivedFrames.push(event.response),
	);
	await page.addInitScript(() => {
		const sockets: WebSocket[] = [];
		const OriginalWebSocket = window.WebSocket;
		const TrackingWebSocket = new Proxy(OriginalWebSocket, {
			construct(target, args, newTarget) {
				const socket = Reflect.construct(target, args, newTarget) as WebSocket;
				sockets.push(socket);
				return socket;
			},
		});
		Object.defineProperty(window, "__kitTestSockets", { value: sockets });
		window.WebSocket = TrackingWebSocket;
	});
	await page.goto(url);
	await waitForTerminal(page);

	const reconnectOutputOffset = receivedFrames.length;
	await page.evaluate(() => {
		const sockets = (
			window as typeof window & { __kitTestSockets: WebSocket[] }
		).__kitTestSockets;
		const socket = sockets.at(-1);
		if (!socket) throw new Error("No browser-TUI WebSocket to disconnect");
		socket.close(4000, "browser test disconnect");
	});
	await expect(page.locator("#status")).toBeVisible();
	await expect(page.locator("#status")).toContainText("disconnected");
	await expect
		.poll(() =>
			page.evaluate(
				() =>
					(window as typeof window & { __kitTestSockets: WebSocket[] })
						.__kitTestSockets.length,
			),
		)
		.toBeGreaterThan(1);
	await expect
		.poll(() => binaryBytes(receivedFrames, reconnectOutputOffset))
		.toBeGreaterThan(2_000);
	await waitForTerminal(page);
	await expect(page.locator("#terminal canvas")).toHaveCount(1);
	await expect(page.locator("#terminal textarea")).toHaveCount(1);

	const reloadOutputOffset = receivedFrames.length;
	await page.reload();
	await expect
		.poll(() => binaryBytes(receivedFrames, reloadOutputOffset))
		.toBeGreaterThan(2_000);
	await waitForTerminal(page);
	await expect(page.locator("#terminal canvas")).toHaveCount(1);
	await expect(page.locator("#terminal textarea")).toHaveCount(1);
	expect(diagnostics.consoleErrors).toEqual([]);
	expect(diagnostics.pageErrors).toEqual([]);
	expect(diagnostics.failedRequests).toEqual([]);
});
