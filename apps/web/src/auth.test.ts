import { describe, expect, test } from "bun:test";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	createModels,
	type OAuthCredential,
	type Provider,
} from "@earendil-works/pi-ai";
import { KitCredentialStore } from "./auth";

describe("KitCredentialStore", () => {
	test("persists and lists existing API key and OAuth credentials", async () => {
		const dir = await mkdtemp(join(tmpdir(), "kit-auth-"));
		const path = join(dir, "auth.json");
		const store = new KitCredentialStore(path);
		try {
			await store.modify("anthropic", async () => ({
				type: "api_key",
				key: "secret",
			}));
			await store.modify("openai-codex", async () => ({
				type: "oauth",
				access: "access",
				refresh: "refresh",
				expires: 123,
			}));

			expect(await store.read("anthropic")).toEqual({
				type: "api_key",
				key: "secret",
			});
			expect(await store.list()).toEqual([
				{ providerId: "anthropic", type: "api_key" },
				{ providerId: "openai-codex", type: "oauth" },
			]);
		} finally {
			await rm(dir, { recursive: true, force: true });
		}
	});

	test("persists OAuth refreshes performed by the Pi model runtime", async () => {
		const dir = await mkdtemp(join(tmpdir(), "kit-auth-"));
		const path = join(dir, "auth.json");
		const store = new KitCredentialStore(path);
		try {
			await store.modify("oauth-test", async () => ({
				type: "oauth",
				access: "expired",
				refresh: "refresh",
				expires: 0,
			}));
			const models = createModels({ credentials: store });
			models.setProvider({
				id: "oauth-test",
				name: "OAuth Test",
				auth: {
					oauth: {
						name: "Test login",
						login: async () => {
							throw new Error("not used");
						},
						refresh: async (credential: OAuthCredential) => ({
							...credential,
							access: "fresh",
							expires: Date.now() + 60_000,
						}),
						toAuth: async (credential: OAuthCredential) => ({
							apiKey: credential.access,
						}),
					},
				},
				getModels: () => [],
			} as unknown as Provider);

			expect((await models.getAuth("oauth-test"))?.auth.apiKey).toBe("fresh");
			expect((await store.read("oauth-test"))?.type).toBe("oauth");
			expect((await store.read("oauth-test")) as OAuthCredential).toMatchObject(
				{ access: "fresh" },
			);
		} finally {
			await rm(dir, { recursive: true, force: true });
		}
	});

	test("serializes cross-provider writes to the shared auth file", async () => {
		const dir = await mkdtemp(join(tmpdir(), "kit-auth-"));
		const path = join(dir, "auth.json");
		const store = new KitCredentialStore(path);
		const secondStore = new KitCredentialStore(path);
		try {
			await Promise.all([
				store.modify("anthropic", async () => ({
					type: "api_key",
					key: "one",
				})),
				secondStore.modify("openai", async () => ({
					type: "api_key",
					key: "two",
				})),
			]);

			const persisted = JSON.parse(await readFile(path, "utf8"));
			expect(Object.keys(persisted).sort()).toEqual(["anthropic", "openai"]);
			expect((await stat(path)).mode & 0o777).toBe(0o600);
			await store.delete("anthropic");
			expect(await store.read("anthropic")).toBeUndefined();
			expect(await store.read("openai")).toBeDefined();
		} finally {
			await rm(dir, { recursive: true, force: true });
		}
	});

	test("does not overwrite malformed credential storage", async () => {
		const dir = await mkdtemp(join(tmpdir(), "kit-auth-"));
		const path = join(dir, "auth.json");
		await Bun.write(path, "{ malformed");
		const store = new KitCredentialStore(path);
		try {
			await expect(
				store.modify("anthropic", async () => ({
					type: "api_key",
					key: "secret",
				})),
			).rejects.toBeDefined();
			expect(await readFile(path, "utf8")).toBe("{ malformed");
		} finally {
			await rm(dir, { recursive: true, force: true });
		}
	});
});
