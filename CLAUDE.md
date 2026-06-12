# Biloba

Biloba is a browser-testing framework for Go. It builds on [chromedp](https://github.com/chromedp/chromedp) (and the Chrome DevTools Protocol) to bring stable, performant, automated browser testing to [Ginkgo](https://github.com/onsi/ginkgo) and [Gomega](https://github.com/onsi/gomega). It is authored by the same person who wrote Ginkgo and Gomega, and it is unapologetically Ginkgo/Gomega-native.

Pre-1.0: the public API may shift. Read `docs/index.md` for the canonical narrative documentation — it is the source of truth for mental models and intended usage.

## The three principles

Every design decision traces back to one of these. When in doubt, weigh changes against them:

1. **Performance via parallelization.** One shared Chrome process; each Ginkgo parallel process drives its own isolated *root tab* (`b`), reused between specs via `b.Prepare()`. Creating tabs is cheaper than creating browsers; reusing a tab is cheaper than creating one. The payoff is real — the suite runs in ~2s parallel vs ~10s serial.

2. **Stability via pragmatism.** Biloba favors a good-enough simulation over realistic emulation. A click is `element.click()` after synchronous, atomic visibility/enabled checks run *in the browser* — not scroll-into-view + compute centroid + dispatch mouse events across multiple async round-trips. Atomicity in single-threaded JS is what kills flakiness. We knowingly trade away a class of realism bugs (occlusion, scroll) for speed and stability, and tell users they can drop to `chromedp` when they want the realistic path.

3. **Conciseness via Ginkgo and Gomega.** Biloba does not try to be a standalone library or work outside Ginkgo. Errors become test failures (most methods don't return errors). It hooks Ginkgo for screenshots-on-failure and progress reports, and streams `console.log` to the `GinkgoWriter`.

## Architecture

- **Go ⇄ browser bridge.** `biloba.js` installs a global `window._biloba` object on every page load. It exposes synchronous, atomic primitives (`exists`, `click`, `isVisible`, `setValue`, …). Go methods call them via `runBilobaHandler(name, selector, args...)` in `dom.go`, which JSON round-trips through `b.JSFunc("_biloba." + name).Invoke(...)`. **DOM interaction logic lives in JS; Go wraps it.** When you change behavior, the JS and Go sides move together.
- **Selectors.** A `selector any` is either a CSS string, or an `XPath` (a `string` type built by the `b.XPath()` DSL in `xpath.go`). The bridge prefixes `s` for CSS and `x` for XPath. Never fetch-then-act on an element handle — always pass a selector into the action so the find-and-act happens atomically in one JS snippet.
- **Tabs.** `b` is the reusable root tab (never closed). `b.NewTab()` makes an isolated tab (its own `BrowserContextID`, i.e. incognito-like). Spawned tabs inherit their opener's context. `b.Prepare()` closes everything but the root and resets state.
- **chromedp escape hatch.** Every tab exposes `b.Context`; Biloba deliberately does not hide chromedp/cdproto. Missing features (e.g. cookies) are expected to be done through `b.Context` until/unless Biloba grows native support.

## The dual immediate/matcher API convention

This is the single most important pattern to preserve. Many DOM methods have **two forms** keyed on argument count:

- **Fully-applied → acts immediately, fails the test on error.** `b.Click("#go")`, `b.SetValue("#x", 3)`, `b.GetProperty(sel, "href")`.
- **Under-applied → returns a Gomega matcher you poll.** `Eventually("#go").Should(b.Click())`, `Eventually("#x").Should(b.SetValue(3))`.

Immediate methods call `b.gt.Helper()` then `b.gt.Fatalf(...)` on error. Matchers are built with `gcustom.MakeMatcher` and return `(bool, error)` (commonly via `bilobaJSResponse.MatcherResult()`), using `.WithMessage`/`.WithTemplate` for failure output. **Biloba itself never polls** — it returns matchers and lets the user wrap them in `Eventually`/`Consistently`.

Also common: a `Foo`/`HaveFoo`/`EachHaveFoo` family — `Foo` acts on the **first** match, `FooForEach`/`EachHaveFoo` act on **all** matches (returning/asserting slices, empty when nothing matches). The "first vs. all" distinction is conveyed by the method name.

## Testing

**All tests are Ginkgo specs. Run them with:**

```
ginkgo -r -p -randomize-all
```

Repo-specific testing conventions (see `biloba_suite_test.go`):
- A shared `b` is set up in `SynchronizedBeforeSuite` (spin up Chrome on process 1, connect on all). `b.Prepare()` runs in a `BeforeEach` with `OncePerOrdered`.
- Specs serve HTML from `./fixtures/*.html` via a `ghttp` server at `fixtureServer`. Add a fixture file when you need new DOM to test against.
- Biloba is wired to a custom `*bilobaT` (`gt`) that **captures** `Fatal`/`Fatalf` into `gt.failures` instead of aborting. To assert that a Biloba call *should* fail the test, call it and then `ExpectFailures(<string or matcher>...)`. An `AfterEach` guards that every expected failure was asserted.
- Use the `no-browser` label for specs that must skip `b.Prepare()`.
- Typical spec shape: `b.Navigate(fixtureServer + "/dom.html")`, then `Eventually("#anchor").Should(b.Exist())` to confirm the page is ready, then exercise behavior.

## Where things live

| Concern | Go | Test |
|---|---|---|
| Setup, config, Chrome lifecycle | `biloba.go` | `biloba_suite_test.go` |
| DOM query/interaction methods & matchers | `dom.go` | `dom_test.go` |
| Property get/set/match | `properties.go` | `properties_test.go` |
| XPath DSL | `xpath.go` | `xpath_test.go` |
| Tabs / spawned tabs | `tabs.go` | `tabs_test.go` |
| Dialogs | `dialog_handling.go` | `dialog_handling_test.go` |
| Downloads | `downloads.go` | `downloads_test.go` |
| Arbitrary JS (`Run`, `JSFunc`, `JSVar`, `EvaluateTo`) | `javascript.go` | `javascript_test.go` |
| Navigation, logging, screenshots, window size | `navigation.go`, `logging.go`, `screenshots.go`, `windows.go` | `*_test.go` |
| Browser-side primitives | `biloba.js` | (exercised via Go tests) |

`TODO` tracks the backlog (items tagged `@B` Biloba, `@G` Ginkgo, `@Ω` Gomega).

## Conventions

- Keep docs in sync: user-facing behavior changes belong in `docs/index.md` (narrative) and godoc comments (sparse reference that links to the docs). Bump `BILOBA_VERSION` in `biloba.go` and update `CHANGELOG.md` on release.
- Match the surrounding style: terse godoc on exported symbols ending with a link to the docs section; JS in `biloba.js` is dense and functional (`one(...)`/`each(...)` combinators) — follow it.
