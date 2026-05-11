export type ToastVariant = "error" | "warning" | "info";

export type ToastInput = {
	variant: ToastVariant;
	title: string;
	lines: string[];
	persistent?: boolean;
};

export type Toast = ToastInput & {
	id: number;
};
