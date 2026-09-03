---
name: setup
description: Wire Biloba into a Go Ginkgo/Gomega suite — go get, the bootstrap file (SynchronizedBeforeSuite + Prepare), chrome-headless-shell installation, high-fidelity vs fast headless modes, shared vs per-process browsers, reusable vs fresh tabs, window size, screenshot and baseline directories, and running the suite. Use when setting up Biloba's Go API or changing its suite-level Chrome lifecycle.
---

# Setting up Biloba in your suite

One-time wiring for a **Go/Ginkgo** suite. Authoring model → `write-tests`. Mental model → `overview`. Docs: <https://onsi.github.io/biloba/#getting-started>.

## 1. Add Biloba and bootstrap a suite

```bash
go get github.com/onsi/biloba
mkdir browser && cd browser
ginkgo bootstrap
```

Edit the generated `*_suite_test.go`:

```go
package browser_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/biloba"
)

func TestBrowser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Browser Suite")
}

var b *biloba.Biloba

var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
}, func() {
	b = biloba.ConnectToChrome(GinkgoT())
})

var _ = BeforeEach(func() {
	b.Prepare()
}, OncePerOrdered)
```

- `GinkgoT()` is the seam: Chrome errors become suite failures.
- `SpinUpChrome` runs **once** (process 1) and writes connection info to disk; `ConnectToChrome` runs on **every** parallel process and opens that process's reusable root tab `b`.
- `b.Prepare()` resets `b` between specs (closes other tabs, clears state, navigates to `about:blank`). `OncePerOrdered` keeps it from resetting between `It`s inside an `Ordered` container — which is also why network/dialog handlers **accumulate** inside an `Ordered` block (see `flaky-specs`).

## 2. Get `chrome-headless-shell`

`SpinUpChrome` drives **`chrome-headless-shell`** by default — the lightweight headless build that is ~an order of magnitude faster and lets one Chrome process drive many isolated contexts in parallel. It's a standalone binary, separate from your Chrome install. Lookup order:

1. `biloba.HeadlessShellPath("/path/to/chrome-headless-shell")`
2. `BILOBA_CHROME_HEADLESS_SHELL` env var
3. your `PATH`
4. the `@puppeteer/browsers` and Biloba download caches

If none turn it up Biloba **fails fast with instructions** — it never silently downloads. Install it once:

```bash
npx @puppeteer/browsers install chrome-headless-shell@stable
```

Or let Biloba download+cache it on first use (opt-in, since it reaches the network):

```go
biloba.SpinUpChrome(GinkgoT(), biloba.AutoInstallHeadlessShell())
```

**Full-browser realism:** `biloba.SpinUpChrome(GinkgoT(), biloba.HighFidelityHeadless())` runs the full ("new") headless Chrome — pixel-accurate, extensions — but markedly slower, and it serializes parallel work. Keep the bulk of the suite on the shell and run a focused high-fidelity lane where it earns its keep.

## 3. Choose a bootstrap variation

Trade isolation against performance. All three are small edits — try them on your suite.

**Default — shared browser, reused root tab** (most performant, good-enough isolation): the snippet in step 1.

**Per-process browser** (stronger isolation, slower startup):

```go
var _ = BeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
	b = biloba.ConnectToChrome(GinkgoT())
})
var _ = BeforeEach(func() { b.Prepare() }, OncePerOrdered)
```

**Fresh tab per spec** (per-spec cleanup, a per-spec cost):

```go
var rootB *biloba.Biloba
var b *biloba.Biloba

var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
}, func() {
	rootB = biloba.ConnectToChrome(GinkgoT())
})
var _ = BeforeEach(func() {
	rootB.Prepare()
	b = rootB.NewTab()
}, OncePerOrdered)
```

Per-process browsers and fresh-tab-per-spec can be combined.

**Serve the app from a stable origin — the same trade, one level out.** The Go reflex is one `httptest.NewServer` per spec, which puts every spec on a new ephemeral port: a new origin, every asset URL changed, the renderer cold on every navigation. Measured on one real suite (1.67 MB bundle): **~31ms extra per `b.Navigate`, ~20s off a 1,558-spec `--procs=6` run.** It is not the HTTP cache — disabling that on a stable origin costs nothing, and the recovered time lands in *script* time. Small static fixtures will see much less.

