# Solid Configuration

## Project Setup

### Quick Start

```bash
bunx create-tui@latest -t solid my-app
cd my-app && bun install
```

The CLI creates the `my-app` directory for you - it must **not already exist**.

Options: `--no-git` (skip git init), `--no-install` (skip bun install)

### Manual Setup

```bash
mkdir my-tui && cd my-tui
bun init
bun install @opentui/solid @opentui/core solid-js
```

## TypeScript Configuration

### tsconfig.json

```json
{
  "compilerOptions": {
    "lib": ["ESNext"],
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    
    "jsx": "preserve",
    "jsxImportSource": "@opentui/solid",
    
    "strict": true,
    "skipLibCheck": true,
    "noEmit": true,
    "types": ["bun-types"]
  },
  "include": ["src/**/*"]
}
```

**Critical settings:**
- `jsx: "preserve"` - Let Solid's compiler handle JSX
- `jsxImportSource: "@opentui/solid"` - Import JSX runtime from OpenTUI Solid

## Bun Configuration

### bunfig.toml

**For development only** - use the CLI flag instead to support compiled binaries:

```bash
bun --preload=@opentui/solid/preload src/index.tsx
```

**Why not bunfig.toml?**

The `preload` in bunfig.toml causes runtime errors in compiled binaries because the compiled executable tries to resolve the npm module at runtime. Instead:

1. **Development**: Use `--preload` CLI flag
2. **Build**: Pass the solid plugin directly to `Bun.build`

If you only need development mode and won't compile, bunfig.toml works:

```toml
preload = ["@opentui/solid/preload"]
```

But **do not use this if building a compiled binary**.

## Package Configuration

### package.json

```json
{
  "name": "my-tui-app",
  "type": "module",
  "scripts": {
    "start": "bun --preload=@opentui/solid/preload src/index.tsx",
    "dev": "bun --preload=@opentui/solid/preload --watch src/index.tsx",
    "test": "bun test",
    "build": "bun run build.ts"
  },
  "dependencies": {
    "@opentui/core": "latest",
    "@opentui/solid": "latest",
    "solid-js": "latest"
  },
  "devDependencies": {
    "@types/bun": "latest",
    "typescript": "latest"
  }
}
```

## Project Structure

Recommended structure:

```
my-tui-app/
├── src/
│   ├── components/
│   │   ├── Header.tsx
│   │   ├── Sidebar.tsx
│   │   └── MainContent.tsx
│   ├── stores/
│   │   └── appStore.ts
│   ├── App.tsx
│   └── index.tsx
├── bunfig.toml           # Optional - see Bun Configuration section
├── package.json
└── tsconfig.json
```

### Entry Point (src/index.tsx)

```tsx
import { render } from "@opentui/solid"
import { App } from "./App"

render(() => <App />)
```

### App Component (src/App.tsx)

```tsx
import { Header } from "./components/Header"
import { Sidebar } from "./components/Sidebar"
import { MainContent } from "./components/MainContent"

export function App() {
  return (
    <box flexDirection="column" width="100%" height="100%">
      <Header />
      <box flexDirection="row" flexGrow={1}>
        <Sidebar />
        <MainContent />
      </box>
    </box>
  )
}
```

## Renderer Configuration

### render() Options

```tsx
import { render } from "@opentui/solid"
import { ConsolePosition } from "@opentui/core"

render(() => <App />, {
  // Rendering
  targetFPS: 60,
  
  // Behavior
  exitOnCtrlC: true,
  autoFocus: true,          // Auto-focus elements on click (default: true)
  useMouse: true,           // Enable mouse support (default: true)
  
  // Debug console
  consoleOptions: {
    position: ConsolePosition.BOTTOM,
    sizePercent: 30,
    startInDebugMode: false,
  },
  
  // Cleanup
  onDestroy: () => {
    // Cleanup code
  },
})
```

### Using Existing Renderer

```tsx
import { render } from "@opentui/solid"
import { createCliRenderer } from "@opentui/core"

const renderer = await createCliRenderer({
  exitOnCtrlC: false,
})

render(() => <App />, renderer)
```

## Building for Distribution

### Build Script (build.ts)

```typescript
import solidPlugin from "@opentui/solid/bun-plugin"

await Bun.build({
  entrypoints: ["./src/index.tsx"],
  outdir: "./dist",
  target: "bun",
  minify: true,
  plugins: [solidPlugin],
})

console.log("Build complete!")
```

Run: `bun run build.ts`

### Creating Executables

**Critical**: Do NOT use `preload` in bunfig.toml when building executables. The preload causes runtime errors because compiled binaries cannot resolve npm modules.

```typescript
import solidPlugin from "@opentui/solid/bun-plugin"

await Bun.build({
  entrypoints: ["./src/index.tsx"],
  target: "bun",
  plugins: [solidPlugin],
  compile: {
    outfile: "my-app",
    // target: "bun-darwin-arm64", // optional, defaults to current platform
  },
})
```

**Available targets:**
- `bun-darwin-arm64` - macOS Apple Silicon
- `bun-darwin-x64` - macOS Intel
- `bun-linux-x64` - Linux x64
- `bun-linux-arm64` - Linux ARM64
- `bun-windows-x64` - Windows x64

Or omit `target` to compile for the current platform automatically.

## Environment Variables

Create `.env` for development:

```env
# Debug settings
OTUI_SHOW_STATS=false
SHOW_CONSOLE=false

# App settings
API_URL=https://api.example.com
```

Bun auto-loads `.env` files:

```tsx
const apiUrl = process.env.API_URL
```

## Testing Configuration

### Test Setup

```typescript
// src/test-utils.tsx
import { testRender } from "@opentui/solid"

export async function renderForTest(
  Component: () => JSX.Element,
  options = { width: 80, height: 24 }
) {
  return await testRender(Component, options)
}
```

### Test Example

```typescript
// src/components/Counter.test.tsx
import { test, expect } from "bun:test"
import { renderForTest } from "../test-utils"
import { Counter } from "./Counter"

test("Counter renders initial value", async () => {
  const { snapshot } = await renderForTest(() => <Counter initialValue={5} />)
  expect(snapshot()).toContain("Count: 5")
})
```

## Common Configuration Issues

### Missing Preload (Development)

**Symptom**: JSX not transformed, syntax errors in development

**Fix**: Use the `--preload` CLI flag:

```bash
bun --preload=@opentui/solid/preload src/index.tsx
```

### Preload in bunfig.toml (Compiled Binaries)

**Symptom**: Compiled binary fails with `error: preload not found "@opentui/solid/preload"`

**Fix**: Remove preload from bunfig.toml. Instead:

1. Use `--preload` CLI flag for dev/start scripts
2. Pass the solid plugin to `Bun.build` in your build script

### Wrong JSX Settings

**Symptom**: JSX compiles to React calls

**Fix**: Ensure tsconfig has:

```json
{
  "compilerOptions": {
    "jsx": "preserve",
    "jsxImportSource": "@opentui/solid"
  }
}
```

### Build Missing Plugin

**Symptom**: Built output has untransformed JSX

**Fix**: Add Solid plugin to build:

```typescript
import solidPlugin from "@opentui/solid/bun-plugin"

await Bun.build({
  // ...
  plugins: [solidPlugin],
})
```
