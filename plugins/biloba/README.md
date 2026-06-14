# Biloba plugin for Claude Code

Skills that help an AI agent (and you) write fast, stable browser tests with [Biloba](https://onsi.github.io/biloba/) — a Ginkgo/Gomega-native browser-testing framework for Go — **in your own project**.

## Install

The Biloba repo doubles as the marketplace:

```
/plugin marketplace add onsi/biloba
/plugin install biloba@biloba
```

## What you get

All skills are namespaced under `biloba:` and activate when you're working in a Go repo with a Biloba/Ginkgo suite.

| Skill | Use it when |
|---|---|
| `biloba:overview` | You want the mental model — the three principles and how they change the way you write specs (read me first). |
| `biloba:setup` | You're wiring Biloba into a project: `go get`, the bootstrap file, installing `chrome-headless-shell`, the bootstrap variations. |
| `biloba:write-tests` | You're authoring specs: the dual immediate/matcher API, selecting elements, hermetic tests with stubs, multi-tab flows. |
| `biloba:xpath` | You're building an XPath selector with Biloba's `b.XPath()` DSL. |
| `biloba:api` | You need a one-line reference for a Biloba method or matcher. |
| `biloba:explore-unfamiliar-page` | You're writing tests against a page or app you haven't seen — orient first, then draft a spec. |
| `biloba:debug-failures` | A spec failed and you want the DOM outline, a11y tree, and screenshots — and the env knobs that surface them. |

## Versioning

These skills track the Biloba library. Pin to the same Biloba version you've `go get`'d; the narrative docs at <https://onsi.github.io/biloba/> are the source of truth and the API may shift pre-1.0.
