import { randomUUID } from "node:crypto";
import { link, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";

export type FileMutationResult = {
	updated: boolean;
	content: string;
};

export type FileOperations = {
	read(filePath: string): Promise<string>;
	write(filePath: string, content: string): Promise<void>;
	mutate(
		filePath: string,
		update: (current: string) => string,
	): Promise<FileMutationResult>;
	mutateOrCreate(
		filePath: string,
		update: (current: string) => string,
	): Promise<FileMutationResult>;
};

export const defaultFileOperations: FileOperations = {
	read: (filePath) => readFile(filePath, "utf8"),
	async write(filePath, content) {
		await mkdir(path.dirname(filePath), { recursive: true });
		await writeFile(filePath, content, "utf8");
	},
	async mutate(filePath, update) {
		const current = await readFile(filePath, "utf8");
		const content = update(current);
		if (content === current) return { updated: false, content };
		await writeFile(filePath, content, "utf8");
		return { updated: true, content };
	},
	async mutateOrCreate(filePath, update) {
		try {
			return await defaultFileOperations.mutate(filePath, update);
		} catch (error) {
			if (!hasErrorCode(error, "ENOENT")) throw error;
		}

		const initialContent = update("");
		const directory = path.dirname(filePath);
		const temporaryPath = path.join(
			directory,
			`.${path.basename(filePath)}.${randomUUID()}.tmp`,
		);
		await mkdir(directory, { recursive: true });
		try {
			await writeFile(temporaryPath, initialContent, {
				encoding: "utf8",
				flag: "wx",
				mode: 0o600,
			});
			try {
				await link(temporaryPath, filePath);
			} catch (error) {
				if (!hasErrorCode(error, "EEXIST")) throw error;
				return defaultFileOperations.mutate(filePath, update);
			}
			return { updated: initialContent.length > 0, content: initialContent };
		} finally {
			await rm(temporaryPath, { force: true }).catch(() => {});
		}
	},
};

function hasErrorCode(error: unknown, code: string): boolean {
	return (
		typeof error === "object" &&
		error !== null &&
		"code" in error &&
		error.code === code
	);
}

export type FileOperationHandler = {
	handles(filePath: string): boolean;
	read(filePath: string): string | Promise<string>;
	write(filePath: string, content: string): void | Promise<void>;
	mutate(
		filePath: string,
		update: (current: string) => string,
	): FileMutationResult | Promise<FileMutationResult>;
};
