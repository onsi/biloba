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
| `make driver-test` | the Go driver packages (`./engine ./protocol ./cmd/bilobad`, including the generated-protocol and biloba.js drift specs) and the TypeScript unit suites | when touching `engine/`, `protocol/`, `cmd/bilobad/`, or `typescript/` |
| `make driver-parity` | the Go/TypeScript parity contract: a real `bilobad` driving a real shared Chrome, asserted against the same fixture from both languages | same, plus before trusting a new vertical slice |
| `make driver-e2e` | the shipping topology: three vitest worker *processes*, one `bilobad` each, one shared Chrome | before anything touching session isolation, daemon lifecycle, or shared-browser attach/detach |
| `make check-plugins` | both distributed plugin manifests, versions, namespaces, skill names, and client-separation rules | whenever changing `plugins/`, marketplace metadata, docs links, or the release version |

Under the hood `make test` is just `ginkgo -r -p --randomize-all`. `-p` (parallel) is the realistic mode — Biloba is built for it (one shared Chrome, one isolated root tab per process); `--randomize-all` enforces spec independence.

`make stress-test` exists because Biloba's flakes are timing/concurrency races in the Chrome DevTools target lifecycle that a single clean run won't surface. It runs `ginkgo -procs=6 --repeat 40 --timeout=1500s --poll-progress-after=45s` under background `stress` load: the load perturbs scheduling so races show up, `--poll-progress-after` dumps the wedged goroutine stack within 45s of any hang, and the generous `--timeout` is a *total* budget across all repeats (so size it above repeats × per-run, or a healthy run looks like a timeout). Don't run it on every change — reach for it after touching tab create/close, `AllTabs`, `ConnectToChrome`, or anything in the chromedp bridge.

To focus while debugging, run in serial and optionally non-headless/interactive:

```
ginkgo --focus="..."                 # serial, easier to read
BILOBA_INTERACTIVE=true ginkgo       # headed; pauses on failure until ^C (serial, few specs)
```

### The driver lanes, and why they refuse to skip

CI runs every one of these (`.github/workflows/test.yml`). Two rules keep them honest:

- **The engine suite fails rather than skips when it cannot find Chrome.** It resolves the binary
  through `engine.LocateChrome("")` — the same search the Go suite and `bilobad` use (explicit path
  → `BILOBA_CHROME_HEADLESS_SHELL` → `$PATH` → the puppeteer/Biloba caches). It is the only guard
  that `engine/biloba.js` still matches the canonical `biloba.js`, so a quiet skip there means the
  copy can drift unnoticed. If it fails, run `make update-chrome`.
- **`typescript/test/parity.test.ts` fails rather than skips when `BILOBA_DAEMON_EXECUTABLE` is
  unset.** It is the only test that exercises the real daemon end to end; "4 skipped, exit 0" is
  indistinguishable from "4 passed" at a glance. Run it via `make driver-parity` (which builds the
  daemon first). To opt out on purpose, set `BILOBA_SKIP_PARITY=true`.

`bilobad` resolves Chrome itself, so neither lane needs a browser environment variable.

`make driver-test` deliberately does *not* run `go generate ./engine` — regenerating
`engine/biloba.js` there would silently repair the drift the engine suite exists to catch. Run
`go generate ./engine` yourself after editing `biloba.js`.

### The e2e suite tests the topology, not an approximation of it

`typescript/test/e2e/` is the only place the shipping arrangement — N worker processes, one daemon
each, one shared Chrome — exists as such. `globalSetup` starts the single Chrome in the main process
and provides its websocket URL; each test *file* becomes its own forked worker with its own daemon.

The `rendezvous()` barriers in `test/e2e/harness.ts` are load-bearing twice over. They order the
workers so an assertion happens while the others are demonstrably live, **and** they are the proof
that the workers are concurrent processes at all: run the files serially and the first one blocks
until it times out. A green run is therefore evidence of the topology. If you add a worker file,
update the expected count in every `rendezvous()` call — a stale count makes the barrier release
early and quietly stops proving anything.

`crash.e2e.test.ts` runs in a second vitest invocation with a Chrome of its own, because it kills
things on purpose. Its specs run in a deliberate order — the recoverable crashes first, the terminal
one (killing Chrome) last — and vitest runs `it`s in source order, so that ordering is local and
explicit rather than a property of how files happen to get scheduled.

