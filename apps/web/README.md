# Kit browser client

The browser client the Kit server serves to Android phones and browsers
without Kit installed ([ADR 0059](../../docs/adrs/0059-serve-a-browser-client-from-the-kit-server.md),
[user guide](../../docs/features/browser-client.md)). It is a protocol client
written in TypeScript with [Solid](https://www.solidjs.com) and
[Mica](https://github.com/akonwi/mica), on types generated from
[`api/kit-session.openapi.json`](../../api/kit-session.openapi.json).

Bun builds and checks it. Users never need Bun: the `kit` executable embeds
the build.

## Layout

| Path           | Owns                                                                        |
| -------------- | --------------------------------------------------------------------------- |
| `src/api`      | Requests, headers, errors, the SSE parser, generated `schema.ts`            |
| `src/auth`     | The device credential and the reload-once rule                              |
| `src/session`  | Event projection, the stream watch, transcript, composer, interaction docks |
| `src/markdown` | Markdown rendered from mdast as elements, never HTML; Mermaid through Pele  |
| `src/pages`    | Pairing, Sessions, Session                                                  |
| `src/styles`   | Mica with Kit's palette and radii, and the app stylesheet                   |
| `scripts`      | Bundling, the embedded build, the dev server, API type generation           |
| `public`       | The manifest, icons, and favicon served at the root                         |

## Developing

```sh
bun install
bun run dev        # http://127.0.0.1:5199, forwarding /v1 to KIT_URL
bun run check      # API types up to date, Biome, tsc, unit tests
bun run build      # writes internal/webui/build for the kit executable
```

Point the dev server at a Kit server you started for development, never the
one serving your own sessions:

```sh
KIT_URL=http://127.0.0.1:47613 bun run dev
```

To open it from a phone on your tailnet, also listen on the Mac's Tailscale
address: `HOSTS=127.0.0.1,$(tailscale ip -4) KIT_URL=… bun run dev`.

Follow [worktree development](../../.agents/skills/worktree-development/SKILL.md)
to run that server under an isolated `KIT_HOME`. The dev server serves pages
under the same Content Security Policy as the Kit server and rewrites `Origin`
so the server accepts the forwarded requests. Pair the dev page with a link
from `kit server pair`, replacing the link's host with `127.0.0.1:5199`.

After the session contract changes, run `bun run generate:api`. `bun run check`
fails while `src/api/schema.ts` is stale. `PROTOCOL_VERSION` in
`src/api/types.ts` must match the server's.

## Building into kit

`bun run build` writes `internal/webui/build`, which Go embeds. The directory
is ignored except its `.gitignore`. An executable built without it answers the
client's pages with a 404 page. Release builds run `bun run build` and
`scripts/check-web-build.sh`, which refuses a build without `index.html` or
with an asset it names but lacks.
