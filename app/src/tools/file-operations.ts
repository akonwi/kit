import { mkdir, readFile, writeFile } from "node:fs/promises";
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
};

export type FileOperationHandler = {
	handles(filePath: string): boolean;
	read(filePath: string): string | Promise<string>;
	write(filePath: string, content: string): void | Promise<void>;
	mutate(
		filePath: string,
		update: (current: string) => string,
	): FileMutationResult | Promise<FileMutationResult>;
};
