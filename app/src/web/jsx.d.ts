import "solid-js";

declare module "solid-js" {
	namespace JSX {
		interface IntrinsicElements {
			"m-vstack": HTMLAttributes<HTMLElement> & {
				gap?: string;
				align?: string;
				justify?: string;
			};
			"m-hstack": HTMLAttributes<HTMLElement> & {
				gap?: string;
				align?: string;
				justify?: string;
			};
		}
	}
}
