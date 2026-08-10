/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	type JSX,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { normalizeRemoteHttpUrl } from "../app/remote-url";
import { TIMES } from "../shell/glyphs";
import { isRecord } from "./client-state";
import { useWebClient } from "./WebClientContext";

function requestPayload(request: Record<string, unknown>) {
	return isRecord(request.payload) ? request.payload : {};
}

function OpenUrlToast(props: {
	request: Record<string, unknown>;
}): JSX.Element {
	const { snapshot, controller } = useWebClient();
	let toast: HTMLElement | undefined;
	const requestId = props.request.id as string;
	const payload = createMemo(() => requestPayload(props.request));
	const url = createMemo(() => normalizeRemoteHttpUrl(payload().url));
	const hydrationError = createMemo(() =>
		snapshot().interactionHydrationErrors.get(requestId),
	);
	const responseError = createMemo(() =>
		snapshot().interactionResponseErrors.get(requestId),
	);
	const disabled = createMemo(
		() =>
			snapshot().protocol.phase !== "live" ||
			snapshot().answeringInteractionId !== null ||
			props.request.payloadOmitted === true ||
			url() === null,
	);

	createEffect(() => {
		if (
			props.request.payloadOmitted === true &&
			snapshot().protocol.phase === "live" &&
			!hydrationError()
		) {
			controller.ensureInteractionHydrated(requestId);
		}
	});

	onMount(() => toast?.showPopover());
	onCleanup(() => {
		if (toast?.matches(":popover-open")) toast.hidePopover();
	});

	const dismiss = () => {
		if (snapshot().protocol.phase !== "live") return;
		void controller.answerInteraction(requestId, { opened: false });
	};

	const open = async (): Promise<void> => {
		const target = url();
		if (!target || disabled()) return;
		const popup = window.open("about:blank", "_blank");
		if (!popup) {
			controller.reportInteractionError(
				requestId,
				"Your browser blocked the new tab. Allow popups and try again.",
			);
			return;
		}
		popup.opener = null;
		const referrerPolicy = popup.document.createElement("meta");
		referrerPolicy.name = "referrer";
		referrerPolicy.content = "no-referrer";
		popup.document.head.append(referrerPolicy);
		const accepted = await controller.answerInteraction(requestId, {
			opened: true,
		});
		if (!accepted) {
			popup.close();
			return;
		}
		popup.location.replace(target);
	};

	return (
		<m-toast
			ref={toast}
			class="open-url-toast"
			popover="manual"
			role="status"
			duration="0"
		>
			<button
				class="close"
				type="button"
				aria-label="Dismiss link"
				onClick={dismiss}
			>
				{TIMES}
			</button>
			<b>Open link</b>
			<span>
				{typeof payload().source === "string"
					? `${payload().source} wants to open this URL.`
					: "A plugin wants to open this URL."}
			</span>
			<code>
				{props.request.payloadOmitted === true
					? "Loading link…"
					: (url() ?? "Invalid URL")}
			</code>
			<Show when={hydrationError() ?? responseError()}>
				{(message) => (
					<span class="open-url-error" role="alert">
						{message()}
					</span>
				)}
			</Show>
			<Show when={hydrationError()}>
				<button
					type="button"
					data-variant="ghost"
					onClick={() => controller.retryInteraction(requestId)}
				>
					Retry
				</button>
			</Show>
			<Show when={!hydrationError()}>
				<button
					type="button"
					data-variant="primary"
					disabled={disabled()}
					onClick={() => void open()}
				>
					Open link
				</button>
			</Show>
		</m-toast>
	);
}

export function OpenUrlRequests(): JSX.Element {
	const { snapshot } = useWebClient();
	const request = createMemo(() =>
		snapshot().protocol.pendingInteractions.find(
			(candidate) => isRecord(candidate) && candidate.kind === "open_url",
		),
	);
	const requestKey = createMemo(() => {
		const active = request();
		return active && isRecord(active) && typeof active.id === "string"
			? `${active.id}:${active.payloadOmitted === true ? "omitted" : "ready"}`
			: null;
	});

	return (
		<Show when={requestKey()} keyed>
			{(_key) => {
				const active = request();
				return isRecord(active) ? <OpenUrlToast request={active} /> : null;
			}}
		</Show>
	);
}
