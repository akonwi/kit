export type ToastVariant = "error" | "warning" | "info";

export type ToastInput = {
	variant: ToastVariant;
	title: string;
	lines: string[];
};

export type Toast = ToastInput & {
	id: number;
};
