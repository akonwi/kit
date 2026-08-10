import { spawn } from "node:child_process";
import { platform } from "node:os";

export type ExternalOpenCommand = {
	file: string;
	args: string[];
	options: {
		stdio: "ignore";
		detached: true;
		windowsHide?: true;
	};
};

export function getExternalOpenCommand(
	url: string,
	os: NodeJS.Platform = platform(),
): ExternalOpenCommand {
	if (os === "darwin") {
		return {
			file: "open",
			args: [url],
			options: { stdio: "ignore", detached: true },
		};
	}
	if (os === "win32") {
		return {
			file: "rundll32.exe",
			args: ["url.dll,FileProtocolHandler", url],
			options: { stdio: "ignore", detached: true, windowsHide: true },
		};
	}
	return {
		file: "xdg-open",
		args: [url],
		options: { stdio: "ignore", detached: true },
	};
}

export async function openExternal(url: string): Promise<void> {
	const command = getExternalOpenCommand(url);
	await new Promise<void>((resolve, reject) => {
		const proc = spawn(command.file, command.args, command.options);
		proc.once("error", reject);
		proc.once("spawn", () => {
			proc.unref();
			resolve();
		});
	});
}
