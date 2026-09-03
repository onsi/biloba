# Biloba for Vitest

Skills that help an AI agent write fast, stable browser tests with Biloba's TypeScript client in a `vitest` suite.

## Install

The Biloba repo doubles as the marketplace:

```
/plugin marketplace add onsi/biloba
/plugin install biloba-vitest@biloba
```

## What you get

All skills are namespaced under `biloba-vitest:` and use the TypeScript client API.

| Skill | Use it when |
|---|---|
| `biloba-vitest:overview` | You want the shared-Chrome/daemon/session mental model and routing to the other skills. |
| `biloba-vitest:setup` | You're wiring `bilobad`, Vitest global setup, one daemon per worker, and reusable sessions. |
| `biloba-vitest:write-tests` | You're authoring tests with locators, polling actions/assertions, tabs, frames, network control, and realistic input. |
| `biloba-vitest:visual-assertions` | You're creating or diagnosing screenshot baselines, masks, tolerances, and color schemes. |
| `biloba-vitest:debug-failures` | A test failed and you need `BilobaError`, trajectories, context-wide artifacts, console output, or crash codes. |
| `biloba-vitest:flaky-tests` | A test is intermittent, load-sensitive, order-dependent, or contains redundant client-side polling. |

## Versioning

These skills track the TypeScript client in the same Biloba release. Pin the plugin and client to the same version. The client is still a prototype, and its API may move before 1.0. The narrative docs live at <https://onsi.github.io/biloba/vitest.html>.