You give up no isolation to get it: a brand-new server with brand-new state, bound to the **same port**, is as fast as re-navigating the warm one. Nothing but the origin has to be shared.

Get the stable port the way Ginkgo shards any per-process resource — a shared baseline plus the process index, **not** an ephemeral port (→ `ginkgo:parallelism`):

```go
port := 4000 + GinkgoParallelProcess()   // 4001, 4002, ... one per shard, same every run
l, _ := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
server := httptest.NewUnstartedServer(handler)
server.Listener.Close()
server.Listener = l
server.Start()
```

Then pick one of two shapes deliberately — different trades, not better and worse:

- **Keep one server up per process and reset its state per spec.** Start in `SynchronizedBeforeSuite`'s second function with `DeferCleanup`, clear state in a `BeforeEach`. Suits a server whose state is handed back cheaply — in-memory store, stub registry, seeded fixtures. (Biloba's own suite runs one fixture server per process; static files, nothing to reset.)
- **Bounce the server between specs over the same port.** Suits a server whose per-spec isolation *is* a resource it holds for its lifetime (a `GinkgoT().TempDir()` store), where resetting would mean re-pointing a live server at a new root. Also buys a durability check the other shape can't: restart over the *same* directory and make the new process re-read from disk what the old one wrote.

**If you pin a port, give each fixture its own `&http.Transport{}`.** A zero-value `http.Client` uses `http.DefaultTransport`, whose pool is process-global and keyed on `host:port` — so a spec's first request can get a keep-alive socket to the *previous* spec's closed server, surfacing as an `EOF` out of a `BeforeEach` that points at nothing. `CloseIdleConnections()` on teardown. Measured on the suite that hit it: one fire in nine full-suite runs before the fix, none in sixty after. Only the pinned-port shape has this problem.

## 4. Suite-level config

`SpinUpChrome(GinkgoT(), ...)`:

- `biloba.HighFidelityHeadless()` — full headless Chrome.
- `biloba.AutoInstallHeadlessShell()` — download the shell if missing.
- `biloba.HeadlessShellPath(path)` — point at a specific shell binary.
- `biloba.StartingWindowSize(w, h)` — default tab size (default `1024x768`); process-wide. Per-spec override: `b.SetWindowSize(w, h)` (self-restoring).
- `biloba.ChromeFlags(...)` — raw `chromedp.ExecAllocatorOption`s (e.g. `chromedp.Flag("headless", false)` to watch).

`ConnectToChrome(GinkgoT(), ...)` carries Biloba-specific config — mostly failure artifacts (outlines, screenshots, inline images) → `debug-failures`. Under CI or an AI agent, **failure artifacts need zero config**: Biloba auto-detects and emits a DOM outline plus screenshot files on disk.

## 5. Two directories, and your `.gitignore`

Biloba writes into two directories with opposite lifecycles. Get this right up front:

```
# .gitignore
biloba-screenshots/
```

- **`biloba-screenshots/` — ignore it.** Failure screenshots, plus the `.actual.png`/`.diff.png` artifacts a failed visual assertion produces. Throwaway output, regenerated every run. (`BILOBA_SCREENSHOTS_DIR` / `BilobaConfigScreenshotsToDir(dir)` moves it — on CI, point it at a directory you upload as a build artifact.)
- **`biloba-baselines/` — commit it.** The reference PNGs `b.HaveScreenshot` compares against: few, small, reviewed. Only created once you write a visual assertion. (`BILOBA_SCREENSHOT_BASELINES_DIR` / `BilobaConfigScreenshotBaselinesDir(dir)` moves it.) → `visual-assertions`

Committing the artifacts directory is how a repo's history ends up mostly rejected PNGs; gitignoring the baselines makes every visual assertion fail on a fresh clone.

## 6. Run it

```bash
ginkgo -r -p                 # parallel — Biloba is built for this
ginkgo -r -p -randomize-all  # also enforces spec independence
```

## Next

**Before you write a single spec, load `write-tests`** — the most consequential authoring decision (CSS hook vs. locator vs. XPath) lives there, and generic-automation muscle memory gets it wrong. Reach for `api` for exact method/matcher names.
