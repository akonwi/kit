export const WORKSPACE_MIN_PRIMARY_COLUMNS = 70;
export const WORKSPACE_MIN_SPLIT_TERMINAL_COLUMNS = 125;
export const WORKSPACE_SHELL_BORDER_COLUMNS = 2;
export const WORKSPACE_MIN_SECONDARY_COLUMNS =
	WORKSPACE_MIN_SPLIT_TERMINAL_COLUMNS -
	WORKSPACE_SHELL_BORDER_COLUMNS -
	WORKSPACE_MIN_PRIMARY_COLUMNS;

export function workspaceContentColumns(terminalColumns: number): number {
	if (!Number.isFinite(terminalColumns)) return 0;
	return Math.max(
		0,
		Math.floor(terminalColumns) - WORKSPACE_SHELL_BORDER_COLUMNS,
	);
}

export type WorkspacePaneLayout = {
	primaryColumns: number;
	secondaryColumns: number;
};

export function resolveWorkspacePaneLayout(input: {
	availableColumns: number;
	preferredPaneRatio: number;
	minPrimaryColumns?: number;
	minSecondaryColumns?: number;
}): WorkspacePaneLayout | null {
	const availableColumns = Math.max(0, Math.floor(input.availableColumns));
	const minPrimaryColumns = Math.max(
		1,
		Math.floor(input.minPrimaryColumns ?? WORKSPACE_MIN_PRIMARY_COLUMNS),
	);
	const minSecondaryColumns = Math.max(
		1,
		Math.floor(input.minSecondaryColumns ?? WORKSPACE_MIN_SECONDARY_COLUMNS),
	);
	if (availableColumns < minPrimaryColumns + minSecondaryColumns) return null;

	const preferredPaneRatio = Number.isFinite(input.preferredPaneRatio)
		? Math.max(0, Math.min(1, input.preferredPaneRatio))
		: 0.4;
	const maxSecondaryColumns = availableColumns - minPrimaryColumns;
	const secondaryColumns = Math.max(
		minSecondaryColumns,
		Math.min(
			maxSecondaryColumns,
			Math.round(availableColumns * preferredPaneRatio),
		),
	);
	return {
		primaryColumns: availableColumns - secondaryColumns,
		secondaryColumns,
	};
}

export function preferredPaneRatioFromDivider(input: {
	availableColumns: number;
	containerLeft: number;
	dividerX: number;
}): number {
	if (input.availableColumns <= 0) return 0.4;
	const localDivider = input.dividerX - input.containerLeft;
	return Math.max(
		0,
		Math.min(
			1,
			(input.availableColumns - localDivider) / input.availableColumns,
		),
	);
}
