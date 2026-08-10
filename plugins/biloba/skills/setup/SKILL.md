---
name: setup
description: Wire Biloba into your project's Ginkgo suite — go get, the bootstrap file (SynchronizedBeforeSuite + Prepare), installing chrome-headless-shell, choosing high-fidelity vs the fast headless shell, the three bootstrap variations (shared vs per-process browser, reusable vs fresh tab), window size, the .gitignore split between the throwaway screenshots directory and the committed visual baselines directory, and running the suite. Use when setting up Biloba in a repo or changing the suite-level Chrome lifecycle.
---

# Setting up Biloba in your suite

One-time wiring. Authoring model → `biloba:write-tests`. Mental model → `biloba:overview`. Docs: <https://onsi.github.io/biloba/#getting-started>.

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
- `b.Prepare()` resets `b` between specs (closes other tabs, clears state, navigates to `about:blank`). `OncePerOrdered` keeps it from resetting between `It`s inside an `Ordered` container — which is also why network/dialog handlers **accumulate** inside an `Ordered` block (see `biloba:flaky-specs`).

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

## 4. Suite-level config

`SpinUpChrome(GinkgoT(), ...)`:

- `biloba.HighFidelityHeadless()` — full headless Chrome.
- `biloba.AutoInstallHeadlessShell()` — download the shell if missing.
- `biloba.HeadlessShellPath(path)` — point at a specific shell binary.
- `biloba.StartingWindowSize(w, h)` — default tab size (default `1024x768`); process-wide. Per-spec override: `b.SetWindowSize(w, h)` (self-restoring).
- `biloba.ChromeFlags(...)` — raw `chromedp.ExecAllocatorOption`s (e.g. `chromedp.Flag("headless", false)` to watch).

`ConnectToChrome(GinkgoT(), ...)` carries Biloba-specific config — mostly failure artifacts (outlines, screenshots, inline images) → `biloba:debug-failures`. Under CI or an AI agent, **failure artifacts need zero config**: Biloba auto-detects and emits a DOM outline plus screenshot files on disk.

## 5. Two directories, and your `.gitignore`

Biloba writes into two directories with opposite lifecycles. Get this right up front:

```
# .gitignore
biloba-screenshots/
```

- **`biloba-screenshots/` — ignore it.** Failure screenshots, plus the `.actual.png`/`.diff.png` artifacts a failed visual assertion produces. Throwaway output, regenerated every run. (`BILOBA_SCREENSHOTS_DIR` / `BilobaConfigScreenshotsToDir(dir)` moves it — on CI, point it at a directory you upload as a build artifact.)
- **`biloba-baselines/` — commit it.** The reference PNGs `b.HaveScreenshot` compares against: few, small, reviewed. Only created once you write a visual assertion. (`BILOBA_SCREENSHOT_BASELINES_DIR` / `BilobaConfigScreenshotBaselinesDir(dir)` moves it.) → `biloba:visual-assertions`

Committing the artifacts directory is how a repo's history ends up mostly rejected PNGs; gitignoring the baselines makes every visual assertion fail on a fresh clone.

## 6. Run it

```bash
ginkgo -r -p                 # parallel — Biloba is built for this
ginkgo -r -p -randomize-all  # also enforces spec independence
```

## Next

**Before you write a single spec, load `biloba:write-tests`** — the most consequential authoring decision (CSS hook vs. locator vs. XPath) lives there, and generic-automation muscle memory gets it wrong. Reach for `biloba:api` for exact method/matcher names.
