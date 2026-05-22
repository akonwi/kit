export function formatTimeAgo(date: Date): string {
	const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
	if (seconds < 60) return "just now";
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m ago`;
	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours}h ago`;
	const days = Math.floor(hours / 24);
	if (days < 30) return `${days}d ago`;
	return date.toLocaleDateString();
}

export function formatSessionOption(
	session: {
		name?: string;
		firstMessage?: string;
		id: string;
		cwd: string;
		updatedAt: string;
	},
	widths?: {
		cwd: number;
		updatedAt: number;
	},
): { label: string; description: string } {
	const home = process.env.HOME || process.env.USERPROFILE || "";
	const label =
		session.name ||
		session.firstMessage?.slice(0, 60) ||
		session.id.slice(0, 8);
	const cwd = session.cwd.startsWith(home)
		? `~${session.cwd.slice(home.length)}`
		: session.cwd;
	const ago = formatTimeAgo(new Date(session.updatedAt));

	if (!widths) {
		return {
			label,
			description: `${cwd}  ${ago}`,
		};
	}

	return {
		label,
		description: `${cwd.padStart(widths.cwd)}  ${ago.padStart(widths.updatedAt)}`,
	};
}
