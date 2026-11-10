# TypeScript plugin helper

`kit-plugin.ts` is Kit's helper for writing plugins in TypeScript against
[plugin protocol version 2](../../docs/plugin-protocol/v2.md)
([ADR 0065](../../docs/adrs/0065-stabilize-the-plugin-protocol-as-version-2.md)).
It is one dependency-free file that runs under Bun, and under Node 22.6 or
later with type stripping. `kit-plugin-testing.ts` runs a plugin's setup
against an in-memory Kit for tests.

Plugins copy both files from the Kit repository at the release tag of the Kit
they run; nothing is published to a package registry. A copy's `KIT_RELEASE`
names the release it came from. The repository's
[`ui-api-demo`](../../.kit/plugins/ui-api-demo) fixture uses the helper
through a link instead of a copy.

```ts
import { runPlugin, t, type Kit } from "./kit-plugin.ts";

export async function setup(kit: Kit) {
  await kit.tools.register({
    id: "word_count",
    description: "Count the words in a text",
    input: t.object({ text: t.string() }),
    execute: (input) => `${input.text.split(/\s+/).filter(Boolean).length} words`,
  });
}

if (import.meta.main) runPlugin(setup);
```

```json
{
  "$schema": "https://raw.githubusercontent.com/akonwi/kit/main/docs/plugin-protocol/v2/manifest.schema.json",
  "manifestVersion": 1,
  "protocolVersion": 2,
  "id": "words",
  "transport": { "type": "stdio", "command": "bun", "args": ["plugin.ts"] }
}
```

## Developing the helper

The region between the `BEGIN GENERATED` and `END GENERATED` markers holds
the protocol types, generated from `api/pluginprotocol`. Regenerate it from
the repository root with `go run ./api/pluginprotocol/cmd/emit`; a Go test
fails when it is stale.

```sh
bun install
bun run check   # typecheck, including test/types.ts, then bun test
```

Bun and TypeScript are development dependencies of this directory only;
plugins built with the helper need neither TypeScript nor a package install.
