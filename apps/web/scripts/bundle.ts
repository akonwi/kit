// Bundles the browser client into a directory laid out the way
// internal/webui serves it: index.html, the root files a phone reads, and
// hashed build files under assets/.
import { createHash } from "node:crypto";
import { mkdir, readdir, rename, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { transformAsync } from "@babel/core";
// @ts-expect-error babel presets ship no types
import typescriptPreset from "@babel/preset-typescript";
// @ts-expect-error babel presets ship no types
import solidPreset from "babel-preset-solid";
import type { BunPlugin } from "bun";

const root = fileURLToPath(new URL("..", import.meta.url));

/** Compiles Solid's JSX with its own Babel preset, which Bun's bundler lacks. */
export const solidPlugin: BunPlugin = {
  name: "solid",
  setup(build) {
    build.onLoad({ filter: /\.tsx$/ }, async ({ path }) => {
      const source = await Bun.file(path).text();
      const result = await transformAsync(source, {
        filename: path,
        babelrc: false,
        configFile: false,
        presets: [
          [solidPreset, { generate: "dom", hydratable: false }],
          [typescriptPreset, { onlyRemoveTypeImports: true }],
        ],
      });
      return { contents: result?.code ?? "", loader: "js" };
    });
  },
};

export interface BundleOptions {
  outdir: string;
  minify: boolean;
}

/** Builds the client into options.outdir, replacing what was there. */
export async function bundle({ outdir, minify }: BundleOptions): Promise<void> {
  await rm(outdir, { recursive: true, force: true });
  await mkdir(join(outdir, "assets"), { recursive: true });

  const result = await Bun.build({
    entrypoints: [join(root, "src/main.tsx")],
    outdir,
    target: "browser",
    format: "esm",
    // Pele loads as a chunk of its own when a reply first shows a diagram.
    splitting: true,
    minify,
    sourcemap: "none",
    plugins: [solidPlugin],
    // Fonts are copied as files of their own below; the policy allows no data: fonts.
    external: ["/fonts/*"],
    naming: {
      entry: "assets/[name]-[hash].[ext]",
      chunk: "assets/[name]-[hash].[ext]",
      asset: "assets/[name]-[hash].[ext]",
    },
    define: { "process.env.NODE_ENV": JSON.stringify(minify ? "production" : "development") },
  });
  if (!result.success) {
    for (const log of result.logs) console.error(log);
    throw new Error("The browser client did not build.");
  }

  const relative = (path: string) => path.slice(outdir.length).replace(/^\/+/, "");
  const script = result.outputs.find(
    (output) => output.kind === "entry-point" && output.path.endsWith(".js"),
  );
  if (!script) throw new Error("The browser client build has no script.");
  const styles: string[] = [];
  for (const output of result.outputs.filter((value) => value.path.endsWith(".css"))) {
    styles.push(await placeFonts(output.path, outdir));
  }

  const template = await Bun.file(join(root, "src/index.html")).text();
  const links = styles.map((style) => `<link rel="stylesheet" href="/${relative(style)}">`).join("\n    ");
  const html = template
    .replace("<!-- styles -->", links)
    .replace("<!-- script -->", `<script type="module" src="/${relative(script.path)}"></script>`);
  await Bun.write(join(outdir, "index.html"), html);

  const publicDir = join(root, "public");
  for (const name of await readdir(publicDir)) {
    await Bun.write(join(outdir, name), Bun.file(join(publicDir, name)));
  }

  await compress(join(outdir, "assets"));
}

function contentHash(data: Uint8Array | string): string {
  return createHash("sha256").update(data).digest("hex").slice(0, 10);
}

/**
 * Copies the fonts a stylesheet names under /fonts/ into assets/ with
 * content-hashed names, points the stylesheet at them, and renames the
 * stylesheet by its final content. Returns the stylesheet's new path.
 */
async function placeFonts(path: string, outdir: string): Promise<string> {
  let css = await Bun.file(path).text();
  const fonts = new Set(css.match(/\/fonts\/[a-z0-9-]+\.woff2/g) ?? []);
  for (const font of fonts) {
    const name = font.slice("/fonts/".length);
    const source = join(root, "node_modules/@fontsource/jetbrains-mono/files", name);
    const data = new Uint8Array(await Bun.file(source).arrayBuffer());
    const hashed = `${name.replace(/\.woff2$/, "")}-${contentHash(data)}.woff2`;
    await Bun.write(join(outdir, "assets", hashed), data);
    css = css.replaceAll(font, `/assets/${hashed}`);
  }
  const renamed = join(outdir, "assets", `main-${contentHash(css)}.css`);
  await Bun.write(path, css);
  await rename(path, renamed);
  return renamed;
}

/** Writes a gzip sibling for each compressible build file that it shrinks. */
async function compress(dir: string): Promise<void> {
  for (const name of await readdir(dir)) {
    if (!/\.(js|css|svg|json)$/.test(name)) continue;
    const path = join(dir, name);
    if ((await stat(path)).size < 1024) continue;
    const data = new Uint8Array(await Bun.file(path).arrayBuffer());
    const zipped = gzipSync(data, { level: 9 });
    if (zipped.byteLength < data.byteLength) await Bun.write(`${path}.gz`, zipped);
  }
}
