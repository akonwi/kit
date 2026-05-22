import { execFile } from "node:child_process";

export type GitHubPullRequest = {
	number: number;
	title: string;
	url: string;
	headRefName: string;
	baseRefName: string;
};

const GH_TIMEOUT_MS = 2_500;
const PR_VIEW_FIELDS = [
	"number",
	"title",
	"url",
	"headRefName",
	"baseRefName",
].join(",");

export async function getCurrentBranchPullRequest(
	cwd: string,
	branch: string | null,
): Promise<GitHubPullRequest | null> {
	if (!isNamedBranch(branch)) return null;
	const stdout = await runGh(["pr", "view", "--json", PR_VIEW_FIELDS], cwd);
	if (!stdout) return null;
	return parsePullRequest(stdout);
}

export function parsePullRequest(json: string): GitHubPullRequest | null {
	let value: unknown;
	try {
		value = JSON.parse(json);
	} catch {
		return null;
	}
	if (!isRecord(value)) return null;
	const number = value.number;
	if (typeof number !== "number" || !Number.isInteger(number)) return null;

	return {
		number,
		title: optionalString(value.title),
		url: optionalString(value.url),
		headRefName: optionalString(value.headRefName),
		baseRefName: optionalString(value.baseRefName),
	};
}

function isNamedBranch(branch: string | null): branch is string {
	return (
		branch != null &&
		branch.length > 0 &&
		branch !== "detached" &&
		!branch.startsWith("detached@")
	);
}

function optionalString(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function runGh(args: string[], cwd: string): Promise<string | null> {
	return new Promise((resolve) => {
		execFile(
			"gh",
			args,
			{
				cwd,
				encoding: "utf8",
				timeout: GH_TIMEOUT_MS,
			},
			(error, stdout) => {
				if (error) {
					resolve(null);
					return;
				}
				resolve(stdout.trim());
			},
		);
	});
}
