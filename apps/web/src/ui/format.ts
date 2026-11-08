// Short forms for times, paths, and token counts.

const WEEKDAY = new Intl.DateTimeFormat(undefined, { weekday: "short" });
const MONTH_DAY = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const MONTH_DAY_YEAR = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
});

function startOfDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/** How long ago a session changed: now, 4m, 3h, Yesterday, Mon, Oct 28. */
export function relativeTime(value: string, now: Date = new Date()): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const minutes = Math.floor((now.getTime() - date.getTime()) / 60_000);
  if (minutes < 1) return "now";
  if (minutes < 60) return `${minutes}m`;
  const days = Math.round((startOfDay(now) - startOfDay(date)) / 86_400_000);
  if (days === 0) return `${Math.floor(minutes / 60)}h`;
  if (days === 1) return "Yesterday";
  if (days < 7) return WEEKDAY.format(date);
  return date.getFullYear() === now.getFullYear() ? MONTH_DAY.format(date) : MONTH_DAY_YEAR.format(date);
}

/** A date as the menu says when this browser paired: Nov 7. */
export function shortDate(value: string, now: Date = new Date()): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.getFullYear() === now.getFullYear() ? MONTH_DAY.format(date) : MONTH_DAY_YEAR.format(date);
}

/** The server user's home directory, as health reports it, without a trailing slash. */
export function serverUserHome(home: string): string | undefined {
  if (!home.startsWith("/")) return undefined;
  return home.length > 1 ? home.replace(/\/+$/, "") : home;
}

/** A path with the user's home shown as ~. */
export function displayPath(path: string, home: string | undefined): string {
  if (!home) return path;
  if (path === home) return "~";
  return path.startsWith(`${home}/`) ? `~${path.slice(home.length)}` : path;
}

/** A token count in thousands: 82k, 1.2M. */
export function tokens(count: number): string {
  if (count >= 1_000_000)
    return `${(count / 1_000_000).toFixed(count >= 10_000_000 ? 0 : 1).replace(/\.0$/, "")}M`;
  if (count >= 1000) return `${Math.round(count / 1000)}k`;
  return String(count);
}

/** A file size in decimal units, as the Mac shows it: 412 bytes, 18 KB, 2.1 MB. */
export function bytes(count: number): string {
  if (count < 1000) return `${count} ${count === 1 ? "byte" : "bytes"}`;
  const [value, unit] = count < 1_000_000 ? [count / 1000, "KB"] : [count / 1_000_000, "MB"];
  const shown =
    value < 10 && unit === "MB" ? value.toFixed(1).replace(/\.0$/, "") : String(Math.round(value));
  return `${shown} ${unit}`;
}
