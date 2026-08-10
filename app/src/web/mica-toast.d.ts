declare module "@akonwi/mica/toast.js" {
	export type MicaToastOptions = {
		description?: string;
		variant?: "" | "danger" | "success" | "warning";
		duration?: number;
	};

	export function toast(title: string, options?: MicaToastOptions): HTMLElement;
}
