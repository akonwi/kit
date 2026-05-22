import { spawn } from "node:child_process";
import { platform } from "node:os";

export async function openExternal(url: string): Promise<void> {
	const os = platform();

	const command =
		os === "darwin"
			? {
					file: "open",
					args: [url],
					options: { stdio: "ignore" as const, detached: true },
				}
			: os === "win32"
				? {
						file: "cmd",
						args: ["/c", "start", "", url],
						options: {
							stdio: "ignore" as const,
							detached: true,
							windowsHide: true,
						},
					}
				: {
						file: "xdg-open",
						args: [url],
						options: { stdio: "ignore" as const, detached: true },
					};

	await new Promise<void>((resolve, reject) => {
		const proc = spawn(command.file, command.args, command.options);
		proc.once("error", reject);
		proc.once("spawn", () => {
			proc.unref();
			resolve();
		});
	});
}
