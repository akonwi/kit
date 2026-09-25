import { describe, expect, test } from "bun:test";
import { getExternalOpenCommand } from "./open-external";

const URL_WITH_COMMAND_CHARACTERS =
	"https://example.com/search?first=one&second=two%7Cthree";

describe("external URL opener", () => {
	test("does not route Windows URLs through cmd.exe", () => {
		expect(
			getExternalOpenCommand(URL_WITH_COMMAND_CHARACTERS, "win32"),
		).toEqual({
			file: "rundll32.exe",
			args: ["url.dll,FileProtocolHandler", URL_WITH_COMMAND_CHARACTERS],
			options: { stdio: "ignore", detached: true, windowsHide: true },
		});
	});

	test("passes URLs as one argument on macOS and Linux", () => {
		expect(
			getExternalOpenCommand(URL_WITH_COMMAND_CHARACTERS, "darwin").args,
		).toEqual([URL_WITH_COMMAND_CHARACTERS]);
		expect(
			getExternalOpenCommand(URL_WITH_COMMAND_CHARACTERS, "linux").args,
		).toEqual([URL_WITH_COMMAND_CHARACTERS]);
	});
});
