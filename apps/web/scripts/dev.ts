// Serves the client from a fresh build and forwards the session API to a
// running Kit server, so the client can change without rebuilding kit.
//
//   KIT_URL=http://127.0.0.1:PORT bun run dev
//
// Pages get the same security headers the server sends. Use a development
// server on an isolated KIT_HOME, never the shared one.
import { watch } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { bundle } from "./bundle";

const target = new URL(process.env.KIT_URL ?? "");
if (!/^https?:$/.test(target.protocol)) {
  console.error("Set KIT_URL to a Kit server, such as http://127.0.0.1:47613.");
  process.exit(1);
}
const port = Number(process.env.PORT ?? 5199);
const outdir = fileURLToPath(new URL("../.dev-build", import.meta.url));
const csp =
  "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' blob: data:; " +
  "connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

let building: Promise<void> = bundle({ outdir, minify: false });
let queued = false;
const rebuild = () => {
  if (queued) return;
  queued = true;
  building = building.then(async () => {
    queued = false;
    try {
      await bundle({ outdir, minify: false });
      console.log("Rebuilt.");
    } catch (error) {
      console.error(error);
    }
  });
};
watch(fileURLToPath(new URL("../src", import.meta.url)), { recursive: true }, rebuild);
await building;

const pages = (pathname: string) =>
  pathname === "/" || pathname === "/pair" || /^\/sessions\/[^/]+$/.test(pathname);

// Listens on loopback unless HOSTS lists addresses, such as
// "127.0.0.1,100.x.y.z" so a phone on the tailnet can open the page too.
// Avoid a LAN address, where the
// pairing code and device token would cross the network unencrypted.
const hosts = (process.env.HOSTS ?? "127.0.0.1")
  .split(",")
  .map((host) => host.trim())
  .filter(Boolean);

async function serve(request: Request): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname.startsWith("/v1/")) {
    // The server accepts a browser origin naming its own host.
    const headers = new Headers(request.headers);
    headers.delete("host");
    if (headers.has("origin")) headers.set("origin", target.origin);
    return fetch(new URL(url.pathname + url.search, target), {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual",
    });
  }
  await building;
  const file = Bun.file(join(outdir, pages(url.pathname) ? "index.html" : url.pathname.slice(1)));
  if (url.pathname.includes("..") || !(await file.exists()))
    return new Response("Not found", { status: 404 });
  return new Response(file, { headers: { "Content-Security-Policy": csp, "Cache-Control": "no-store" } });
}

for (const hostname of hosts) {
  Bun.serve({ port, hostname, idleTimeout: 0, fetch: serve });
  console.log(`Kit web client on http://${hostname}:${port}, forwarding /v1 to ${target.origin}`);
}
