/** @jsxImportSource solid-js */
import { createContext, useContext } from "solid-js";

export type ActivitySource =
	| { kind: "single-item"; itemId: string; turnId: string }
	| {
			kind: "turn-intermediate";
			turnId: string;
			anchorItemId: string;
	  };

export type WorkspaceContextValue = {
	openActivity(source: ActivitySource): void;
	isActivityOpen(source: ActivitySource): boolean;
};

export const WorkspaceContext = createContext<WorkspaceContextValue>();

export function useWorkspace(): WorkspaceContextValue {
	const value = useContext(WorkspaceContext);
	if (!value) throw new Error("WorkspacePaneHost is missing");
	return value;
}
