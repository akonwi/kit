# kit

Kit is a TUI coding agent heavily inspired by [Pi](https://pi.dev) and built on top of [`pi-agent-core`](https://github.com/earendil-works/pi-mono/tree/main/packages/agent) with [OpenTUI](https://opentui.com/).

[akonwi.io/kit](https://akonwi.io/kit)

## Requirements

- [Bun](https://bun.sh) `>= 1.3.0`

## Install

From npm:

```bash
bun install --global @akonwi/kit
```

From a checkout:

```bash
bun install
bun run build
bun link
```

The packaged CLI uses the compiled binary as its non-development entry point.

## Usage

```bash
kit                  # resumes the most recent session for the current directory or starts a new one
kit --no-session     # runs the interactive TUI with in-memory session state
kit -p "review this" # runs without the TUI and resumes the current directory's latest session
kit -p --no-session "review this" # runs in memory without persisting a session
kit -p --model openai/gpt-5.5 "review this" # selects an exact provider/model
kit -p --session abc123 "continue this" # continues and persists a specific session
kit --rpc            # runs as a headless JSONL subprocess
kit --rpc --no-session # runs the RPC conversation in memory
kit --web            # serves the current directory's latest session in the browser
kit --web --no-session # serves an in-memory browser session
kit -s abc123        # opens a specific session by ID (long or short id)
kit threads          # launches a session picker
```

## What Kit includes

- terminal-first coding agent workflow
- session restore and persistence
- slash commands, prompt commands, and skills
- settings UI and app-owned settings
- code review tools and diff browser

For feature details, including the [RPC mode protocol](docs/features/rpc-mode.md),
see [`docs/features/`](docs/features/).

## Development

Commit messages in this repo should use [Conventional Commits](https://www.conventionalcommits.org/), preferably in the form `type(scope): summary`.

Run from source:

```bash
bun run dev
```

Build the distributed binary:

```bash
bun run build
```

Preview the npm package contents:

```bash
bun run pack:dry
```

## More documentation

- feature docs: [`docs/features/`](docs/features/)
- ADRs: [`docs/adrs/`](docs/adrs/)
- project guidance: [`AGENTS.md`](AGENTS.md)
