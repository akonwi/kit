import { describe, expect, test } from "bun:test";
import { normalizePublicUrl, WebAccessPolicy } from "./web-access-policy";

function request(
	url: string,
	options: { origin?: string; authorization?: string; method?: string } = {},
): Request {
	return new Request(url, {
		method: options.method,
		headers: {
			...(options.origin ? { origin: options.origin } : {}),
			...(options.authorization
				? { authorization: options.authorization }
				: {}),
		},
	});
}

describe("normalizePublicUrl", () => {
	test("accepts HTTP origins and strips a root path", () => {
		expect(normalizePublicUrl("https://Kit.Example.com:8443/")).toBe(
			"https://kit.example.com:8443",
		);
		expect(normalizePublicUrl("http://localhost:4783")).toBe(
			"http://localhost:4783",
		);
	});

	test("rejects unsupported or non-origin URLs", () => {
		for (const value of [
			"ftp://kit.example.com",
			"https://user:secret@kit.example.com",
			"https://kit.example.com/subpath",
			"https://kit.example.com/?query=yes",
			"not a URL",
		]) {
			expect(normalizePublicUrl(value)).toBeNull();
		}
	});
});

describe("WebAccessPolicy", () => {
	test("combines the listener, public URL, and explicit allowlists", () => {
		const policy = new WebAccessPolicy({
			authRealm: "Kit test",
			publicUrl: "https://kit.example.com",
			allowedHosts: ["extra.example.com"],
			allowedOrigins: ["https://extra.example.com"],
		});
		policy.setListenerAddress("127.0.0.1", 4783);

		expect(policy.isAllowedHost("127.0.0.1:4783")).toBe(true);
		expect(policy.isAllowedHost("localhost:4783")).toBe(true);
		expect(policy.isAllowedHost("kit.example.com")).toBe(true);
		expect(policy.isAllowedHost("extra.example.com")).toBe(true);
		expect(policy.isAllowedHost("other.example.com")).toBe(false);

		const internalUrl = new URL("http://127.0.0.1:4783/api/rpc");
		expect(
			policy.isAllowedWebSocketRequest(
				request(internalUrl.href, { origin: "https://kit.example.com" }),
				internalUrl,
			),
		).toBe(true);
		expect(
			policy.isAllowedWebSocketRequest(
				request(internalUrl.href, { origin: "https://attacker.example" }),
				internalUrl,
			),
		).toBe(false);
	});

	test("keeps a canonical HTTPS origin authoritative over proxy-reconstructed HTTP", () => {
		const policy = new WebAccessPolicy({
			authRealm: "Kit test",
			publicUrl: "https://kit.example.com",
		});
		const url = new URL("http://kit.example.com/api/rpc");
		expect(
			policy.isAllowedWebSocketRequest(
				request(url.href, { origin: "https://kit.example.com" }),
				url,
			),
		).toBe(true);
		expect(
			policy.isAllowedWebSocketRequest(
				request(url.href, { origin: "http://kit.example.com" }),
				url,
			),
		).toBe(false);
	});

	test("does not trust forwarded topology headers", () => {
		const policy = new WebAccessPolicy({
			authRealm: "Kit test",
			publicUrl: "https://kit.example.com",
		});
		policy.setListenerAddress("127.0.0.1", 4783);
		const url = new URL("http://proxy.invalid:4783/api/rpc");
		const forwarded = new Request(url, {
			headers: {
				host: "proxy.invalid:4783",
				origin: "https://kit.example.com",
				forwarded: "host=kit.example.com;proto=https",
				"x-forwarded-host": "kit.example.com",
				"x-forwarded-proto": "https",
			},
		});
		expect(policy.isAllowedHost(url.host)).toBe(false);
		expect(policy.isAllowedWebSocketRequest(forwarded, url)).toBe(false);
	});

	test("validates Basic auth with a timing-safe digest", () => {
		const policy = new WebAccessPolicy({
			authRealm: "Kit test",
			basicAuth: { username: "user", password: "secret:extra" },
		});
		const authorization = `Basic ${Buffer.from("user:secret:extra").toString("base64")}`;
		expect(
			policy.isAuthorized(request("http://localhost", { authorization })),
		).toBe(true);
		expect(policy.isAuthorized(request("http://localhost"))).toBe(false);
		expect(
			policy.authenticationRequiredResponse().headers.get("www-authenticate"),
		).toBe('Basic realm="Kit test", charset="UTF-8"');
	});

	test("adds public and request hosts to CSP WebSocket sources", () => {
		const policy = new WebAccessPolicy({
			authRealm: "Kit test",
			publicUrl: "https://kit.example.com",
		});
		expect(
			policy.webSocketConnectSources(new URL("http://127.0.0.1:4783/")),
		).toBe("ws://127.0.0.1:4783 wss://127.0.0.1:4783 wss://kit.example.com");
	});
});
