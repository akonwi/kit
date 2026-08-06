# Kit RPC browser harness

A temporary, dependency-free browser stress harness for Kit's RPC mode. It starts one long-running Kit child and permits one token-authenticated WebSocket client at a time.

From `app/`:

```sh
bun examples/rpc-web-ui/server.ts
```

Open the stderr-reported address (defaults to `127.0.0.1:4173`). Set `HOST` or `PORT` to override it. The page supports prompts and therefore may make model calls; the smoke test does not:

```sh
bun examples/rpc-web-ui/smoke.ts
```
