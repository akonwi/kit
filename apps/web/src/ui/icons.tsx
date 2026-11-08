// Kit's icons: Phosphor, as the Mac app draws them, and Kit's own marks.
import { PHOSPHOR } from "./phosphor";

export type IconName = keyof typeof PHOSPHOR;

/** A Phosphor icon in a square box of `size` pixels, in the current color. */
export function Icon(props: { name: IconName; size?: number; class?: string }) {
  const size = () => props.size ?? 20;
  return (
    <svg
      class={props.class ? `icon ${props.class}` : "icon"}
      width={size()}
      height={size()}
      viewBox="0 0 256 256"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d={PHOSPHOR[props.name]} />
    </svg>
  );
}

export function Spinner(props: { size?: number; class?: string }) {
  const size = () => props.size ?? 14;
  return (
    <svg
      class={props.class ? `spinner ${props.class}` : "spinner"}
      width={size()}
      height={size()}
      viewBox="0 0 16 16"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-opacity="0.35" stroke-width="2" />
      <path class="arc" d="M8 2a6 6 0 016 6" stroke-width="2" stroke-linecap="round" />
    </svg>
  );
}

export function NeedsBadge(props: { size?: number }) {
  const size = () => props.size ?? 16;
  return (
    <svg class="needs-badge" width={size()} height={size()} viewBox="0 0 16 16" aria-hidden="true">
      <circle class="disc" cx="8" cy="8" r="8" />
      <path
        d="M6.2 6.1a1.9 1.9 0 113 1.5c-.7.4-1.2.8-1.2 1.6"
        class="mark"
        stroke-width="1.6"
        fill="none"
        stroke-linecap="round"
      />
      <circle class="dot" cx="8" cy="11.6" r=".95" />
    </svg>
  );
}

/** Kit's K_ mark: the K takes the text color, the underscore stays blue. */
export function Logo(props: { height?: number }) {
  const height = () => props.height ?? 18;
  return (
    <svg
      class="logo"
      width={Math.round((height() * 1024) / 768)}
      height={height()}
      viewBox="150 140 730 490"
      role="img"
      aria-label="Kit"
    >
      <path
        fill="currentColor"
        d="M187.120 610.300L187.120 157.700L264.620 157.700L264.620 342.460L317.940 342.460L404.120 157.700L487.820 157.700L386.140 373.460L492.780 610.300L406.600 610.300L317.320 410.040L264.620 410.040L264.620 610.300Z"
      />
      <path fill="#4F65FF" d="M549.200 610.300L549.200 540.240L846.800 540.240L846.800 610.300Z" />
    </svg>
  );
}
