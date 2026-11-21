// The notices the browser client ships: the license of every package the
// build bundled, the font it serves, and the icon paths it carries. A
// bundled package without a license file, or under a license not listed
// here, stops the build, so a new dependency can't ship unnoticed.
import { readdir } from "node:fs/promises";
import { join } from "node:path";

/** Licenses the client may ship; each is permissive and asks only for its notice. */
export const ALLOWED_LICENSES = new Set([
  "MIT",
  "ISC",
  "0BSD",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "Apache-2.0",
  "OFL-1.1",
]);

/** One component's notice. */
export interface Notice {
  name: string;
  version: string;
  license: string;
  source?: string;
  text: string;
}

/**
 * The package folders a build's inputs come from, sorted. A package nested
 * under another's node_modules is its own folder, as it may be another version.
 */
export function bundledPackages(inputs: Iterable<string>): string[] {
  const dirs = new Set<string>();
  for (const input of inputs) {
    const parts = input.split(/[\\/]/);
    const at = parts.lastIndexOf("node_modules");
    const first = parts[at + 1];
    if (at < 0 || !first) continue;
    const length = first.startsWith("@") ? 3 : 2;
    if (parts.length < at + length + 1) continue;
    dirs.add(parts.slice(0, at + length).join("/"));
  }
  return [...dirs].sort();
}

interface PackageJson {
  name?: string;
  version?: string;
  license?: unknown;
  homepage?: unknown;
  repository?: unknown;
}

/** Where a package's source lives, as its package.json says. */
function source(pkg: PackageJson): string | undefined {
  const repository = pkg.repository;
  const url =
    typeof repository === "string"
      ? repository
      : repository && typeof repository === "object" && "url" in repository
        ? String((repository as { url: unknown }).url)
        : typeof pkg.homepage === "string"
          ? pkg.homepage
          : undefined;
  return url?.replace(/^git\+/, "").replace(/\.git$/, "");
}

/** An allowed license by its SPDX name, whatever case a package writes it in. */
function allowedLicense(name: string): string | undefined {
  const lower = name.toLowerCase();
  return [...ALLOWED_LICENSES].find((license) => license.toLowerCase() === lower);
}

/**
 * Reads an installed package's notice: its license file, then its NOTICE
 * file if it has one, which Apache-2.0 asks a distributor to pass on.
 * Refuses a package without a license file or allowed license.
 */
export async function packageNotice(dir: string): Promise<Notice> {
  const pkg = (await Bun.file(join(dir, "package.json")).json()) as PackageJson;
  const name = pkg.name ?? dir;
  const named = typeof pkg.license === "string" ? pkg.license : "";
  const license = allowedLicense(named);
  if (!license) {
    throw new Error(
      `${name} is licensed ${named ? `as ${named}` : "with no license named"}, which the client doesn’t ship.`,
    );
  }
  const entries = await readdir(dir);
  const file = entries.find((entry) => /^(licen[cs]e|copying)(\.(md|txt))?$/i.test(entry));
  if (!file) throw new Error(`${name} has no license file to ship with it.`);
  const text = (await Bun.file(join(dir, file)).text()).trim();
  if (!text) throw new Error(`${name}’s license file is empty.`);
  const noticeFile = entries.find((entry) => /^notice(\.(md|txt))?$/i.test(entry));
  const notice = noticeFile ? (await Bun.file(join(dir, noticeFile)).text()).trim() : "";
  return {
    name,
    version: pkg.version ?? "",
    license,
    source: source(pkg),
    text: notice ? `${text}\n\n${notice}` : text,
  };
}

/** The notices file: a header, then each component with its license text. */
export function noticesText(notices: readonly Notice[]): string {
  const rule = "-".repeat(72);
  const sections = notices.map((notice) => {
    const title = [notice.name, notice.version].filter(Boolean).join(" ");
    const lines = [title, `License: ${notice.license}`];
    if (notice.source) lines.push(`Source: ${notice.source}`);
    return `${lines.join("\n")}\n\n${notice.text}`;
  });
  return [
    "Kit browser client: third-party notices",
    "",
    "The Kit browser client includes the following software and fonts, each",
    "under its own license.",
    "",
    ...sections.flatMap((section) => [rule, "", section, ""]),
  ].join("\n");
}
