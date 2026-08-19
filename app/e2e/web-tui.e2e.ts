import type { Browser, Page } from "@playwright/test";
import { expect, test } from "./web-tui.fixture";

type TrackedFrame =
	| { kind: "binary"; bytes: number[] }
	| { kind: "text"; text: string };

type SocketTrackingSnapshot = {
	receivedBytes: number;
	sent: TrackedFrame[];
	socketCount: number;
};

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

async function installSocketTracking(page: Page): Promise<void> {
	await page.addInitScript(() => {
		type BrowserTrackedFrame =
			| { kind: "binary"; bytes: number[] }
			| { kind: "text"; text: string };
		type BrowserSocketTracking = {
			receivedBytes: number;
			sent: BrowserTrackedFrame[];
			sockets: WebSocket[];
		};
		const tracking: BrowserSocketTracking = {
			receivedBytes: 0,
			sent: [],
			sockets: [],
		};
		const OriginalWebSocket = window.WebSocket;
		window.WebSocket = new Proxy(OriginalWebSocket, {
			construct(target, args, newTarget) {
				const socket = Reflect.construct(target, args, newTarget) as WebSocket;
				tracking.sockets.push(socket);
				const originalSend = socket.send.bind(socket);
				socket.send = ((
					data: string | ArrayBufferLike | Blob | ArrayBufferView,
				) => {
					if (typeof data === "string") {
						tracking.sent.push({ kind: "text", text: data });
					} else if (data instanceof ArrayBuffer) {
						tracking.sent.push({
							kind: "binary",
							bytes: Array.from(new Uint8Array(data)),
						});
					} else if (ArrayBuffer.isView(data)) {
						tracking.sent.push({
							kind: "binary",
							bytes: Array.from(
								new Uint8Array(data.buffer, data.byteOffset, data.byteLength),
							),
						});
					}
					originalSend(data);
				}) as typeof socket.send;
				socket.addEventListener("message", (event) => {
					if (event.data instanceof ArrayBuffer) {
						tracking.receivedBytes += event.data.byteLength;
					} else if (event.data instanceof Blob) {
						tracking.receivedBytes += event.data.size;
					}
				});
				return socket;
			},
		});
		Object.defineProperty(window, "__kitSocketTracking", {
			value: tracking,
		});
	});
}

async function socketTracking(page: Page): Promise<SocketTrackingSnapshot> {
	return page.evaluate(() => {
		const tracking = (
			window as typeof window & {
				__kitSocketTracking: {
					receivedBytes: number;
					sent: TrackedFrame[];
					sockets: WebSocket[];
				};
			}
		).__kitSocketTracking;
		return {
			receivedBytes: tracking.receivedBytes,
			sent: tracking.sent,
			socketCount: tracking.sockets.length,
		};
	});
}

function binaryText(frames: TrackedFrame[], offset = 0): string {
	return Buffer.concat(
		frames
			.slice(offset)
			.filter(
				(frame): frame is Extract<TrackedFrame, { kind: "binary" }> =>
					frame.kind === "binary",
			)
			.map((frame) => Buffer.from(frame.bytes)),
	).toString("utf8");
}

