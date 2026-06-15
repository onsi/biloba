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
	Eventually("#user").Should(tab.SetValue("sally"))
	tab.SetValue("#password", "yllas")
	tab.Click("#log-in")
	Eventually(".chat-page").Should(tab.Exist())
}

Describe("a simple chat app", func() {
	var tab *Biloba
	BeforeEach(func() {
		login(b, "sally", "yllas")
		tab = b.NewTab()
		login(tab, "jane", "enaj")
	})

	It("shows all logged in users as present", func() {
		userXPath := b.XPath("div").WithID("user-list").Descendant()
		// b should show both users
		Eventually(userXPath.WithText("Sally")).Should(b.HaveClass("online"))
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
		// tab should show both users
		Eventually(userXPath.WithText("Sally")).Should(tab.HaveClass("online"))
		Eventually(userXPath.WithText("Jane")).Should(tab.HaveClass("online"))
	})

	It("shows Jane when Sally is typing", func() {
		lastEntryXPath := tab.XPath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		Eventually(lastEntryXPath).Should(SatisfyAll(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))

		b.SetValue("#input", "")
		Eventually(lastEntryXPath).ShouldNot(SatisfyAny(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))
	})

	It("shows Jane new messages from Sally, and sally new messages from Jane", func() {
		lastEntryXPath := tab.XPath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		b.Click("#send")
		Eventually(lastEntryXPath).Should(tab.HaveInnerText("Hey Jane, how are you?"))

		tab.SetValue("#input", "I'm splendid, Sally!")
		tab.Click("#send")
		Eventually(lastEntryXPath).Should(b.HaveInnerText("I'm splendid, Sally!"))
	})

	It("tracks when users aren't online", func() {
		userXPath := b.XPath("div").WithID("user-list").Descendant()
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))

		tab.Close()
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("offline"))
	})
})
```

Run these in series with `ginkgo`.  And in parallel with `ginkgo -p` for fast, stable, isolated browser tests.

Biloba is quite feature complete and in active development.  However, a 1.0 release milestone has not been reached yet, so the public API contract may shift as the project evolves.

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