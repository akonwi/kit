import { For, Show } from "solid-js";
import type { ReviewDiffView } from "../../settings";
import { syntaxStyle, theme } from "../../shell/theme";
import type { ReviewHunk, ReviewLine } from "./model";

type ReviewSide = "additions" | "deletions";
type DiffCellKind = "add" | "context" | "delete" | "empty" | "metadata";

type DiffCell = {
	kind: DiffCellKind;
	lineNumber?: number;
	sign: string;
	text: string;
};

type SplitRow = {
	id: string;
	deletion: DiffCell;
	addition: DiffCell;
};

type UnifiedRow = {
	id: string;
	kind: Exclude<DiffCellKind, "empty">;
	deletionLineNumber?: number;
	additionLineNumber?: number;
	sign: string;
	text: string;
};

export type ReviewDiffBlockProps = {
	view: ReviewDiffView;
	hunk?: ReviewHunk;
	rawPatch?: string;
	filetype?: string;
};

function getLineNumberWidth(hunk: ReviewHunk): number {
	const maxLineNumber = Math.max(
		0,
		...hunk.lines.flatMap((line) => [
			line.deletionLineNumber ?? 0,
			line.additionLineNumber ?? 0,
		]),
	);
	return Math.max(1, String(maxLineNumber).length);
}

function formatLineNumber(
	lineNumber: number | undefined,
	width: number,
): string {
	return lineNumber == null
		? " ".repeat(width)
		: String(lineNumber).padStart(width);
}

function cellForLine(line: ReviewLine, side: ReviewSide): DiffCell {
	if (line.kind === "context") {
		return {
			kind: "context",
			lineNumber:
				side === "deletions"
					? line.deletionLineNumber
					: line.additionLineNumber,
			sign: " ",
			text: line.text,
		};
	}
	if (line.kind === "delete") {
		return {
			kind: side === "deletions" ? "delete" : "empty",
			lineNumber: side === "deletions" ? line.deletionLineNumber : undefined,
			sign: side === "deletions" ? "-" : " ",
			text: side === "deletions" ? line.text : "",
		};
	}
	return {
		kind: side === "additions" ? "add" : "empty",
		lineNumber: side === "additions" ? line.additionLineNumber : undefined,
		sign: side === "additions" ? "+" : " ",
		text: side === "additions" ? line.text : "",
	};
}

function buildUnifiedRows(hunk: ReviewHunk): UnifiedRow[] {
	return hunk.lines.map((line, index) => ({
		id: `${hunk.id}:unified:${index}`,
		kind: line.kind,
		deletionLineNumber: line.deletionLineNumber,
		additionLineNumber: line.additionLineNumber,
		sign: line.kind === "add" ? "+" : line.kind === "delete" ? "-" : " ",
		text: line.text,
	}));
}

function buildSplitRows(hunk: ReviewHunk): SplitRow[] {
	const rows: SplitRow[] = [];
	let index = 0;
	while (index < hunk.lines.length) {
		const line = hunk.lines[index];
		if (line.kind === "context") {
			rows.push({
				id: `${hunk.id}:split:${index}`,
				deletion: cellForLine(line, "deletions"),
				addition: cellForLine(line, "additions"),
			});
			index += 1;
			continue;
		}

		const deletions: ReviewLine[] = [];
		const additions: ReviewLine[] = [];
		const startIndex = index;
		while (index < hunk.lines.length) {
			const current = hunk.lines[index];
			if (current.kind === "context") break;
			if (current.kind === "delete") deletions.push(current);
			else additions.push(current);
			index += 1;
		}

		for (
			let rowIndex = 0;
			rowIndex < Math.max(deletions.length, additions.length);
			rowIndex += 1
		) {
			const deletion = deletions[rowIndex];
			const addition = additions[rowIndex];
			rows.push({
				id: `${hunk.id}:split:${startIndex}:${rowIndex}`,
				deletion: deletion
					? cellForLine(deletion, "deletions")
					: { kind: "empty", sign: " ", text: "" },
				addition: addition
					? cellForLine(addition, "additions")
					: { kind: "empty", sign: " ", text: "" },
			});
		}
	}
	return rows;
}

function backgroundForKind(kind: DiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffAddedBg;
		case "delete":
			return theme.diffRemovedBg;
		case "metadata":
			return theme.bgMuted;
		default:
			return theme.bgSurface;
	}
}

function contentBackgroundForKind(kind: DiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.diffAddedContentBg;
		case "delete":
			return theme.diffRemovedContentBg;
		default:
			return backgroundForKind(kind);
	}
}

