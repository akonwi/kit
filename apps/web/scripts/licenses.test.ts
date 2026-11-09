import { afterAll, describe, expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { bundledPackages, noticesText, packageNotice } from "./licenses";

const scratch = await mkdtemp(join(tmpdir(), "kit-web-licenses-"));
afterAll(() => rm(scratch, { recursive: true, force: true }));

async function fixture(name: string, pkg: Record<string, unknown>, files: Record<string, string> = {}) {
  const dir = join(scratch, name);
  await Bun.write(join(dir, "package.json"), JSON.stringify(pkg));
  for (const [file, text] of Object.entries(files)) await Bun.write(join(dir, file), text);
  return dir;
}

describe("bundledPackages", () => {
  test("names each package folder the build read from, once", () => {
    expect(
      bundledPackages([
        "src/main.tsx",
        "node_modules/solid-js/dist/solid.js",
        "node_modules/solid-js/web/dist/web.js",
        "node_modules/@akonwi/mica/dist/mica.css",
        "node_modules/micromark/index.js",
        "node_modules/mdast-util-gfm/node_modules/micromark/index.js",
        "node_modules/@scope",
      ]),
    ).toEqual([
      "node_modules/@akonwi/mica",
      "node_modules/mdast-util-gfm/node_modules/micromark",
      "node_modules/micromark",
      "node_modules/solid-js",
    ]);
  });
});

describe("packageNotice", () => {
  test("reads the license file a package ships, and where its source lives", async () => {
    const dir = await fixture(
      "ok",
      {
        name: "ok",
        version: "1.2.3",
        license: "MIT",
        repository: { url: "git+https://github.com/x/ok.git" },
      },
      { "license.md": "MIT License\n\nCopyright (c) X\n" },
    );
    expect(await packageNotice(dir)).toEqual({
      name: "ok",
      version: "1.2.3",
      license: "MIT",
      source: "https://github.com/x/ok",
      text: "MIT License\n\nCopyright (c) X",
    });
  });

  test("refuses a license the client doesn't ship, or none", async () => {
    const gpl = await fixture("gpl", { name: "gpl", license: "GPL-3.0" }, { LICENSE: "GPL" });
    await expect(packageNotice(gpl)).rejects.toThrow(
      "gpl is licensed as GPL-3.0, which the client doesn’t ship.",
    );
    const unnamed = await fixture("unnamed", { name: "unnamed" }, { LICENSE: "?" });
    await expect(packageNotice(unnamed)).rejects.toThrow("unnamed is licensed with no license named");
  });

  test("refuses a package with no license file", async () => {
    const dir = await fixture("bare", { name: "bare", license: "MIT" }, { "README.md": "hi" });
    await expect(packageNotice(dir)).rejects.toThrow("bare has no license file to ship with it.");
  });
});

describe("noticesText", () => {
  test("lists each component with its license and text", () => {
    expect(
      noticesText([
        { name: "a", version: "1.0.0", license: "MIT", source: "https://a.dev", text: "A license" },
        { name: "Icons", version: "", license: "MIT", text: "Icon license" },
      ]),
    ).toBe(
      [
        "Kit browser client: third-party notices",
        "",
        "The Kit browser client includes the following software and fonts, each",
        "under its own license.",
        "",
        "-".repeat(72),
        "",
        "a 1.0.0\nLicense: MIT\nSource: https://a.dev\n\nA license",
        "",
        "-".repeat(72),
        "",
        "Icons\nLicense: MIT\n\nIcon license",
        "",
      ].join("\n"),
    );
  });
});
