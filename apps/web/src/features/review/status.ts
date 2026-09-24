import { theme } from "../../shell/theme";
import type { ReviewFile } from "./model";

export function reviewStatusLabel(file: ReviewFile): string {
	switch (file.status) {
		case "new":
			return "A";
		case "deleted":
			return "D";
		case "rename-pure":
		case "rename-changed":
			return "R";
		default:
			return "M";
	}
}

export function reviewStatusText(file: ReviewFile): string {
	switch (file.status) {
		case "new":
			return "new";
		case "deleted":
			return "deleted";
		case "rename-pure":
			return "renamed";
		case "rename-changed":
			return "renamed, modified";
		default:
			return "modified";
	}
}

export function reviewStatusColor(file: ReviewFile): string {
	switch (file.status) {
		case "new":
			return theme.toolText;
		case "deleted":
			return theme.errorText;
		case "rename-pure":
		case "rename-changed":
			return theme.warningText;
		default:
			return theme.warningText;
	}
}
