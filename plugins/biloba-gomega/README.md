# Biloba for Gomega

Skills that help an AI agent (and you) write fast, stable browser tests with [Biloba](https://onsi.github.io/biloba/) — a Ginkgo/Gomega-native browser-testing framework for Go — **in your own project**.

## Install

The Biloba repo doubles as the marketplace:

```
/plugin marketplace add onsi/biloba
/plugin install biloba-gomega@biloba
```

## What you get

All skills are namespaced under `biloba-gomega:` and cover Biloba's Go API in Ginkgo/Gomega suites.

| Skill | Use it when |
|---|---|
| `biloba-gomega:overview` | You want the mental model — the three principles and how they change the way you write specs (read me first). |
| `biloba-gomega:setup` | You're wiring Biloba into a project: `go get`, the bootstrap file, installing `chrome-headless-shell`, the bootstrap variations. |
| `biloba-gomega:write-tests` | You're authoring specs: the dual immediate/matcher API, selecting elements, hermetic tests with stubs, multi-tab flows. |
| `biloba-gomega:realistic-mode` | You need realistic interactions — occlusion, CSS `:hover`, drag, scroll, touch — via the `b.Realistic()` track. |
| `biloba-gomega:visual-assertions` | You're asserting that something still *looks* right — `b.HaveScreenshot` against a committed baseline, masking, tolerance, and reading a failed comparison. |
| `biloba-gomega:xpath` | You're building an XPath selector with Biloba's `b.XPath()` DSL. |
| `biloba-gomega:api` | You need a one-line reference for a Biloba method or matcher. |
| `biloba-gomega:explore-unfamiliar-page` | You're writing tests against a page or app you haven't seen — orient first, then draft a spec. |
| `biloba-gomega:debug-failures` | A spec failed and you want the DOM outline, a11y tree, and screenshots — and the env knobs that surface them. |
| `biloba-gomega:flaky-specs` | A spec is flaky, order-dependent, or only fails under `-p`/CI — the single-shot-read and racing-interaction smells, and their polling fixes. |

## Versioning

These skills track the Biloba library. Pin to the same Biloba version you've `go get`'d; the narrative docs at <https://onsi.github.io/biloba/> are the source of truth and the API may shift pre-1.0.
