import type { SessionSummary } from "../../session";
import { TREE_BRANCH, TREE_CORNER } from "../../shell/glyphs";

export type SessionTreeNode = {
	session: SessionSummary;
	children: SessionTreeNode[];
};

export type SessionTreeRow = {
	session: SessionSummary;
	depth: number;
	isCurrent: boolean;
	isLeaf: boolean;
	isLastChild: boolean;
	ancestorHasNextSibling: boolean[];
};

function compareSessions(a: SessionSummary, b: SessionSummary): number {
	return (
		new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime() ||
		new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
	);
}

export function buildRelatedSessionTree(
	sessions: SessionSummary[],
	currentSessionId: string,
): SessionTreeNode | null {
	const byId = new Map(sessions.map((session) => [session.id, session]));
	const current = byId.get(currentSessionId);
	if (!current) return null;

	let root = current;
	const seen = new Set<string>([root.id]);
	while (root.parentSessionId) {
		const parent = byId.get(root.parentSessionId);
		if (!parent || seen.has(parent.id)) break;
		root = parent;
		seen.add(parent.id);
	}

	const childrenByParent = new Map<string, SessionSummary[]>();
	for (const session of sessions) {
		if (!session.parentSessionId) continue;
		const children = childrenByParent.get(session.parentSessionId) ?? [];
		children.push(session);
		childrenByParent.set(session.parentSessionId, children);
	}

	for (const children of childrenByParent.values()) {
		children.sort(compareSessions);
	}

	const buildNode = (
		session: SessionSummary,
		visited: Set<string>,
	): SessionTreeNode => {
		if (visited.has(session.id)) {
			return { session, children: [] };
		}

		const nextVisited = new Set(visited);
		nextVisited.add(session.id);
		return {
			session,
			children: (childrenByParent.get(session.id) ?? []).map((child) =>
				buildNode(child, nextVisited),
			),
		};
	};

	return buildNode(root, new Set());
}

function latestUpdatedAt(node: SessionTreeNode): number {
	return Math.max(
		new Date(node.session.updatedAt).getTime(),
		...node.children.map(latestUpdatedAt),
	);
}

function compareSessionNodes(a: SessionTreeNode, b: SessionTreeNode): number {
	return (
		latestUpdatedAt(b) - latestUpdatedAt(a) ||
		compareSessions(a.session, b.session)
	);
}

export function buildSessionForest(
	sessions: SessionSummary[],
): SessionTreeNode[] {
	const byId = new Map(sessions.map((session) => [session.id, session]));
	const childrenByParent = new Map<string, SessionSummary[]>();

	for (const session of sessions) {
		const parentId = session.parentSessionId;
		if (!parentId || parentId === session.id || !byId.has(parentId)) continue;
		const children = childrenByParent.get(parentId) ?? [];
		children.push(session);
		childrenByParent.set(parentId, children);
	}

	for (const children of childrenByParent.values()) {
		children.sort(compareSessions);
	}

	const visited = new Set<string>();
	const buildNode = (
		session: SessionSummary,
		path: Set<string>,
	): SessionTreeNode => {
		visited.add(session.id);
		const nextPath = new Set(path);
		nextPath.add(session.id);
		return {
			session,
			children: (childrenByParent.get(session.id) ?? [])
				.filter((child) => !visited.has(child.id) && !nextPath.has(child.id))
				.map((child) => buildNode(child, nextPath)),
		};
	};

	const rootSessions = sessions
		.filter(
			(session) =>
				!session.parentSessionId ||
				session.parentSessionId === session.id ||
				!byId.has(session.parentSessionId),
		)
		.sort(compareSessions);
	const roots: SessionTreeNode[] = [];

	for (const session of rootSessions) {
		if (!visited.has(session.id)) roots.push(buildNode(session, new Set()));
	}

	for (const session of [...sessions].sort(compareSessions)) {
		if (!visited.has(session.id)) roots.push(buildNode(session, new Set()));
	}

	return roots.sort(compareSessionNodes);
}

function flattenSessionNodes(
	roots: SessionTreeNode[],
	currentSessionId: string,
): SessionTreeRow[] {
	const rows: SessionTreeRow[] = [];

	const visit = (
		node: SessionTreeNode,
		depth: number,
		ancestorHasNextSibling: boolean[],
		isLastChild: boolean,
	) => {
		rows.push({
			session: node.session,
			depth,
			isCurrent: node.session.id === currentSessionId,
			isLeaf: node.children.length === 0,
			isLastChild,
			ancestorHasNextSibling,
		});

		node.children.forEach((child, index) => {
			visit(
				child,
				depth + 1,
				[...ancestorHasNextSibling, !isLastChild],
				index === node.children.length - 1,
			);
		});
	};

	roots.forEach((root, index) => {
		visit(root, 0, [], index === roots.length - 1);
	});
	return rows;
}

export function flattenSessionTree(
	root: SessionTreeNode,
	currentSessionId: string,
): SessionTreeRow[] {
	return flattenSessionNodes([root], currentSessionId);
}

export function flattenSessionForest(
	roots: SessionTreeNode[],
	currentSessionId: string,
): SessionTreeRow[] {
	return flattenSessionNodes(roots, currentSessionId);
}

export function findSessionRowIndex(
	rows: SessionTreeRow[],
	sessionId: string,
): number {
	return rows.findIndex((row) => row.session.id === sessionId);
}

export function getSessionTreeTitle(row: SessionTreeRow): string {
	return (
		row.session.name?.trim() ||
		row.session.firstMessage?.trim() ||
		row.session.id.slice(0, 8)
	);
}

export function formatSessionTreePrefix(row: SessionTreeRow): string {
	if (row.depth === 0) return "";

	const columns = row.ancestorHasNextSibling
		.slice(1)
		.map((hasNext) => (hasNext ? "│  " : "   "))
		.join("");
	const branch = row.isLastChild ? TREE_CORNER : TREE_BRANCH;
	return `${columns}${branch}`;
}

export function formatSessionTreeLabel(row: SessionTreeRow): string {
	return `${formatSessionTreePrefix(row)}${getSessionTreeTitle(row)}`;
}
