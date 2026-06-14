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

## Asserting that a Biloba call SHOULD fail the spec

This is the non-obvious part. Biloba normally turns errors into Ginkgo failures via `GinkgoT().Fatalf`. In this suite, Biloba is wired to a custom `*bilobaT` (the package var `gt`) that **captures** `Fatal`/`Fatalf` into `gt.failures` instead of aborting the spec. So to test Biloba's own failure behavior:

```go
It("errors when the selector is malformed", func() {
	b.HasElement(b.XPath("//[blarg]"))                       // would normally fail the spec
	ExpectFailures(ContainSubstring("is not a valid XPath expression"))
})
```

- `ExpectFailures(expected ...any)` asserts the captured failures match (each arg is a Gomega matcher or a string compared with `Equal`) and then clears the buffer.
- An `AfterEach` asserts `gt.failures` is empty — **if a spec triggers a Biloba failure and you don't consume it with `ExpectFailures`, the spec fails** with "Did you forget to call ExpectFailures?".

For **matchers**, you usually don't go through `gt` — call `Match` directly and inspect the returned error:
```go
match, err := b.BeVisible().Match("#non-existing")
Ω(match).Should(BeFalse())
Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
```

You can also assert exact failure-message text for matchers via `matcher.FailureMessage(actual)` (see `HaveCount` specs).

## Other conventions

- Label a spec `no-browser` to skip the `b.Prepare()` in `BeforeEach` (used for specs that don't drive the browser).
- Put new specs in the `*_test.go` file matching the source file (`dom.go` → `dom_test.go`, etc.).
- `console.log`/`console.assert` from the page stream to the `GinkgoWriter`; a failing `console.assert` counts as a spec failure.
