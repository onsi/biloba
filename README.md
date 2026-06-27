![Ginkgo](https://onsi.github.io/biloba/images/biloba.png)

[![test](https://github.com/onsi/biloba/workflows/test/badge.svg?branch=master)](https://github.com/onsi/biloba/actions?query=workflow%3Atest+branch%3Amaster) | [Biloba Docs](https://onsi.github.io/biloba/)

---

# Biloba

> "Automated browser testing is slow and flaky" - _every developer, ever_

Biloba builds on top of [chromedp](https://github.com/chromedp/chromedp) to bring stable, performant, automated browser testing to Ginkgo. It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

It's blazing fast and designed to work well with AI toolchains like Claude Code.  It's under active development and use as I build out a new feature-rich single-page app with Claude.

Take a look at the [documentation](https://onsi.github.io/biloba) to learn more and get started!

Here's a quick taste of what Biloba specs look like:

```go
func login(tab *Biloba, user string, password string) {
	GinkgoHelper()
	tab.Navigate("/login")
	Eventually(tab.ByLabel("Username")).Should(tab.SetValue(user)) // locator: a form control by its label
	tab.SetValue(tab.ByLabel("Password"), password)
	tab.Click(tab.ByRole("button").WithName("Log in"))            // locator: role + accessible name
	Eventually(".chat-page").Should(tab.Exist())
}

Describe("a simple chat app", func() {
	// b is a *Biloba instance spun up in our BeforeSuite (not shown).  We open an
	// isolated tab per user, and generate reusable selectors/locators off b.
	var tabSally, tabJane *Biloba
	BeforeEach(func() {
		tabSally = b.NewTab()
		login(tabSally, "sally", "yllas")
		tabJane = b.NewTab()
		login(tabJane, "jane", "enaj")
	})

	It("shows all logged in users as present", func() {
		// both tabs should show both users online, by the names a user actually reads
		for _, tab := range []*Biloba{tabSally, tabJane} {
			Eventually(b.ByText("Sally").Within("#user-list")).Should(tab.HaveClass("online"))
			Eventually(b.ByText("Jane").Within("#user-list")).Should(tab.HaveClass("online"))
		}
	})

	It("shows Jane that Sally is typing", func() {
		lastEntry := b.ByRole("listitem").Within("#conversation").Last()
		tabSally.SetValue("#input", "Hey Jane, how are you?")
		Eventually(lastEntry).Should(SatisfyAll(
			tabJane.HaveText("Sally is typing..."),
			tabJane.HaveClass("typing"),
		))

		tabSally.SetValue("#input", "")
		Eventually(lastEntry).ShouldNot(SatisfyAny(
			tabJane.HaveText("Sally is typing..."),
			tabJane.HaveClass("typing"),
		))
	})

	It("delivers messages between Sally and Jane", func() {
		lastEntry := b.ByRole("listitem").Within("#conversation").Last()
		tabSally.Type("#input", "Hey Jane, how are you?") // real keystrokes...
		tabSally.Type("#input", biloba.Keys.Enter)        // ...sent by pressing Enter
		Eventually(lastEntry).Should(tabJane.HaveText("Hey Jane, how are you?"))

		tabJane.Type("#input", "I'm splendid, Sally!")
		tabJane.Click(b.ByRole("button").WithName("Send"))
		Eventually(lastEntry).Should(tabSally.HaveText("I'm splendid, Sally!"))
	})

	It("lets Sally share a document that Jane can download", func() {
		tabSally.SetUpload(b.ByLabel("Attach a file"), "./fixtures/report.pdf")
		tabSally.Click(b.ByRole("button").WithName("Send"))

		doc := b.ByRole("link").WithName("report.pdf")
		Eventually(doc).Should(tabJane.BeVisible()) // Jane sees the shared document...
		tabJane.Click(doc)                          // ...and downloads it
		Eventually(tabJane).Should(tabJane.HaveDownloaded("report.pdf"))
	})

	It("reveals message actions on hover", func() {
		rb := tabSally.Realistic() // a view of the same tab, driven by real Chrome input
		tabSally.SetValue("#input", "Hey Jane")
		tabSally.Click(b.ByRole("button").WithName("Send"))

		last := b.ByRole("listitem").Within("#conversation").Last()
		rb.Hover(last) // genuine CSS :hover — one of the few things the fast track can't do
		Eventually(b.ByRole("button").WithName("React").Within(last)).Should(tabSally.BeVisible())
	})

	It("shows an error when a message fails to send", func() {
		tabSally.AbortRequest(ContainSubstring("/messages")) // make the send fail, hermetically
		tabSally.SetValue("#input", "Hey Jane")
		tabSally.Click(b.ByRole("button").WithName("Send"))
		Eventually(b.ByRole("alert")).Should(tabSally.HaveText("Message failed to send"))
	})

	It("loads conversation history", func() {
		// stub the history response
		tabSally.StubRequest(ContainSubstring("/history"), biloba.StubResponse{
			Body: `[{"from":"Jane","text":"Welcome back!"}]`,
		})
		tabSally.Navigate("/chat")
		Eventually(b.ByRole("listitem").Within("#conversation")).Should(tabSally.HaveText("Welcome back!"))
	})

	It("tracks when users aren't online", func() {
		jane := b.ByText("Jane").Within("#user-list")
		Eventually(jane).Should(tabSally.HaveClass("online"))

		tabJane.Close()
		Eventually(jane).Should(tabSally.HaveClass("offline"))
	})
})
```

Run these in series with `ginkgo`.  And in parallel with `ginkgo -p` for fast, stable, isolated browser tests.

Biloba is quite feature complete and in active development.  However, a 1.0 release milestone has not been reached yet, so the public API contract may shift as the project evolves.

### Poll by default

Browsers are asynchronous, so Biloba's interactions and value-getters **poll by default**.  A fully-applied call like `tab.Click("#go")` or `tab.SetValue("#input", "hi")` retries — finding-and-acting atomically in the browser — until it succeeds or times out.  No more sprinkling `Eventually(...).Should(tab.Exist())` gates in front of every action: the action waits for you.

When you want to make the wait explicit (to compose with `Consistently`, or assert on a richer condition), every interaction also has a matcher form you hand to Gomega:

```go
Eventually("#go").Should(tab.Click())
Eventually(tab.ByLabel("Email")).Should(tab.SetValue("me@example.com"))
```

And when you genuinely want act-once / fail-fast semantics — no polling — opt out with `tab.Immediate()`:

```go
tab.Immediate().Click("#go") // act now; fail immediately if it isn't clickable yet
```

Polling timeout, interval, and context are configurable Gomega-style with `tab.WithTimeout(...)`, `tab.WithPolling(...)`, and `tab.WithContext(...)`.

### Fast and realistic interaction tracks

By default Biloba interactions are **fast**: atomic JavaScript simulations (`el.click()`, value-set, synthetic events) that run as a single in-browser snippet — no scroll, no occlusion check, no real pointer.  This is what keeps Biloba quick and stable, and it's the right default for the vast majority of specs.

For the handful of specs that need genuine input fidelity — real CSS `:hover`, occlusion-aware clicks, scroll-into-view, real keystrokes/drags/wheel/touch — `b.Realistic()` returns a view of the *same tab* whose interactions route through real Chrome DevTools Protocol input.  Same API, just a more faithful (and slightly slower) interaction engine.  See the [documentation](https://onsi.github.io/biloba) (and the `biloba:realistic-mode` Claude Code skill).

### Performance

Biloba is fast.  [**onsi/biloba-comparison**](https://github.com/onsi/biloba-comparison) is a reproducible, three-way speed comparison against Playwright — an identical 32-scenario suite run under biloba-fast, biloba-realistic, and Playwright.  On an Apple M1 Max (whole-suite wall clock, median of 15 runs):

| config | parallel (8 workers) | serial |
|---|---:|---:|
| **biloba-fast** | **2.57s** | **9.55s** |
| **biloba-realistic** | **3.26s** | **18.60s** |
| playwright | 8.23s | 38.37s |

biloba-fast runs the suite **~3.2× faster in parallel / ~4.0× serial** than Playwright; even biloba-realistic — doing the same real-CDP-input work Playwright does — stays **~2.5× / ~2.1×** ahead.  See [the comparison repo](https://github.com/onsi/biloba-comparison) for the methodology, the per-bucket breakdown, and the charts.

### Failure Output

Biloba automatically captures and emits screenshots and any JavaScript console output when tests fail.  It even hooks into Ginkgo's progress emitter infrastructure so `^T`/`SIGNIFO` on a mac (`SIGUSR2` on linux) will spit out a screenshot.

Screenshots are great for humans but won't show up in most CI systems and don't help AI agents.  Biloba autodetects when it's being run in CI or by an agent and spits out DOM outlines and puts screenshot files on disk instead automatically.

### Using Biloba with Claude Code

Biloba ships a set of [Claude Code](https://claude.com/claude-code) skills as a plugin, with this repo doubling as the marketplace. From inside Claude Code:

```
/plugin marketplace add onsi/biloba
/plugin install biloba@biloba
```

(or non-interactively: `claude plugin marketplace add onsi/biloba` then `claude plugin install biloba@biloba`)

---

Ginkgo Tree Graphics Designed By 可行 From <a href="https://lovepik.com/image-401791345/ginkgo-branches-in-autumn.html">LovePik.com</a>