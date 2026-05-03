export type McpConfigSource =
	| "shared-user"
	| "kit-user"
	| "shared-project"
	| "kit-project";

export type McpServerAuth = {
	type: "oauth" | "bearer";
	bearerToken?: string;
	bearerTokenEnv?: string;
};

export type McpServerDefinition =
	| {
			name: string;
			type: "stdio";
			command: string;
			args: string[];
			env: Record<string, string>;
			cwd?: string;
			description?: string;
			disabled: boolean;
			auth?: McpServerAuth;
			source: McpConfigSource;
			filePath: string;
	  }
	| {
			name: string;
			type: "http";
			url: string;
			headers: Record<string, string>;
			description?: string;
			disabled: boolean;
			auth?: McpServerAuth;
			source: McpConfigSource;
			filePath: string;
	  };

export type LoadMcpConfigResult = {
	servers: McpServerDefinition[];
	warnings: string[];
	files: Array<{ source: McpConfigSource; filePath: string; loaded: boolean }>;
};

export type McpToolMetadata = {
	serverName: string;
	name: string;
	canonicalName: string;
	description: string;
	inputSchema?: Record<string, unknown>;
};

export type McpServerStatus =
	| "disabled"
	| "configured"
	| "connecting"
	| "connected"
	| "error";

export type McpServerRuntimeState = {
	name: string;
	status: McpServerStatus;
	type: McpServerDefinition["type"];
	description?: string;
	source: McpConfigSource;
	filePath: string;
	toolCount: number;
	lastError?: string;
	disabled: boolean;
};