Every case in it pins a *diagnosis*. A crash reported as an assertion timeout is worse than useless:
it says the page never reached the expected state, sending the reader to debug a test that was fine.
The four covered: a crashed renderer (`PAGE_CRASHED`, recoverable by navigating), a dead browser
(`BROWSER_GONE`), a worker killed without teardown (its daemon reaps itself, Chrome and the other
workers are untouched), and Chrome unreachable at daemon startup (`DRIVER_CLOSED` with the daemon's
stderr attached).

Two of those carry timing bounds, and the bounds are the point rather than decoration: CDP does not
*fail* calls against a dead renderer or a dead browser, it stops answering them, so both used to sit
until their deadline and then report a timeout. Loosening those bounds would let the old behaviour
back in silently.

The isolation assertion is self-evidencing for the same reason: all three workers set
`owner=<name>` on the *same* origin, so three different values coexisting is exactly what browser-
context isolation means. If it broke, they would see each other's.

### Two guards on the wire protocol

`protocol/` is the Go↔TypeScript boundary, and it has a guard on each side that you should extend
rather than work around:

- `protocol/encoding_test.go` marshals every response type and compares the keys that actually
  appear against the keys the **generated** TypeScript declares required. This is what catches
  `omitempty` on a struct (a no-op in Go: the field ships on every response while TypeScript calls
  it optional — make the Go field a pointer). When you add a wire field, add its type to the table.
- `typescript/test/protocol-golden.test.ts` asserts the goldens with `toEqual`, not
  `toMatchObject`, so a field the daemon starts or stops emitting is a failure rather than a
  silently-ignored extra key.

`protocol/generated_test.go` is the drift guard for the generated TypeScript and golden frames: it
renders them from the Go wire structs and compares against what is on disk. It deliberately does not
shell out to git — "does the tree match the last commit?" is the wrong question (it passes a staged
divergent file and fails a working tree you just regenerated correctly), so nothing here needs you to
commit before the check goes green. After editing the wire structs, run `go generate ./protocol` and
commit the generated files alongside the change.

Two conventions the wire specs enforce, worth knowing before you add a method:

- **A bad request costs the request, not the daemon.** One worker's daemon owns every session that
  worker is driving, so an unknown method, mis-shaped params, a duplicate in-flight id, a zero id, or
  an unparseable frame body are all answered as errors while the loop keeps going. Only a genuinely
  desynced stream (short read, bad length prefix) ends it. `protocol/stdio_test.go` pins each case
  with a `stillServing()` call afterwards — follow that shape.
- **A stub daemon and the real one answer to one contract.** `typescript/test/support/assertions.ts`
  holds the invariants a timed-out operation owes; both `client.test.ts` (fast, stub) and
  `parity.test.ts` (slow, real `bilobad`) assert through it. Generated types constrain the *shape* a
  stub can send, but nothing constrains its *behaviour* — a stub is free to claim a failed click
  succeeded. Sharing the assertion is what stops that: it caught the stub reporting a timed-out
  click with an empty trajectory, which the real daemon cannot produce. Put invariants there and
  payload specifics at the call site.
- **Some failures are not worth retrying.** `engine.Poll` stops immediately on a fatal attempt
  error — the engine's runner-neutral answer to `gomega.StopTrying`, which the Go API already uses
  for the same reason (`capture.go`, `visual.go`, `cookie_matchers.go`: a decode into the wrong
  pointer type, a missing baseline, a selector that will never parse). Mark one with
  `engine.Fatal(err)`, or give an `engine.Error` a code whose `Fatal()` is true. Getting this wrong
  is expensive in a specific way: the poll burns its whole budget and then reports a *timeout*,
  attributing to the page a failure that was never about the page. The line to walk: a
  `ReferenceError` or `TypeError` is the ordinary shape of "not there yet" (biloba.js not installed,
  an element not rendered) and must stay retryable; a `SyntaxError` cannot start parsing later, so it
  is fatal. Same for a malformed *expectation* — invalid `expectedJson` is the caller's bug, not a
  page that has not settled.
- **Meaning goes on the wire, not into inference.** `evaluate` carries an explicit `invoke` flag
  rather than guessing from whether the argument array is empty. If you find yourself deriving one
  field's meaning from another's emptiness, add the field instead.

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

