import type { AttachmentDetachReason } from "../../shell/attachments-controller";

export type ReviewDraftDetachAction = "preserve" | "consume" | "clear";

export function reviewDraftDetachAction(
	reason: AttachmentDetachReason,
): ReviewDraftDetachAction {
	if (reason === "pending") return "preserve";
	return reason === "consumed" ? "consume" : "clear";
}
