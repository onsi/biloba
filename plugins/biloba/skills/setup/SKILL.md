---
name: setup
description: Wire Biloba into your project's Ginkgo suite — go get, the bootstrap file (SynchronizedBeforeSuite + Prepare), installing chrome-headless-shell, choosing high-fidelity vs the fast headless shell, the three bootstrap variations (shared vs per-process browser, reusable vs fresh tab), window size, and running the suite. Use when setting up Biloba in a repo or changing the suite-level Chrome lifecycle.
---

# Setting up Biloba in your suite

This is the one-time wiring. For the authoring model see `biloba:write-tests`; for the mental model see `biloba:overview`. Docs: <https://onsi.github.io/biloba/#getting-started>.

## 1. Add Biloba and bootstrap a suite

```bash
go get github.com/onsi/biloba
mkdir browser && cd browser
ginkgo bootstrap
```

Then edit the generated `*_suite_test.go` to wire in Biloba:

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
- `b.Prepare()` resets `b` between specs (closes other tabs, clears state, navigates to `about:blank`). `OncePerOrdered` keeps it from resetting between `It`s inside an `Ordered` container.

## 2. Get `chrome-headless-shell`

By default `SpinUpChrome` drives **`chrome-headless-shell`** — the lightweight headless build that is ~an order of magnitude faster and lets one Chrome process drive many isolated contexts in parallel (see `#headless-fidelity`). It's a standalone binary, separate from your Chrome install. Biloba looks for it in this order:

1. `biloba.HeadlessShellPath("/path/to/chrome-headless-shell")`
2. `BILOBA_CHROME_HEADLESS_SHELL` env var
3. your `PATH`
4. the `@puppeteer/browsers` and Biloba download caches

If none turn it up, Biloba **fails fast with instructions** (it will not silently download). Install it once:

```bash
npx @puppeteer/browsers install chrome-headless-shell@stable
```

Or have Biloba download+cache it the first time (opt-in, since it reaches the network):

```go
biloba.SpinUpChrome(GinkgoT(), biloba.AutoInstallHeadlessShell())
```

### When you need full-browser realism

```go
biloba.SpinUpChrome(GinkgoT(), biloba.HighFidelityHeadless())
```

This runs the full ("new") headless Chrome — pixel-accurate, extensions, etc. — but markedly slower and it serializes parallel work. Keep the bulk of the suite on the shell and run a focused high-fidelity lane where it earns its keep.

## 3. Choose a bootstrap variation

Trade isolation against performance by editing the bootstrap. All three are simple code changes — try them on your suite.

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

You can mix per-process browsers with fresh-tab-per-spec.

## 4. Suite-level config

`SpinUpChrome(GinkgoT(), ...)` options:
- `biloba.HighFidelityHeadless()` — full headless Chrome.
- `biloba.AutoInstallHeadlessShell()` — download the shell if missing.
- `biloba.HeadlessShellPath(path)` — point at a specific shell binary.
- `biloba.StartingWindowSize(w, h)` — default tab size (default `1024x768`); a process-wide setting.
- `biloba.ChromeFlags(...)` — raw `chromedp.ExecAllocatorOption`s (e.g. `chromedp.Flag("headless", false)` to watch).

`ConnectToChrome(GinkgoT(), ...)` carries Biloba-specific config — most of it about failure artifacts (outlines, screenshots, inline images), which is covered in `biloba:debug-failures`. Under CI or an AI agent, **failure artifacts need zero config** — Biloba auto-detects and emits a DOM outline plus screenshot files on disk.

## 5. Run it

```bash
ginkgo -r -p              # parallel — Biloba is built for this
ginkgo -r -p -randomize-all
```
