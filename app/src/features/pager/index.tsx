import type { InternalPluginAPI } from "../../plugins";
import { PagerFeedbackAttachment } from "./attachment";
import { PagerContent } from "./PagerContent";
import {
	createPagerController,
	type PagerController,
} from "./pager-controller";

export type { PagerController } from "./pager-controller";

const PAGER_FEEDBACK_ATTACHMENT_ID = "pager-feedback";

export function PagerPlugin(kit: InternalPluginAPI): () => void {
	const pager: PagerController = createPagerController();
	let opening: Promise<void> | null = null;
	let closeOverlay: (() => void) | null = null;
	let projectedAttachment: PagerFeedbackAttachment | null = null;
	let ownerSessionId = kit.session.get().id;

	function clearDraft(expectedGeneration?: number): boolean {
		if (!pager.clearDraft(expectedGeneration)) return false;
		closeOverlay?.();
		return true;
	}

	function syncDraftAttachment(): void {
		const feedback = pager.getFeedbackMessage();
		const snapshot = pager.getDraftSnapshot();
		if (!feedback || !snapshot) {
			kit.attachments.detach(PAGER_FEEDBACK_ATTACHMENT_ID);
			return;
		}

		const generation = pager.draftGeneration;
		const attachment = new PagerFeedbackAttachment(
			PAGER_FEEDBACK_ATTACHMENT_ID,
			feedback,
			pager.getNoteCount(),
			snapshot,
			{
				onDetach: () => {
					clearDraft(generation);
				},
				onOpen: async () => {
					if (!pager.reopen()) return;
					await openPager();
				},
			},
		);
		projectedAttachment = attachment;
		kit.attachments.attach(attachment);
	}

	async function openPager(): Promise<void> {
		if (opening) return opening;
		const promise = kit.ui
			.custom<void>((props) => {
				closeOverlay = () => props.done(undefined);
				return (
					<PagerContent
						pager={pager}
						onClose={closeOverlay}
						surfaceProps={props.surfaceProps}
					/>
				);
			})
			.finally(() => {
				if (pager.active) pager.closeView();
				opening = null;
				closeOverlay = null;
				syncDraftAttachment();
			});
		opening = promise;
		return promise;
	}

	const unsubscribeAttachments = kit.attachments.subscribe((event) => {
		if (event.type !== "attached") return;
		if (!(event.attachment instanceof PagerFeedbackAttachment)) return;
		if (event.attachment === projectedAttachment) return;
		pager.restoreDraft(event.attachment.snapshot);
		syncDraftAttachment();
	});

	const retainedAttachment = kit.attachments
		.attachments()
		.find(
			(attachment) =>
				attachment.id === PAGER_FEEDBACK_ATTACHMENT_ID &&
				attachment instanceof PagerFeedbackAttachment,
		);
	if (retainedAttachment instanceof PagerFeedbackAttachment) {
		pager.restoreDraft(retainedAttachment.snapshot);
		syncDraftAttachment();
	}

	// Auto-activate pager when the last assistant response substantially
	// overflows the visible transcript viewport.
	// Respects the `pager` setting; `/pager` always works regardless.
	kit.on("agent.turn.completed", async () => {
		if (kit.settings.get().pager === false) return;
		if (pager.active) return;
		if (
			pager.tryAutoActivate(
				kit.session.getMessages(),
				kit.ui.getTranscriptViewport(),
			)
		) {
			await openPager();
		}
	});

	kit.on("session.active.changed", (event) => {
		if (event.session.id === ownerSessionId) return;
		ownerSessionId = event.session.id;
		kit.attachments.detach(PAGER_FEEDBACK_ATTACHMENT_ID);
		clearDraft();
	});

	kit.registerCommand(
		"pager",
		{ description: "Open pager for last assistant response, or close if open" },
		async () => {
			if (pager.active) {
				pager.closeView();
				closeOverlay?.();
				return;
			}
			if (!pager.tryActivate(kit.session.getMessages())) {
				kit.ui.toast({
					title: "No assistant response to paginate.",
					variant: "warning",
				});
				return;
			}
			await openPager();
		},
	);

	return () => {
		unsubscribeAttachments();
		const wasActive = pager.active;
		const hasAttachedProjection = kit.attachments
			.attachments()
			.some((attachment) => attachment.id === PAGER_FEEDBACK_ATTACHMENT_ID);
		pager.closeView();
		closeOverlay?.();
		// Preserve a visible or actively edited draft across same-session plugin
		// reloads, but never resurrect a projection provisionally removed while
		// message submission is pending.
		if (wasActive || hasAttachedProjection) syncDraftAttachment();
	};
}