**Every test in this repo is a Ginkgo spec — always.** That includes the internal ones in package `biloba` (the ones that reflect over unexported fields or exercise a pure function). Import Ginkgo and Gomega under their **package names** rather than dot-importing, which is all it takes to avoid colliding with biloba's own exported names: `ginkgo "github.com/onsi/ginkgo/v2"` / `gomega "github.com/onsi/gomega"`, then `ginkgo.Describe`/`ginkgo.It`/`gomega.Expect`. Both test packages compile into one binary, so specs registered from package `biloba` run under the same `RunSpecs`. Label them `no-browser` if they don't need a tab. `tab_state_internal_test.go` is the model.

### Half-coverage of a two-sided API reads as coverage

The worst spec in this repo's history was correct, exercised the real API, asserted a true thing, and stayed green through a nine-site bug:

```go
It("fails the spec with a directive message when Await times out", func() {
    b.WithTimeout(60 * time.Millisecond).HoldResponse(ContainSubstring("/api/never-requested")).Await()
    ExpectFailures(ContainSubstring("Timed out after 60ms waiting for HoldResponse to intercept..."))
})
```

`b.WithTimeout(d).HoldResponse(url)` composes two things: the knob has to reach `Await`, **and** the handler has to register on the tab. This asserts the first. The URL is never requested, so the second cannot be exercised — and `0 matching response(s) have been intercepted so far` sits in the expected output as a *success* condition. Registration through a view was broken the whole time and this spec could not have noticed.

The smell: **an assertion on a timeout, a status, an empty tally, or an error path, where the happy path it implies is never actually taken.** Ask of any zero in an expected output — *could this ever have been non-zero in this spec?* If not, the spec is asserting its fixture rather than the code.

The fix is a **second spec from the opposite direction**, not a bigger assertion on the first: one spec proves the deadline is honoured, another proves the handler fires. Widening the original would only have made a one-sided spec longer.

Watch for it wherever a spec composes a *view* (`Realistic()`, `WithTimeout(d)`, `WithPolling(d)`, `Immediate()`) with a method that registers or mutates state — that composition has two halves by construction.

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
- **Text:** `b.HaveInnerText`/`b.HaveTextContent`; the ordered text of a group is `Expect(".step").To(b.EachHaveInnerText("Pick", "Pay", "Done"))`. For **negation** ("nothing here says X"), use a text locator + `ShouldNot(b.Exist())` — but **anchor the scope first**, because a `Within`/`Containing` whose scope doesn't resolve matches *nothing*, so the negation passes instantly and permanently against a page that never rendered the scope:
  ```go
  Eventually("#published-list").Should(b.Exist())                                   // the scope is real…
  Eventually(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist()) // …so this means something
  ```
- **Capturing a value you also assert on:** `Eventually(sel).Should(b.HaveAttribute("data-id", Not(BeEmpty())).Capture(&id))` — one read. Asserting and *then* calling the getter is two reads of a page that may have changed in between. Under `ShouldNot` nothing is captured (by design), so a spec that needs the value uses a `Should`.
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
- **Source-level guard specs** live in `*_internal_test.go` (package `biloba`, labelled `no-browser`) and grep Biloba's own `*.go` for a class the compiler and `go vet` can't see: `format_verbs_internal_test.go` (a `%w` outside `fmt.Errorf`), `tab_state_internal_test.go` (a `Biloba` field a view copies rather than shares), `cdp_internal_test.go` (a `chromedp.Run(b.Context, ...)` with no deadline). When one fires, fix the code — reaching for the regex is only right if the exempt form is genuinely correct, and then say why in the failure message.
- **Simulating a fault beats reproducing one** when the real thing is slow or unrecoverable. The seams live in `export_test.go` and follow one shape — set, return a restore func, `DeferCleanup` it: `SetNavigationTimeoutForTest`, `SetTransientReadErrorForTest`, `SetCDPTimeoutsForTest` (shrinks the CDP backstop so a wedge spec runs in milliseconds), `SetWedgedCDPForTest` (a Chrome that accepts a command and never answers — a real wedged renderer would peg a CPU for the rest of the run). Reach for real Chrome when the fault is recoverable: `cdp_test.go` crashes a spawned tab with `page.Crash()` and navigates it back.
