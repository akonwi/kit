import type { ImageMessagePart, MessagePart } from "../../messages/parts";
import { messagePartToPromptText } from "../../messages/parts";
import type { Attachment } from "../../shell/attachments-controller";

export class ImageAttachment implements Attachment {
	readonly type = "image";
	readonly icon = "🖼️";
	readonly summary: string;

	constructor(
		public readonly id: string,
		public readonly filename: string,
		public readonly mimeType: string,
		public readonly data: string,
	) {
		this.summary = filename;
	}

	toMessagePart(): MessagePart {
		const part: ImageMessagePart = {
			type: "image",
			data: this.data,
			mimeType: this.mimeType,
			filename: this.filename,
		};
		return part;
	}

	toPromptText(): string {
		return messagePartToPromptText(this.toMessagePart());
	}
}
