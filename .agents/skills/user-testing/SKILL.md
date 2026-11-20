---
name: user-testing
description: Validate a running app as an end user against a flow or acceptance criteria, capture evidence, and report reproducible findings and blockers. Use when asked to user-test, QA, click through, smoke-test, or verify a feature, fix, or PR in the actual UI of a web or mobile app, even if they only say "check that it works" or "try it as a user". Not for writing or running automated test suites, or for reviewing source code.
---

# User testing

Validate the experience a user gets, not whether controls respond. Observed
behavior is the result; source code is context at most and is never a
substitute for running the flow.

## Pick the interface reference

Read one before acting, by where the target runs:

- In a browser (web app, local dev server, browser extension page): read [references/web.md](references/web.md).
- On a phone or tablet, simulator, or emulator (iOS, Android): read [references/mobile.md](references/mobile.md).

Neither fits (desktop app, terminal UI): apply this file's method with whatever
automation the session has, and say which interface reference was missing.

If the interface needs a tool the session doesn't have, report that blocker.
Do not fall back to reading code and calling the flow tested.

## Plan

Establish from the brief: the environment and URL or app build, the
authorized role or account, the flow, the acceptance criteria, and the test
data. Do not ask again for anything the brief already gives. If something
essential is missing, ask for only that and continue independent checks
where safe.

Write down the starting state, the meaningful transitions, and the expected
final outcome before driving the UI. Cover the intended path, then the
validation, empty, error, and recovery states that are in scope.

## Test like a user

- Drive the real UI and check visible outcomes against each criterion.
- A click, a request, or a route change is not a working workflow. Confirm the intended record, role, destination, and saved state, and inspect persisted results when the task warrants it.
- Re-read the screen after every action that changes it; treat an action as unconfirmed until the new state shows the expected result.
- Separate measured facts (sizes, colors, error text) from visual judgments.
- Check diagnostics (console, network, device logs) when the interface exposes them. Mark diagnostics you couldn't inspect as unavailable; never imply an unperformed check passed.
- When retrying a blocked flow, continue from the earlier attempt and keep new evidence distinct from old.

## Evidence

- Capture the start state, key transitions (forms, modals, validation, loading), the final state, and any error or blocker when it appears.
- Keep the subject of each capture inside the frame with enough surrounding context to read it. Avoid redundant images.
- Save to the caller's evidence folder, else `~/screenshots/<flow-name>/`, with numbered filenames. Keep earlier captures on retries.
- Cite real saved paths or inline captures with one-line captions. If captures are inline only, say so; never invent paths.
- Claim video only if a recorder was actually used. A screenshot sequence is not a video.

## Boundaries

- Treat accounts and data as real. Stay within the authorized environment, role, and test records. Do not sign in, sign out, or switch accounts unless the brief authorizes it.
- Perform only the mutations the flow needs. Ask before destructive, external, or shared-data actions outside it. Never touch production to validate a local build.
- Do not edit source or config, run builds, migrations, or automated suites, or start, stop, or clean up services unless asked. Saving evidence is allowed.
- Never stop processes by name or pattern. If cleanup is authorized, act only on exact resources this task created.
- Preserve unrelated tabs, drafts, records, and work. Restore temporary changes (viewport, device settings, test state) when safe, and disclose what remains changed.

## Gotchas

- A setup failure (server down, missing seed data, expired login) is not a product defect. Report it as a blocker unless evidence ties it to the change under test.
- An empty or stale screen right after an action usually means the read happened too early or hit the wrong target. Re-read before concluding the app failed.
- "Looks right" in a screenshot can hide a wrong record or role. Check the identifying data on screen, not just the layout.

## Report

```markdown
**Flow tested:** <environment, role, records, criteria exercised>
**Result:** pass | fail | partial — <what passed; what was blocked or untested>

**Findings**
1. <title> — <severity/impact>
   - Steps: <numbered, reproducible>
   - Expected: <…>  Actual: <…>
   - Evidence: <path or inline capture>, <measurements or diagnostic errors>

**Not checked / blockers:** <missing access, data, tools, diagnostics>
**Suggested regression coverage:** <behavioral steps for meaningful defects; don't write or run tests>
**Remaining state changes:** <test data, viewport, device settings left altered, or "none">
```

Avoid speculative defects. If everything passed, say so plainly and list what
was and wasn't covered.
