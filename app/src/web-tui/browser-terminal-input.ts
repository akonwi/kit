export type BrowserKeyLike = {
	key: string;
	ctrlKey: boolean;
	shiftKey: boolean;
	altKey: boolean;
	metaKey: boolean;
	isComposing?: boolean;
};

const FIXED_KEYS: Record<string, string> = {
	Enter: "\r",
	Backspace: "\x7f",
	Tab: "\t",
	Escape: "\x1b",
	Insert: "\x1b[2~",
	Delete: "\x1b[3~",
	PageUp: "\x1b[5~",
	PageDown: "\x1b[6~",
	F1: "\x1bOP",
	F2: "\x1bOQ",
	F3: "\x1bOR",
	F4: "\x1bOS",
	F5: "\x1b[15~",
	F6: "\x1b[17~",
	F7: "\x1b[18~",
	F8: "\x1b[19~",
	F9: "\x1b[20~",
	F10: "\x1b[21~",
	F11: "\x1b[23~",
	F12: "\x1b[24~",
};

const NAVIGATION_KEYS: Record<string, string> = {
	ArrowUp: "A",
	ArrowDown: "B",
	ArrowRight: "C",
	ArrowLeft: "D",
	Home: "H",
	End: "F",
};

function modifierParameter(key: BrowserKeyLike): number {
	return (
		1 +
		(key.shiftKey ? 1 : 0) +
		(key.altKey ? 2 : 0) +
		(key.ctrlKey ? 4 : 0) +
		(key.metaKey ? 8 : 0)
	);
}

export type BrowserPlatform = "mac" | "other";

export function classifyBrowserPlatform(value: string): BrowserPlatform {
	return /mac|iphone|ipad|ipod/i.test(value) ? "mac" : "other";
}

function browserPlatform(): BrowserPlatform {
	if (typeof navigator === "undefined") return "other";
	const navigatorWithData = navigator as Navigator & {
		userAgentData?: { platform?: string };
	};
	return classifyBrowserPlatform(
		navigatorWithData.userAgentData?.platform ?? navigator.platform,
	);
}

export function isBrowserCopyKey(event: BrowserKeyLike): boolean {
	return (
		(event.metaKey && event.key.toLowerCase() === "c") ||
		(event.ctrlKey && event.shiftKey && event.key.toLowerCase() === "c") ||
		(event.key === "Insert" && event.ctrlKey)
	);
}