function signColorForKind(kind: DiffCellKind): string {
	switch (kind) {
		case "add":
			return theme.toolText;
		case "delete":
			return theme.errorText;
		case "metadata":
			return theme.metaText;
		default:
			return theme.textMuted;
	}
}

function textColorForKind(kind: DiffCellKind): string {
	if (kind === "metadata") return theme.metaText;
	if (kind === "empty") return theme.textPlaceholder;
	return theme.textPrimary;
}

function renderContentText(
	text: string,
	kind: DiffCellKind,
	filetype: string | undefined,
) {
	const bg = () => contentBackgroundForKind(kind);
	if (filetype && kind !== "metadata" && kind !== "empty") {
		return (
			<code
				content={text}
				filetype={filetype}
				syntaxStyle={syntaxStyle()}
				fg={textColorForKind(kind)}
				bg={bg()}
				wrapMode="none"
				flexGrow={1}
				height={1}
				flexShrink={0}
			/>
		);
	}
	return (
		<text
			fg={textColorForKind(kind)}
			bg={bg()}
			flexGrow={1}
			height={1}
			flexShrink={0}
		>
			{text}
		</text>
	);
}

function renderUnifiedRow(
	row: UnifiedRow,
	lineNumberWidth: number,
	filetype: string | undefined,
) {
	const bg = () => backgroundForKind(row.kind);
	return (
		<box flexDirection="row" backgroundColor={bg()} height={1} flexShrink={0}>
			<text fg={theme.textMuted} bg={bg()}>
				{formatLineNumber(row.deletionLineNumber, lineNumberWidth)}
			</text>
			<text fg={theme.textMuted} bg={bg()}>
				{" "}
				{formatLineNumber(row.additionLineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(row.kind)} bg={bg()}>
				{row.sign}{" "}
			</text>
			{renderContentText(row.text, row.kind, filetype)}
		</box>
	);
}

function renderSplitCell(
	cell: DiffCell,
	lineNumberWidth: number,
	filetype: string | undefined,
) {
	const bg = () => backgroundForKind(cell.kind);
	return (
		<box
			width="50%"
			flexDirection="row"
			backgroundColor={bg()}
			height={1}
			flexShrink={0}
		>
			<text fg={theme.textMuted} bg={bg()}>
				{formatLineNumber(cell.lineNumber, lineNumberWidth)}
			</text>
			<text fg={signColorForKind(cell.kind)} bg={bg()}>
				{cell.sign}{" "}
			</text>
			{renderContentText(cell.text, cell.kind, filetype)}
		</box>
	);
}

function rawPatchRows(rawPatch: string): UnifiedRow[] {
	return rawPatch
		.replace(/\r\n/g, "\n")
		.split("\n")
		.filter((line) => line.length > 0)
		.map((line, index) => {
			const kind =
				line.startsWith("@@") ||
				line.startsWith("diff --git") ||
				line.startsWith("index ") ||
				line.startsWith("--- ") ||
				line.startsWith("+++ ")
					? "metadata"
					: line.startsWith("+")
						? "add"
						: line.startsWith("-")
							? "delete"
							: "context";
			return {
				id: `raw:${index}`,
				kind,
				sign: kind === "add" ? "+" : kind === "delete" ? "-" : " ",
				text:
					kind === "add" || kind === "delete" || line.startsWith(" ")
						? line.slice(1)
						: line,
			};
		});
}

export function ReviewDiffBlock(props: ReviewDiffBlockProps) {
	return (
		<Show
			when={props.hunk}
			fallback={
				<box flexDirection="column" gap={0}>
					<For each={rawPatchRows(props.rawPatch ?? "")}>
						{(row) => renderUnifiedRow(row, 1, props.filetype)}
					</For>
				</box>
			}
		>
			{(hunk) => {
				const lineNumberWidth = () => getLineNumberWidth(hunk());
				return (
					<Show
						when={props.view === "split"}
						fallback={
							<box flexDirection="column" gap={0}>
								<For each={buildUnifiedRows(hunk())}>
									{(row) =>
										renderUnifiedRow(row, lineNumberWidth(), props.filetype)
									}
								</For>
							</box>
						}
					>
						<box flexDirection="column" gap={0}>
							<For each={buildSplitRows(hunk())}>
								{(row) => (
									<box
										flexDirection="row"
										width="100%"
										height={1}
										flexShrink={0}
									>
										{renderSplitCell(
											row.deletion,
											lineNumberWidth(),
											props.filetype,
										)}
										{renderSplitCell(
											row.addition,
											lineNumberWidth(),
											props.filetype,
										)}
									</box>
								)}
							</For>
						</box>
					</Show>
				);
			}}
		</Show>
	);
}
