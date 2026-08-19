import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	testMatch: "**/*.e2e.ts",
	outputDir: "./test-results/web-tui",
	fullyParallel: false,
	workers: 1,
	timeout: 90_000,
	expect: { timeout: 30_000 },
	reporter: process.env.CI ? "line" : "list",
	projects: [
		{
			name: "chromium",
			use: {
				...devices["Desktop Chrome"],
				screenshot: "only-on-failure",
				trace: "retain-on-failure",
				viewport: { width: 1_000, height: 700 },
			},
		},
	],
});
