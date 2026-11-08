// Builds the browser client into internal/webui/build, which the kit
// executable embeds (ADR 0059).
import { fileURLToPath } from "node:url";
import { bundle } from "./bundle";

const outdir = fileURLToPath(new URL("../../../internal/webui/build", import.meta.url));
await bundle({ outdir, minify: true });
// The directory's .gitignore keeps build output out of the checkout.
await Bun.write(
  `${outdir}/.gitignore`,
  "# The browser client build (apps/web) writes here; see ADR 0059.\n*\n!.gitignore\n",
);
console.log(`Built the browser client into ${outdir}`);
