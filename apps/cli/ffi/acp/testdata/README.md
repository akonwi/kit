The ACP tests validate every message `kit acp` writes against the official
Agent Client Protocol v1 JSON schema.

`acp-v1-schema.json` is `schema/v1/schema.json` from
https://github.com/agentclientprotocol/agent-client-protocol at commit
7441d106a853682cce61d2dd7db54123e4b9a477 (schema crate 1.29.1, 2026-11-07),
copied unchanged. It is licensed under the Apache License 2.0 by its authors.
Replace it with a newer copy of the same file to check Kit against a later v1
schema.

The `*.golden` files are protocol transcripts the golden tests compare with:
`->` lines are written by the client and `<-` lines by `kit acp`, with
generated identifiers, times, and paths replaced by placeholders. Rewrite them
after an intended change with:

```sh
KIT_UPDATE_ACP_GOLDEN=1 go test ./ffi/acp
```

## ACP Test Compatibility Kit

`kit acp` is also checked with the [ACP Test Compatibility
Kit](https://github.com/EugeneTheDev/acp-tck) for v1, which needs
[uv](https://docs.astral.sh/uv/). The compiled test binary stands in for
`kit acp` when `KIT_ACP_TEST_AGENT=1`: each process runs its own Kit server on
a temporary home with the tests' scripted provider, so the kit needs no
provider login and touches no real Kit home. The scripted prompt `slow` runs
until it is cancelled. From `apps/cli`:

```sh
go test -c -o /tmp/kit-acp-test-agent ./ffi/acp
cd path/to/acp-tck
uv run acp-tck --agent-cwd "$(mktemp -d)" --agent-env KIT_ACP_TEST_AGENT=1 \
  --cancel-prompt slow -- /tmp/kit-acp-test-agent
```

The verdict is expected to be `CONFORMANT`, with one advisory failure,
`ACP-SESSION-004`: Kit does not connect to the MCP servers a client passes
(ADR 0063).