function terminalSizeControls(
	frames: TrackedFrame[],
	offset = 0,
): TerminalSizeControl[] {
	return frames
		.slice(offset)
		.filter(
			(frame): frame is Extract<TrackedFrame, { kind: "text" }> =>
				frame.kind === "text",
		)
		.flatMap((frame) => {
			try {
				const value = JSON.parse(frame.text) as Record<string, unknown>;
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

async function clickCellAtDeviceScale(
	browser: Browser,
	url: string,
	deviceScaleFactor: number,
): Promise<{ column: number; row: number; backingScale: number }> {
	const context = await browser.newContext({
		deviceScaleFactor,
		viewport: { width: 1_000, height: 700 },
	});
	const page = await context.newPage();
	try {
		await installSocketTracking(page);
		await page.goto(url);
		await waitForTerminal(page);
		const canvas = page.locator("#terminal canvas");
		const bounds = await canvas.boundingBox();
		if (!bounds) throw new Error("Terminal Canvas has no bounds");
		const offset = (await socketTracking(page)).sent.length;
		await page.mouse.click(
			bounds.x + bounds.width * 0.37,
			bounds.y + bounds.height * 0.41,
		);
		const sgrPrefix = `${String.fromCharCode(27)}\\[<`;
		let match: RegExpMatchArray | null = null;
		await expect
			.poll(async () => {
				match = binaryText((await socketTracking(page)).sent, offset).match(
					new RegExp(`${sgrPrefix}0;(\\d+);(\\d+)M`),
				);
				return match !== null;
			})
			.toBe(true);
		if (!match) throw new Error("Terminal click did not produce an SGR cell");
		const backingWidth = await canvas.evaluate(
			(element) => (element as HTMLCanvasElement).width,
		);
		return {
			column: Number(match[1]),
			row: Number(match[2]),
			backingScale: backingWidth / bounds.width,
		};
	} finally {
		await context.close();
	}
}

test("boots the compiled ghostty client and renders the custom theme", async ({
	browserName,
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
	if (browserName === "chromium") {
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
	}
});

test("encodes keyboard, mouse, wheel, and resize through the real browser", async ({
	browserName,
	webTuiPage,
}) => {
	const { diagnostics, page, server, url } = webTuiPage;
	await installSocketTracking(page);
	await page.addInitScript(() => {
		window.addEventListener(
			"keydown",
			(event) => {
				if (event.altKey) {
					Object.defineProperty(window, "__kitLastAltKey", {
						configurable: true,
						value: event.key,
					});
				}
			},
			true,
		);
	});
	await page.goto(url);
	await waitForTerminal(page);

	const browserUsesMacKeys = await page.evaluate(() => {
		const navigatorWithData = navigator as Navigator & {
			userAgentData?: { platform?: string };
		};
		return /mac|iphone|ipad|ipod/i.test(
			navigatorWithData.userAgentData?.platform ?? navigator.platform,
		);
	});
	const altOffset = (await socketTracking(page)).sent.length;
	await page.keyboard.press("Alt+a");
	await page.waitForTimeout(100);
	const altText = binaryText((await socketTracking(page)).sent, altOffset);
	if (browserUsesMacKeys) {
		const producedKey = await page.evaluate(
			() =>
				(window as typeof window & { __kitLastAltKey?: string })
					.__kitLastAltKey,
		);
		expect(producedKey).toBeTruthy();
		expect(altText).toBe(producedKey === "Dead" ? "" : producedKey);
	} else {
		expect(altText).toContain("\x1ba");
	}

	// Prevent Escape from taking the empty-composer quit path while its bytes
	// are inspected across engines.
	await page.keyboard.type("keep alive");
	await page.waitForTimeout(100);
	const keyboardOffset = (await socketTracking(page)).sent.length;
	await page.keyboard.press("Escape");
	await page.keyboard.press("ArrowUp");
	await expect
		.poll(async () =>
			binaryText((await socketTracking(page)).sent, keyboardOffset),
		)
		.toContain("\x1b");
	await expect
		.poll(async () =>
			binaryText((await socketTracking(page)).sent, keyboardOffset),
		)
		.toContain("\x1b[A");

	const canvas = page.locator("#terminal canvas");
	const bounds = await canvas.boundingBox();
	if (!bounds) throw new Error("Terminal Canvas has no bounds");
	const mouseOffset = (await socketTracking(page)).sent.length;
	const x = bounds.x + bounds.width / 2;
	const y = bounds.y + bounds.height / 2;
	await page.mouse.move(x - 10, y - 5);
	await page.mouse.down();
	await page.mouse.move(x + 10, y + 5);
	await page.mouse.up();
	await canvas.dispatchEvent("wheel", {
		bubbles: true,
		cancelable: true,
		clientX: x,
		clientY: y,
		deltaY: 100,
	});
	const sgrPrefix = `${String.fromCharCode(27)}\\[<`;
	for (const pattern of [
		new RegExp(`${sgrPrefix}0;\\d+;\\d+M`),
		new RegExp(`${sgrPrefix}0;\\d+;\\d+m`),
		new RegExp(`${sgrPrefix}(?:32|35);\\d+;\\d+M`),
		new RegExp(`${sgrPrefix}65;\\d+;\\d+M`),
	]) {
		await expect
			.poll(async () =>
				binaryText((await socketTracking(page)).sent, mouseOffset),
			)
			.toMatch(pattern);
	}

	const selectionOffset = (await socketTracking(page)).sent.length;
	await page.keyboard.down("Shift");
	await page.mouse.move(bounds.x + 10, bounds.y + 10);
	await page.mouse.down();
	await page.mouse.move(
		bounds.x + bounds.width * 0.75,
		bounds.y + bounds.height * 0.5,
	);
	await page.mouse.up();
	await page.keyboard.up("Shift");
	await page.waitForTimeout(100);
	expect((await socketTracking(page)).sent).toHaveLength(selectionOffset);
	if (browserName === "chromium") {
		await page
			.context()
			.grantPermissions(["clipboard-read", "clipboard-write"], { origin: url });
		await page.keyboard.press(
			process.platform === "darwin" ? "Meta+C" : "Control+Shift+C",
		);
		await expect
			.poll(() => page.evaluate(() => navigator.clipboard.readText()))
			.not.toBe("");
		expect((await socketTracking(page)).sent).toHaveLength(selectionOffset);

		await page.evaluate(() => {
			Object.defineProperty(navigator.clipboard, "writeText", {
				configurable: true,
				value: () => Promise.reject(new Error("forced clipboard fallback")),
			});
		});
		const fallbackOffset = (await socketTracking(page)).sent.length;
		await page.keyboard.press(
			process.platform === "darwin" ? "Meta+C" : "Control+Shift+C",
		);
		await page.keyboard.type("z");
		await expect
			.poll(async () =>
				binaryText((await socketTracking(page)).sent, fallbackOffset),
			)
			.toContain("z");
	}

	await expect
		.poll(
			async () =>
				terminalSizeControls((await socketTracking(page)).sent).length,
		)
		.toBeGreaterThan(0);
	const initialTracking = await socketTracking(page);
	const initialSize = terminalSizeControls(initialTracking.sent).at(-1);
	if (!initialSize) throw new Error("Terminal did not send its initial size");
	const initialCanvasSize = await canvas.evaluate((element) => ({
		height: (element as HTMLCanvasElement).height,
		width: (element as HTMLCanvasElement).width,
	}));
	const resizeOffset = initialTracking.sent.length;
	const resizeOutputOffset = initialTracking.receivedBytes;
	await page.setViewportSize({ width: 840, height: 560 });
	await expect
		.poll(async () =>
			terminalSizeControls(
				(await socketTracking(page)).sent,
				resizeOffset,
			).some(
				(control) =>
					control.cols < initialSize.cols && control.rows < initialSize.rows,
			),
		)
		.toBe(true);
	await expect
		.poll(
			async () =>
				(await socketTracking(page)).receivedBytes - resizeOutputOffset,
		)
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

	server.allowExitCode(0);
	const interruptOffset = (await socketTracking(page)).sent.length;
	await page.keyboard.press("Control+C");
	await expect
		.poll(async () =>
			binaryText((await socketTracking(page)).sent, interruptOffset),
		)
		.toContain("\x03");
});

test("maps the same terminal cell at standard and high DPI", async ({
	browser,
	webTuiServer,
}) => {
	const standard = await clickCellAtDeviceScale(browser, webTuiServer.url, 1);
	const highDpi = await clickCellAtDeviceScale(browser, webTuiServer.url, 2);
	expect({ column: highDpi.column, row: highDpi.row }).toEqual({
		column: standard.column,
		row: standard.row,
	});
	expect(standard.backingScale).toBeCloseTo(1, 1);
	expect(highDpi.backingScale).toBeCloseTo(2, 1);
});

test("preserves large Unicode input and browser-owned clipboard shortcuts", async ({
	webTuiPage,
}) => {
	const { diagnostics, page, url } = webTuiPage;
	await installSocketTracking(page);
	await page.goto(url);
	await waitForTerminal(page);

	const value = "λ🙂".repeat(6_000);
	const expectedPaste = `\x1b[200~${value}\x1b[201~`;
	const inputOffset = (await socketTracking(page)).sent.length;
	await page.locator("#terminal textarea").evaluate((element, text) => {
		const event = new Event("paste", { bubbles: true, cancelable: true });
		Object.defineProperty(event, "clipboardData", {
			value: { getData: (type: string) => (type === "text/plain" ? text : "") },
		});
		element.dispatchEvent(event);
	}, value);
	await expect
		.poll(
			async () =>
				binaryText((await socketTracking(page)).sent, inputOffset).length,
		)
		.toBe(expectedPaste.length);
	const tracking = await socketTracking(page);
	const inputFrames = tracking.sent
		.slice(inputOffset)
		.filter(
			(frame): frame is Extract<TrackedFrame, { kind: "binary" }> =>
				frame.kind === "binary",
		);
	expect(inputFrames.length).toBeGreaterThan(1);
	expect(
		Math.max(...inputFrames.map((frame) => frame.bytes.length)),
	).toBeLessThanOrEqual(16 * 1024);
	expect(binaryText(tracking.sent, inputOffset)).toBe(expectedPaste);

	const compositionOffset = tracking.sent.length;
	await page.locator("#terminal textarea").evaluate((element) => {
		element.dispatchEvent(
			new CompositionEvent("compositionend", {
				bubbles: true,
				data: "漢",
			}),
		);
	});
	await expect
		.poll(async () =>
			binaryText((await socketTracking(page)).sent, compositionOffset),
		)
		.toBe("漢");

	const clipboardOffset = (await socketTracking(page)).sent.length;
	await page.keyboard.press("Meta+C");
	await page.keyboard.press("Control+Shift+C");
	await page.evaluate(() => {
		window.dispatchEvent(
			new KeyboardEvent("keydown", {
				bubbles: true,
				isComposing: true,
				key: "Process",
			}),
		);
	});
	await page.waitForTimeout(100);
	expect((await socketTracking(page)).sent).toHaveLength(clipboardOffset);
	expect(diagnostics.consoleErrors).toEqual([]);
	expect(diagnostics.pageErrors).toEqual([]);
	expect(diagnostics.failedRequests).toEqual([]);
});

test("reconnects after network loss and reloads without duplicate browser state", async ({
	webTuiPage,
}) => {
	const { diagnostics, page, url } = webTuiPage;
	await installSocketTracking(page);
	await page.goto(url);
	await waitForTerminal(page);

	const reconnectOutputOffset = (await socketTracking(page)).receivedBytes;
	await page.evaluate(() => {
		const tracking = (
			window as typeof window & {
				__kitSocketTracking: { sockets: WebSocket[] };
			}
		).__kitSocketTracking;
		const socket = tracking.sockets.at(-1);
		if (!socket) throw new Error("No browser-TUI WebSocket to disconnect");
		socket.close(4000, "browser test disconnect");
	});
	await expect(page.locator("#status")).toBeVisible();
	await expect(page.locator("#status")).toContainText("disconnected");
	await expect
		.poll(async () => (await socketTracking(page)).socketCount)
		.toBeGreaterThan(1);
	await expect
		.poll(
			async () =>
				(await socketTracking(page)).receivedBytes - reconnectOutputOffset,
		)
		.toBeGreaterThan(2_000);
	await waitForTerminal(page);
	await expect(page.locator("#terminal canvas")).toHaveCount(1);
	await expect(page.locator("#terminal textarea")).toHaveCount(1);

	await page.reload();
	await expect
		.poll(async () => (await socketTracking(page)).receivedBytes)
		.toBeGreaterThan(2_000);
	await waitForTerminal(page);
	await expect(page.locator("#terminal canvas")).toHaveCount(1);
	await expect(page.locator("#terminal textarea")).toHaveCount(1);
	expect(diagnostics.consoleErrors).toEqual([]);
	expect(diagnostics.pageErrors).toEqual([]);
	expect(diagnostics.failedRequests).toEqual([]);
});
