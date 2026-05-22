import { execFile } from "node:child_process";
import { platform } from "node:os";

export type SpeechVoice = {
	name: string;
	locale?: string;
	sample?: string;
};

export type SpeechVoiceDiscovery =
	| {
			supported: true;
			voices: SpeechVoice[];
	  }
	| {
			supported: false;
			reason: string;
			voices: SpeechVoice[];
	  };

function parseVoiceLine(line: string): SpeechVoice | null {
	const trimmed = line.trim();
	if (!trimmed) return null;

	const parts = trimmed.split(/\s{2,}/).map((part) => part.trim());
	if (parts.length === 0 || parts[0].length === 0) return null;

	const [name, locale, sampleWithMarker] = parts;
	const sample = sampleWithMarker?.replace(/^#\s*/, "").trim();
	return {
		name,
		...(locale ? { locale } : {}),
		...(sample ? { sample } : {}),
	};
}

export async function discoverSpeechVoices(): Promise<SpeechVoiceDiscovery> {
	if (platform() !== "darwin") {
		return {
			supported: false,
			reason: "Voice selection is currently available on macOS only.",
			voices: [],
		};
	}

	return new Promise<SpeechVoiceDiscovery>((resolve) => {
		execFile("say", ["-v", "?"], (error, stdout) => {
			if (error) {
				resolve({
					supported: false,
					reason: `Could not discover system voices: ${error.message}`,
					voices: [],
				});
				return;
			}

			const voices = stdout
				.split(/\r?\n/)
				.map(parseVoiceLine)
				.filter((voice): voice is SpeechVoice => voice !== null)
				.sort((a, b) => a.name.localeCompare(b.name));

			resolve({ supported: true, voices });
		});
	});
}
