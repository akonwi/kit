import type { ReviewFile } from "./model";

export function buildReviewFeedbackMessage(options: {
	files: ReviewFile[];
	fileNotes: Map<string, string>;
	hunkNotes: Map<string, string>;
}): string | null {
	const blocks: string[] = [];

	for (const file of options.files) {
		const sections: string[] = [];
		const fileNote = options.fileNotes.get(file.noteKey)?.trim();
		if (fileNote) {
			sections.push(`### File-level feedback\n${fileNote}`);
		}

		for (const hunk of file.hunks) {
			const note = options.hunkNotes.get(hunk.noteKey)?.trim();
			if (!note) continue;
			sections.push(`### Hunk: ${hunk.header}\n${note}`);
		}

		if (sections.length === 0) continue;
		blocks.push(`## ${file.path}\n\n${sections.join("\n\n")}`);
	}

	if (blocks.length === 0) return null;

	return [
		"Here is my review feedback on the current uncommitted changes.",
		"",
		...blocks.flatMap((block, index) => (index === 0 ? [block] : ["", block])),
		"",
		"Please use this review feedback to revise the changes.",
	].join("\n");
}