export function isBrowserOwnedKey(event: BrowserKeyLike): boolean {
	return (
		isBrowserCopyKey(event) ||
		((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "v") ||
		(event.key === "Insert" && event.shiftKey)
	);
}

function isMacOptionText(
	event: BrowserKeyLike,
	platform: BrowserPlatform,
): boolean {
	return (
		platform === "mac" &&
		event.altKey &&
		!event.ctrlKey &&
		!event.metaKey &&
		(event.key.length === 1 || event.key === "Dead")
	);
}

/** Encode browser keys whose native handling is unreliable or browser-owned. */
export function encodeBrowserKey(
	event: BrowserKeyLike,
	platform: BrowserPlatform = "other",
): string | null {
	if (event.isComposing) return null;

	// Keep browser clipboard conventions available. Ctrl+C remains the TUI
	// interrupt; Cmd+C and Ctrl+Shift+C copy browser selection.
	if (isBrowserOwnedKey(event)) return null;

	if (event.ctrlKey && !event.altKey && !event.metaKey) {
		if (event.key.length === 1) {
			const character = event.key.toLowerCase();
			const code = character.charCodeAt(0);
			if (code >= 97 && code <= 122) return String.fromCharCode(code - 96);
			const controls: Record<string, string> = {
				"[": "\x1b",
				"\\": "\x1c",
				"]": "\x1d",
				"^": "\x1e",
				_: "\x1f",
				"?": "\x7f",
				" ": "\x00",
			};
			if (character in controls) return controls[character] ?? null;
		}
	}

	if (event.key === "Tab" && event.shiftKey) return "\x1b[Z";

	// Linux terminals conventionally encode Alt+printable as an Escape prefix.
	// macOS Option is left native because it commonly drives dead keys and IME.
	if (
		platform !== "mac" &&
		event.altKey &&
		!event.ctrlKey &&
		!event.metaKey &&
		event.key.length === 1
	) {
		return `\x1b${event.key}`;
	}

	const navigation = NAVIGATION_KEYS[event.key];
	if (navigation) {
		const modifiers = modifierParameter(event);
		return modifiers === 1
			? `\x1b[${navigation}`
			: `\x1b[1;${modifiers}${navigation}`;
	}

	const fixed = FIXED_KEYS[event.key];
	if (!fixed) return null;
	if (event.altKey && !event.ctrlKey && !event.metaKey) return `\x1b${fixed}`;
	return fixed;
}

export type MouseTrackingMode = 0 | 1000 | 1002 | 1003;

const DEC_PRIVATE_MODE_PATTERN = new RegExp(
	`${String.fromCharCode(27)}\\[\\?([\\d;]+)([hl])`,
	"g",
);

/** Tracks the DEC modes needed to encode browser pointer events. */
export class TerminalProtocolState {
	private readonly mouseModes = new Set<MouseTrackingMode>();
	private tail = "";
	private readonly decoder = new TextDecoder();
	mouseSgr = false;
	bracketedPaste = false;

	get mouseTracking(): MouseTrackingMode {
		if (this.mouseModes.has(1003)) return 1003;
		if (this.mouseModes.has(1002)) return 1002;
		if (this.mouseModes.has(1000)) return 1000;
		return 0;
	}

	feed(bytes: Uint8Array): void {
		const text = this.tail + this.decoder.decode(bytes, { stream: true });
		for (const match of text.matchAll(DEC_PRIVATE_MODE_PATTERN)) {
			const enabled = match[2] === "h";
			for (const rawMode of match[1]?.split(";") ?? []) {
				const mode = Number(rawMode);
				if (mode === 1000 || mode === 1002 || mode === 1003) {
					if (enabled) this.mouseModes.add(mode);
					else this.mouseModes.delete(mode);
				} else if (mode === 1006) {
					this.mouseSgr = enabled;
				} else if (mode === 2004) {
					this.bracketedPaste = enabled;
				}
			}
		}
		this.tail = text.slice(-96);
	}
}

export type TerminalGeometry = {
	columns: number;
	rows: number;
	bounds: DOMRect;
};

export type BrowserTerminalInputOptions = {
	root: HTMLElement;
	protocol: TerminalProtocolState;
	platform?: BrowserPlatform;
	geometry: () => TerminalGeometry | null;
	send: (data: string) => void;
	focus: () => void;
	copySelection?: () => string;
	writeClipboard?: (text: string) => void | Promise<void>;
};

function mouseModifiers(event: MouseEvent): number {
	return (
		(event.shiftKey ? 4 : 0) | (event.altKey ? 8 : 0) | (event.ctrlKey ? 16 : 0)
	);
}

function mouseButton(event: MouseEvent): number | null {
	if (event.button === 0) return 0;
	if (event.button === 1) return 1;
	if (event.button === 2) return 2;
	return null;
}

/** Renderer-independent browser keyboard and SGR mouse adapter. */
export class BrowserTerminalInput {
	private readonly root: HTMLElement;
	private readonly protocol: TerminalProtocolState;
	private readonly platform: BrowserPlatform;
	private readonly geometry: () => TerminalGeometry | null;
	private readonly send: (data: string) => void;
	private readonly focus: () => void;
	private readonly copySelection: () => string;
	private readonly writeClipboard: (text: string) => void | Promise<void>;
	private disposed = false;

	constructor(options: BrowserTerminalInputOptions) {
		this.root = options.root;
		this.protocol = options.protocol;
		this.platform = options.platform ?? browserPlatform();
		this.geometry = options.geometry;
		this.send = options.send;
		this.focus = options.focus;
		this.copySelection = options.copySelection ?? (() => "");
		this.writeClipboard = options.writeClipboard ?? (() => {});
		window.addEventListener("keydown", this.onKeyDown, true);
		window.addEventListener("mousedown", this.onMouseDown, true);
		window.addEventListener("mouseup", this.onMouseUp, true);
		window.addEventListener("mousemove", this.onMouseMove, true);
		window.addEventListener("contextmenu", this.onContextMenu, true);
		window.addEventListener("paste", this.onPaste, true);
		window.addEventListener("compositionend", this.onCompositionEnd, true);
		window.addEventListener("wheel", this.onWheel, {
			capture: true,
			passive: false,
		});
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		window.removeEventListener("keydown", this.onKeyDown, true);
		window.removeEventListener("mousedown", this.onMouseDown, true);
		window.removeEventListener("mouseup", this.onMouseUp, true);
		window.removeEventListener("mousemove", this.onMouseMove, true);
		window.removeEventListener("contextmenu", this.onContextMenu, true);
		window.removeEventListener("paste", this.onPaste, true);
		window.removeEventListener("compositionend", this.onCompositionEnd, true);
		window.removeEventListener("wheel", this.onWheel, true);
	}

	private readonly onKeyDown = (event: KeyboardEvent) => {
		if (isBrowserOwnedKey(event)) {
			// Preserve paste defaults while preventing ghostty's hidden textarea
			// from translating the same shortcut into terminal input. Canvas
			// selections require an explicit clipboard write.
			event.stopImmediatePropagation();
			if (isBrowserCopyKey(event)) {
				const selection = this.copySelection();
				if (selection) {
					event.preventDefault();
					void Promise.resolve(this.writeClipboard(selection)).catch(() => {});
				}
			}
			return;
		}
		if (event.isComposing) {
			event.stopImmediatePropagation();
			return;
		}
		if (isMacOptionText(event, this.platform)) {
			// A completed Option character is already represented by event.key.
			// Dead keys continue through the browser composition pipeline.
			event.stopImmediatePropagation();
			if (event.key !== "Dead") {
				event.preventDefault();
				this.send(event.key);
			}
			return;
		}
		const sequence = encodeBrowserKey(event, this.platform);
		if (sequence === null) return;
		event.preventDefault();
		event.stopImmediatePropagation();
		this.send(sequence);
	};

	private readonly onMouseDown = (event: MouseEvent) => {
		if (!this.shouldForwardMouse(event)) return;
		const button = mouseButton(event);
		if (button === null) return;
		this.focus();
		this.forwardMouse(event, button | mouseModifiers(event), "M");
	};

	private readonly onMouseUp = (event: MouseEvent) => {
		if (!this.shouldForwardMouse(event)) return;
		const button = mouseButton(event);
		if (button === null) return;
		this.forwardMouse(event, button | mouseModifiers(event), "m");
	};

	private readonly onMouseMove = (event: MouseEvent) => {
		if (!this.shouldForwardMouse(event)) return;
		const tracking = this.protocol.mouseTracking;
		if (tracking !== 1003 && (tracking !== 1002 || event.buttons === 0)) return;
		let button = 3;
		if (event.buttons & 1) button = 0;
		else if (event.buttons & 4) button = 1;
		else if (event.buttons & 2) button = 2;
		this.forwardMouse(event, 32 | button | mouseModifiers(event), "M");
	};

	private readonly onContextMenu = (event: MouseEvent) => {
		if (!this.shouldForwardMouse(event)) return;
		event.preventDefault();
		event.stopImmediatePropagation();
	};

	private readonly onCompositionEnd = (event: CompositionEvent) => {
		if (!this.root.contains(event.target as Node) || !event.data) return;
		event.stopImmediatePropagation();
		this.send(event.data);
	};

	private readonly onPaste = (event: ClipboardEvent) => {
		if (!this.root.contains(event.target as Node)) return;
		const text = event.clipboardData?.getData("text/plain");
		if (!text) return;
		event.preventDefault();
		event.stopImmediatePropagation();
		this.send(
			this.protocol.bracketedPaste ? `\x1b[200~${text}\x1b[201~` : text,
		);
	};

	private readonly onWheel = (event: WheelEvent) => {
		if (!this.shouldForwardMouse(event) || event.deltaY === 0) return;
		const code = (event.deltaY < 0 ? 64 : 65) | mouseModifiers(event);
		this.forwardMouse(event, code, "M");
	};

	private shouldForwardMouse(event: MouseEvent): boolean {
		return (
			this.protocol.mouseTracking !== 0 &&
			this.protocol.mouseSgr &&
			!event.shiftKey &&
			this.root.contains(event.target as Node)
		);
	}

	private forwardMouse(
		event: MouseEvent,
		code: number,
		suffix: "M" | "m",
	): void {
		const geometry = this.geometry();
		if (!geometry || geometry.bounds.width <= 0 || geometry.bounds.height <= 0)
			return;
		const column = Math.max(
			1,
			Math.min(
				geometry.columns,
				Math.floor(
					((event.clientX - geometry.bounds.left) / geometry.bounds.width) *
						geometry.columns,
				) + 1,
			),
		);
		const row = Math.max(
			1,
			Math.min(
				geometry.rows,
				Math.floor(
					((event.clientY - geometry.bounds.top) / geometry.bounds.height) *
						geometry.rows,
				) + 1,
			),
		);
		event.preventDefault();
		event.stopImmediatePropagation();
		this.send(`\x1b[<${code};${column};${row}${suffix}`);
	}
}
