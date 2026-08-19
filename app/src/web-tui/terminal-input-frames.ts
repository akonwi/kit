export const MAX_TERMINAL_INPUT_FRAME_BYTES = 16 * 1024;

/** Split browser input below the server's WebSocket payload ceiling. */
export function terminalInputFrames(
	data: string,
	encoder = new TextEncoder(),
): Uint8Array[] {
	const bytes = encoder.encode(data);
	const frames: Uint8Array[] = [];
	for (
		let offset = 0;
		offset < bytes.byteLength;
		offset += MAX_TERMINAL_INPUT_FRAME_BYTES
	) {
		frames.push(bytes.slice(offset, offset + MAX_TERMINAL_INPUT_FRAME_BYTES));
	}
	return frames;
}
