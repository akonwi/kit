/**
 * Experimental browser-TUI bridge (ghostty-web experiment).
 *
 * Hosts Kit's real OpenTUI application against virtual terminal streams so a
 * browser terminal emulator (ghostty-web + Ghostty WASM) can present it. OpenTUI
 * supports custom stdin/stdout streams: with a non-process stdout it pipes
 * rendered bytes through a NativeSpanFeed and enables remote mode, which is
 * exactly the SSH-like transport this bridge provides.
 *
 * The bridge owns:
 * - a virtual raw-mode stdin the server feeds with browser input bytes
 * - a virtual stdout whose writes are forwarded to the attached client
 * - attach/detach lifecycle mapped onto OpenTUI suspend/resume so a
 *   reconnecting browser terminal receives fresh terminal setup sequences and
 *   a forced full repaint
 */

import { PassThrough, Writable } from "node:stream";

export type TuiOutputSink = {
	send(bytes: Uint8Array): void;
};

/** Subset of OpenTUI's CliRenderer the bridge drives. */
export type TuiRendererControl = {
	resize(width: number, height: number): void;
	suspend(): void;
	resume(): void;
	destroy(): void;
};

export type BridgeTerminal = {
	stdin: NodeJS.ReadStream;
	stdout: NodeJS.WriteStream;
	width: number;
	height: number;
	onRendererReady: (renderer: TuiRendererControl) => void;
};

class VirtualStdin extends PassThrough {
	readonly isTTY = true;
	setRawMode(_mode: boolean): this {
		return this;
	}
	ref(): this {
		return this;
	}
	unref(): this {
		return this;
	}
}

class VirtualStdout extends Writable {
	readonly isTTY = true;
	columns: number;
	rows: number;

	constructor(
		columns: number,
		rows: number,
		private readonly deliver: (bytes: Uint8Array) => void,
	) {
		super();
		this.columns = columns;
		this.rows = rows;
	}

	override _write(
		chunk: unknown,
		_encoding: BufferEncoding,
		callback: (error?: Error | null) => void,
	): void {
		const bytes =
			typeof chunk === "string"
				? new TextEncoder().encode(chunk)
				: new Uint8Array(chunk as Uint8Array);
		this.deliver(bytes);
		callback();
	}
}

export const MIN_TUI_COLS = 20;
export const MAX_TUI_COLS = 500;
export const MIN_TUI_ROWS = 5;
export const MAX_TUI_ROWS = 300;

export function clampTuiSize(
	cols: number,
	rows: number,
): { cols: number; rows: number } {
	const clamp = (value: number, min: number, max: number) =>
		Math.min(max, Math.max(min, Math.floor(value)));
	return {
		cols: clamp(cols, MIN_TUI_COLS, MAX_TUI_COLS),
		rows: clamp(rows, MIN_TUI_ROWS, MAX_TUI_ROWS),
	};
}

export class WebTuiBridge {
	private sink: TuiOutputSink | null = null;
	private renderer: TuiRendererControl | null = null;
	private suspended = false;
	private shutdownRequested = false;
	private cols: number;
	private rows: number;
	private readonly virtualStdin = new VirtualStdin();
	private readonly virtualStdout: VirtualStdout;

	constructor(cols = 80, rows = 24) {
		const size = clampTuiSize(cols, rows);
		this.cols = size.cols;
		this.rows = size.rows;
		this.virtualStdout = new VirtualStdout(this.cols, this.rows, (bytes) => {
			this.sink?.send(bytes);
		});
	}

	/** Streams and initial geometry for OpenTUI bootstrap. */
	get terminal(): BridgeTerminal {
		return {
			stdin: this.virtualStdin as unknown as NodeJS.ReadStream,
			stdout: this.virtualStdout as unknown as NodeJS.WriteStream,
			width: this.cols,
			height: this.rows,
			onRendererReady: (renderer) => {
				this.renderer = renderer;
				if (this.shutdownRequested) {
					this.renderer = null;
					renderer.destroy();
					return;
				}
				// The client can disconnect while the app is still booting; park
				// the renderer until the next attach.
				if (!this.sink) {
					this.suspended = true;
					renderer.suspend();
				}
			},
		};
	}

	get hasRenderer(): boolean {
		return this.renderer !== null;
	}

	get size(): { cols: number; rows: number } {
		return { cols: this.cols, rows: this.rows };
	}

	/**
	 * Attach the single active client. Resuming a suspended renderer replays
	 * terminal setup (alternate screen, mouse tracking) and forces a full
	 * repaint, which a freshly created browser-side terminal needs.
	 */
	attach(sink: TuiOutputSink, cols: number, rows: number): void {
		this.sink = sink;
		this.setSize(cols, rows);
		if (this.renderer && this.suspended) {
			this.suspended = false;
			this.renderer.resume();
			// Resume restores the previous geometry; apply the (possibly
			// unchanged) client geometry after setup has been replayed.
			this.renderer.resize(this.cols, this.rows);
		}
	}

	detach(sink: TuiOutputSink): void {
		if (this.sink !== sink) return;
		this.sink = null;
		if (this.renderer && !this.suspended) {
			this.suspended = true;
			this.renderer.suspend();
		}
	}

	input(bytes: Uint8Array): boolean {
		if (
			!this.renderer &&
			this.virtualStdin.readableLength + bytes.byteLength > 64 * 1024
		) {
			return false;
		}
		this.virtualStdin.write(Buffer.from(bytes));
		return true;
	}

	resize(cols: number, rows: number): void {
		this.setSize(cols, rows);
		if (this.renderer && !this.suspended) {
			this.renderer.resize(this.cols, this.rows);
		}
	}

	/** Request an orderly application shutdown (renderer owns cleanup). */
	shutdown(): boolean {
		this.shutdownRequested = true;
		const renderer = this.renderer;
		if (!renderer) return false;
		this.renderer = null;
		if (this.suspended) {
			this.suspended = false;
			renderer.resume();
		}
		renderer.destroy();
		return true;
	}

	private setSize(cols: number, rows: number): void {
		const size = clampTuiSize(cols, rows);
		this.cols = size.cols;
		this.rows = size.rows;
		this.virtualStdout.columns = this.cols;
		this.virtualStdout.rows = this.rows;
	}
}
