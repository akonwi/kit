import type {
	OAuthClientProvider,
	OAuthDiscoveryState,
} from "@modelcontextprotocol/sdk/client/auth.js";
import type {
	OAuthClientInformationMixed,
	OAuthClientMetadata,
	OAuthTokens,
} from "@modelcontextprotocol/sdk/shared/auth.js";
import type { StoredMcpOAuthSession } from "./oauth-store";

export const MCP_OAUTH_CALLBACK_PORT = 53173;
export const MCP_OAUTH_CALLBACK_PATH = "/mcp/callback";
export const MCP_OAUTH_CALLBACK_URL = `http://127.0.0.1:${MCP_OAUTH_CALLBACK_PORT}${MCP_OAUTH_CALLBACK_PATH}`;

export class KitMcpOAuthProvider implements OAuthClientProvider {
	readonly clientMetadataUrl = undefined;

	constructor(
		private readonly serverName: string,
		private readonly getSession: () => StoredMcpOAuthSession | undefined,
		private readonly saveSession: (
			session: StoredMcpOAuthSession | undefined,
		) => void | Promise<void>,
		private readonly onRedirect?: (url: URL) => void | Promise<void>,
	) {}

	get redirectUrl(): string {
		return MCP_OAUTH_CALLBACK_URL;
	}

	get clientMetadata(): OAuthClientMetadata {
		return {
			client_name: `Kit MCP Client (${this.serverName})`,
			redirect_uris: [this.redirectUrl],
			grant_types: ["authorization_code", "refresh_token"],
			response_types: ["code"],
			token_endpoint_auth_method: "none",
		};
	}

	clientInformation(): OAuthClientInformationMixed | undefined {
		return this.getSession()?.clientInformation;
	}

	async saveClientInformation(
		clientInformation: OAuthClientInformationMixed,
	): Promise<void> {
		await this.savePartial({ clientInformation });
	}

	tokens(): OAuthTokens | undefined {
		return this.getSession()?.tokens;
	}

	async saveTokens(tokens: OAuthTokens): Promise<void> {
		await this.savePartial({ tokens });
	}

	async redirectToAuthorization(authorizationUrl: URL): Promise<void> {
		await this.onRedirect?.(authorizationUrl);
	}

	async saveCodeVerifier(codeVerifier: string): Promise<void> {
		await this.savePartial({ codeVerifier });
	}

	codeVerifier(): string {
		const codeVerifier = this.getSession()?.codeVerifier;
		if (!codeVerifier) throw new Error("No OAuth code verifier saved.");
		return codeVerifier;
	}

	async saveDiscoveryState(state: OAuthDiscoveryState): Promise<void> {
		await this.savePartial({ discoveryState: state });
	}

	discoveryState(): OAuthDiscoveryState | undefined {
		return this.getSession()?.discoveryState;
	}

	async invalidateCredentials(
		scope: "all" | "client" | "tokens" | "verifier" | "discovery",
	): Promise<void> {
		const next = { ...(this.getSession() ?? {}) };
		if (scope === "all" || scope === "client") delete next.clientInformation;
		if (scope === "all" || scope === "tokens") delete next.tokens;
		if (scope === "all" || scope === "verifier") delete next.codeVerifier;
		if (scope === "all" || scope === "discovery") delete next.discoveryState;
		if (Object.keys(next).length === 0) {
			await this.saveSession(undefined);
			return;
		}
		await this.saveSession(next);
	}

	private async savePartial(partial: StoredMcpOAuthSession): Promise<void> {
		await this.saveSession({
			...(this.getSession() ?? {}),
			...partial,
		});
	}
}
