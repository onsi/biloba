---
name: biloba-testing
description: How to write and run Biloba's own Ginkgo test suite. Use when adding or modifying specs in this repo, asserting that a Biloba call should fail the test, working with the fixture server, or running the suite. Covers the run command, the failure-capturing gt/bilobaT harness, ExpectFailures, fixtures, and spec structure.
---

# Testing Biloba

All tests in this repo are **Ginkgo specs** (Gomega for assertions). There is no `go test`-style table testing here — write `Describe`/`Context`/`It`.

## Running the suite

The `Makefile` wraps the canonical invocations — prefer these:

| Command | What it runs | When |
|---|---|---|
| `make test` | headless (chrome-headless-shell), parallel + randomized | your default, every change |
| `make test-all` | `make test`, then the same suite in full ("new") headless google-chrome (`BILOBA_TEST_HIGH_FIDELITY=true`) | before changes touching tab/Chrome lifecycle — both lanes are what CI runs |
| `make stress-test` | 6 procs under moderate CPU/IO load (`stress`), 41 repeats, generous total budget | **only periodically, or when you suspect a change might be flaky** — it's slow and needs `stress` (`brew install stress`) |

Under the hood `make test` is just `ginkgo -r -p --randomize-all`. `-p` (parallel) is the realistic mode — Biloba is built for it (one shared Chrome, one isolated root tab per process); `--randomize-all` enforces spec independence.

`make stress-test` exists because Biloba's flakes are timing/concurrency races in the Chrome DevTools target lifecycle that a single clean run won't surface. It runs `ginkgo -procs=6 --repeat 40 --timeout=1500s --poll-progress-after=45s` under background `stress` load: the load perturbs scheduling so races show up, `--poll-progress-after` dumps the wedged goroutine stack within 45s of any hang, and the generous `--timeout` is a *total* budget across all repeats (so size it above repeats × per-run, or a healthy run looks like a timeout). Don't run it on every change — reach for it after touching tab create/close, `AllTabs`, `ConnectToChrome`, or anything in the chromedp bridge.

To focus while debugging, run in serial and optionally non-headless/interactive:

```
ginkgo --focus="..."                 # serial, easier to read
BILOBA_INTERACTIVE=true ginkgo       # headed; pauses on failure until ^C (serial, few specs)
```

## Suite setup (`biloba_suite_test.go`)

