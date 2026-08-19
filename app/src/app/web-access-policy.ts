import { createHash, timingSafeEqual } from "node:crypto";

export type WebBasicAuthCredentials = {
	username: string;
	password: string;
};

export type WebAccessPolicyOptions = {
	hostname?: string;
	port?: number;
	publicUrl?: string;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
	basicAuth?: WebBasicAuthCredentials;
	authRealm: string;
};

function credentialDigest(value: string): Buffer {
	return createHash("sha256").update(value, "utf8").digest();
}

function decodeBasicAuthorization(header: string | null): string | null {
	const match = header?.match(
		/^Basic ((?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?)$/i,
	);
	const encoded = match?.[1];
	if (!encoded) return null;
	try {
		return new TextDecoder("utf-8", { fatal: true }).decode(
			Buffer.from(encoded, "base64"),
		);
	} catch {
		return null;
	}
}

export function normalizeWebOrigin(value: string): string | null {
	if (value === "null") return value;
	try {
		const origin = new URL(value).origin.toLowerCase();
		return origin === "null" ? null : origin;
	} catch {
		return null;
	}
}

/** Normalize a canonical external URL. Kit currently serves only at `/`. */
export function normalizePublicUrl(value: string): string | null {
	if (value.length > 2_048) return null;
	try {
		const url = new URL(value);
		if (url.protocol !== "http:" && url.protocol !== "https:") return null;
		if (url.username || url.password) return null;
		if (url.pathname !== "/" || url.search || url.hash) return null;
		return url.origin.toLowerCase();
	} catch {
		return null;
	}
}

export class WebAccessPolicy {
	private readonly expectedBasicAuthDigest: Buffer | null;
	private readonly publicOrigin: string | null;
	private listenerHostname: string;
	private listenerPort: number;

	constructor(private readonly options: WebAccessPolicyOptions) {
		this.listenerHostname = options.hostname ?? "127.0.0.1";
		this.listenerPort = options.port ?? 4782;
		this.expectedBasicAuthDigest = options.basicAuth
			? credentialDigest(
					`${options.basicAuth.username}:${options.basicAuth.password}`,
				)
			: null;
		this.publicOrigin = options.publicUrl
			? normalizePublicUrl(options.publicUrl)
			: null;
		if (options.publicUrl && !this.publicOrigin) {
			throw new Error(`Invalid public URL: ${options.publicUrl}`);
		}
	}

	setListenerAddress(hostname: string, port: number): void {
		this.listenerHostname = hostname;
		this.listenerPort = port;
	}

	isAllowedHost(host: string): boolean {
		return (
			this.options.allowedHosts?.includes("*") === true ||
			this.allowedHosts().has(host.toLowerCase())
		);
	}

	isAllowedWebSocketRequest(request: Request, url: URL): boolean {
		return (
			this.isAllowedHost(url.host) &&
			this.isAllowedOrigin(
				request.headers.get("origin"),
				url,
				this.options.allowOriginless === true,
			)
		);
	}

	isAllowedHttpRequest(request: Request, url: URL): boolean {
		return (
			this.isAllowedHost(url.host) &&
			this.isAllowedOrigin(
				request.headers.get("origin"),
				url,
				request.method === "GET" || this.options.allowOriginless === true,
			)
		);
	}

	isAuthorized(request: Request): boolean {
		if (!this.expectedBasicAuthDigest) return true;
		const credentials = decodeBasicAuthorization(
			request.headers.get("authorization"),
		);
		const actualDigest = credentialDigest(credentials ?? "");
		return timingSafeEqual(this.expectedBasicAuthDigest, actualDigest);
	}

	authenticationRequiredResponse(): Response {
		return new Response("Authentication required", {
			status: 401,
			headers: {
				"cache-control": "no-store",
				"www-authenticate": `Basic realm="${this.options.authRealm}", charset="UTF-8"`,
			},
		});
	}

	corsHeaders(request: Request): Record<string, string> {
		const header = request.headers.get("origin");
		const origin = header ? normalizeWebOrigin(header) : null;
		return origin
			? {
					...(this.expectedBasicAuthDigest
						? { "access-control-allow-credentials": "true" }
						: {}),
					"access-control-allow-origin": origin,
					vary: "origin",
				}
			: {};
	}

	webSocketConnectSources(requestUrl: URL): string {
		const sources = new Set<string>();
		const publicUrl = this.publicOrigin ? new URL(this.publicOrigin) : null;
		if (!publicUrl || publicUrl.host !== requestUrl.host) {
			sources.add(`ws://${requestUrl.host}`);
			sources.add(`wss://${requestUrl.host}`);
		}
		if (publicUrl) {
			sources.add(
				`${publicUrl.protocol === "https:" ? "wss:" : "ws:"}//${publicUrl.host}`,
			);
		}
		return [...sources].join(" ");
	}

	private isAllowedOrigin(
		origin: string | null,
		requestUrl: URL,
		allowOriginless: boolean,
	): boolean {
		if (!origin) return allowOriginless;
		const normalizedOrigin = normalizeWebOrigin(origin);
		if (!normalizedOrigin) return false;
		return (
			this.options.allowedOrigins?.includes("*") === true ||
			this.allowedOrigins(requestUrl).has(normalizedOrigin)
		);
	}

	private allowedOrigins(requestUrl: URL): Set<string> {
		const publicUrl = this.publicOrigin ? new URL(this.publicOrigin) : null;
		// A TLS-terminating proxy may reconstruct the backend URL as HTTP while
		// preserving the public Host. In that case the canonical public scheme is
		// authoritative; accepting the reconstructed HTTP origin would weaken an
		// explicitly configured HTTPS boundary.
		const origins = new Set<string>();
		if (!publicUrl || requestUrl.host !== publicUrl.host) {
			origins.add(requestUrl.origin.toLowerCase());
		}
		if (this.publicOrigin) origins.add(this.publicOrigin);
		for (const value of this.options.allowedOrigins ?? []) {
			if (value === "*") continue;
			const origin = normalizeWebOrigin(value);
			if (origin) origins.add(origin);
		}
		return origins;
	}

	private allowedHosts(): Set<string> {
		const hosts = new Set(
			(this.options.allowedHosts ?? []).map((host) => host.toLowerCase()),
		);
		hosts.add(`${this.listenerHostname}:${this.listenerPort}`.toLowerCase());
		if (
			this.listenerHostname === "127.0.0.1" ||
			this.listenerHostname === "::1"
		) {
			hosts.add(`localhost:${this.listenerPort}`);
			hosts.add(`127.0.0.1:${this.listenerPort}`);
			hosts.add(`[::1]:${this.listenerPort}`);
		}
		if (this.publicOrigin) hosts.add(new URL(this.publicOrigin).host);
		return hosts;
	}
}
