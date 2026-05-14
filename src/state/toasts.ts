export type ToastVariant = "error" | "warning" | "info";

export type ToastInput = {
	variant: ToastVariant;
	title: string;
	subtitle?: string;
	persistent?: boolean;
};

export type Toast = ToastInput & {
	id: number;
};