- A single shared `b *biloba.Biloba` is created in `SynchronizedBeforeSuite` (process 1 runs `SpinUpChrome`, every process runs `ConnectToChrome`).
- `b.Prepare()` runs in a `BeforeEach` decorated `OncePerOrdered` (so it doesn't reset between `It`s inside an `Ordered` container).
- Specs are served HTML fixtures from `./fixtures/*.html` by a `ghttp` server reachable at the package var `fixtureServer`. Add a `.html` file there when you need new DOM to test against.

## Typical spec shape

```go
var _ = Describe("...", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())   // confirm the page is ready before exercising it
	})

	It("does the thing", func() {
		Ω("#hello").Should(b.BeVisible())
	})
})
```

Navigate, then `Eventually(<anchor>).Should(b.Exist())` to gate on readiness, then exercise behavior. `Ω` and `Expect` are interchangeable.

## Poll-by-default changes how you assert failures

**Biloba polls by default.** The fully-applied form of a DOM method (`b.Click("#go")`, `b.GetValue("#x")`) now *waits* — it runs the method's matcher under `Eventually` bound to `gt`. Two consequences for specs:

- **A not-found / not-actionable call no longer fails immediately with a `Failed to <verb>` message.** It polls until a deadline and then surfaces a Gomega **"Timed out after…"** failure that wraps the matcher message (and, for a genuine JS error, the error text inside that wrapper). So drive these specs with a *short* timeout and assert the timeout substring, **not** an exact immediate-fatal string:
  ```go
  b.WithTimeout(time.Millisecond * 60).GetInnerText("#non-existing")
  ExpectFailures(ContainSubstring("Timed out after"))
  ```
- **`b.Immediate()` reproduces the old act-once / fail-fast behavior** (it uses `Expect`, a single evaluation). Reach for it when you want to assert the bare matcher message without waiting out a poll:
  ```go
  Ω(b.Immediate().GetInnerText("#non-existing")).Should(Equal(""))
  ExpectFailures(ContainSubstring(`have property "innerText"`))
  ```

## Asserting that a Biloba call SHOULD fail the spec

This is the non-obvious part. Biloba normally turns errors into Ginkgo failures via `GinkgoT().Fatalf`. In this suite, Biloba is wired to a custom `*bilobaT` (the package var `gt`) that **captures** `Fatal`/`Fatalf` into `gt.failures` instead of aborting the spec. So to test Biloba's own failure behavior:

```go
It("errors when the selector is malformed", func() {
	b.HasElement(b.XPath("//[blarg]"))                       // would normally fail the spec
	ExpectFailures(ContainSubstring("is not a valid XPath expression"))
})
```

- `ExpectFailures(expected ...any)` is **`ConsistOf`-based**: it expects **one matcher per captured failure** (each arg is a Gomega matcher or a string compared with `Equal`), then clears the buffer. To assert **two substrings against a single failure**, pass one `SatisfyAll(...)`, not two args:
  ```go
  b.WithTimeout(time.Millisecond*60).SetValue("#non-existing", "foo")
  ExpectFailures(SatisfyAll(
  	ContainSubstring("Timed out after"),
  	ContainSubstring("could not find DOM element matching selector: #non-existing"),
  ))
  ```
- An `AfterEach` asserts `gt.failures` is empty — **if a spec triggers a Biloba failure and you don't consume it with `ExpectFailures`, the spec fails** with "Did you forget to call ExpectFailures?".

This `gt`/`ExpectFailures` path is also how you assert **hard errors from the four-bucket guards** — e.g. configuring a method that doesn't support a knob (`b.WithPolling(...).Navigate(...)`, `b.Immediate().Count(...)`) or configuring a bare matcher (`b.WithTimeout(d).Click()`). These are `gt.Fatalf` calls, so capture them with `ExpectFailures(ContainSubstring("does not support WithPolling"))` (or `ContainSubstring("returns a matcher")` for the bare-matcher guard).

For **matchers**, you usually don't go through `gt` — call `Match` directly and inspect the returned error:
```go
match, err := b.BeVisible().Match("#non-existing")
Ω(match).Should(BeFalse())
Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
```

### The `FailureMessage` gotcha

gcustom matchers render their template from data populated **during `Match`**. So to assert a matcher's `FailureMessage`, reuse the **same matcher instance** the assertion already `Match`-ed — calling `FailureMessage` on a *fresh* matcher renders against empty data:
```go
m := b.EachBeVisible()
Ω(".non-existing").ShouldNot(m)                 // this Match populates m's template data
Ω(m.FailureMessage(".non-existing")).Should(ContainSubstring("Expected at least one element to match"))
```
(See the `HaveCount`/`EachBeVisible` specs.)

## Spec-authoring idioms (reach for these before `b.Run`)

`b.Run` is the escape hatch; most things people reinvent with it already exist as a matcher that polls cleanly under `Eventually`. Keep `b.Run` for genuinely app-specific state.

- **Counting:** `Eventually(sel).Should(b.HaveCount(7))` (or `b.HaveCount(BeNumerically(">", 10))`) — not `b.Run("...querySelectorAll(sel).length", &n)`.
- **Attributes/properties:** `b.GetAttribute`/`b.GetProperty` (or the `b.HaveAttribute`/`b.HaveProperty` matchers) — not `getAttribute`/property reads in JS.
- **Text:** `b.HaveInnerText`/`b.HaveTextContent`; the ordered text of a group is `Expect(".step").To(b.EachHaveInnerText("Pick", "Pay", "Done"))`. For **negation** ("nothing here says X"), use a text locator + `ShouldNot(b.Exist())`: `Eventually(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())` — not a JS text scan.
- **Dismissing a popover/menu (click-away):** `b.Click(sel, b.At(x, y))` is the blessed idiom — target a background region and offset onto the backdrop: `b.Click("body", b.At(5, 5))`.

**Never put a side effect in an `Eventually`/`Consistently` body** — the body re-runs every poll, so a `b.Click` inside it rapid-fires clicks before state settles (a real footgun for cycling controls like a 3-way toggle). The body must be idempotent. To drive a cycling control to a target state, click *once* then wait for the change before reconsidering:

```go
for b.GetAttribute("html", "data-theme") != "dark" {
    before := b.GetAttribute("html", "data-theme")
    b.Click("#theme-toggle")
    Eventually(func() any { return b.GetAttribute("html", "data-theme") }).ShouldNot(Equal(before))
}
```

## Other conventions

- Label a spec `no-browser` to skip the `b.Prepare()` in `BeforeEach` (used for specs that don't drive the browser).
- Put new specs in the `*_test.go` file matching the source file (`dom.go` → `dom_test.go`, etc.).
- `console.log`/`console.assert` from the page stream to the `GinkgoWriter`; a failing `console.assert` counts as a spec failure.
