---
layout: default
title: Biloba
---
{% raw  %}
![Biloba](./images/biloba.png)
<div class="image-attribution">Ginkgo Tree Graphics Designed By 可行 From <a href="https://lovepik.com/image-401791345/ginkgo-branches-in-autumn.html">LovePik.com</a></div>

<blockquote>
"Automated browser testing is slow and flaky"
<div class="attribution">- every developer, ever</div>
</blockquote>

Biloba builds on top of [chromedp](https://github.com/chromedp/chromedp) to bring stable, performant, automated browser testing to Ginkgo. It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

We'll unpack these throughout this document - which is intended as a supplement to the API-level [godocs](https://pkg.go.dev/github.com/onsi/biloba) to give you a mental model for Biloba.

## Getting Started

### Support Policy

Biloba is currently under development.  Until a v1.0 release is made there are no guarantees about the stability of its public API.

### Bootstrapping Biloba

Biloba requires you use the latest versions of Ginkgo v2 and Gomega to build your test suite.  You can add Biloba to a project via:

```bash
go get github.com/onsi/biloba
```

and then create a new automated browser testing suite like this:

```bash
mkdir browser
cd browser
ginkgo bootstrap
```

(of course, you can pick any name you want).  This will generate a new Ginkgo bootstrap file called `browser_suite_test.go`. Open it and incorporate Biloba like so:

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

Someday, when Biloba hits 1.0, `ginkgo bootstrap` will be updated to support generating this boiler plate for you.  For now you'll have to copy and paste it.

We're now ready to write some tests.  Here's an example test that visits Ginkgo's GitHub page and confirms the doc link takes us to Ginkgo's documentation page:

```go
var _ = Describe("The Ginkgo GitHub page", func() {
	BeforeEach(func() {
		b.Navigate("https://github.com/onsi/ginkgo")
		Eventually("#readme").Should(b.Exist())
	})

	It("should have a link to the docs", func() {
		b.Click(b.XPath("a").WithText("Ginkgo Docs"))
		Eventually(b.Location).Should(Equal("https://onsi.github.io/ginkgo/"))
		Eventually(b.XPath("h2").WithTextContains("Getting Started")).Should(b.BeVisible())
	})
})
```

If we run the suite via `ginkgo` we'll see a little green dot: the test passes!

> "There's a lot going on here!"

Let's break it down briefly.  In the bootstrap file we declare a single shared `*Biloba` variable:

```go
var b *Biloba
```

this will be the entrypoint to Biloba for all our specs.  Unlike Ginkgo and Gomega there is no need to `. import` Biloba - most of what we'll in order to drive the browser and write specs lives on `b`.

Speaking of browsers.  We initialize `b` first by spinning up chrome:

```go
biloba.SpinUpChrome(GinkgoT())
```

and then connecting to it:

```go
b = biloba.ConnectToChrome(GinkgoT())
```

> "But wait, where is all the error handling?"

...there isn't any.  Or, more correctly, it's all happening for you.  In both of these statements, `GinkgoT()` serves as the connection point between Biloba and Ginkgo.  If an error occurs when spinning up or connecting to Chrome, Biloba will tell Ginkgo... and fail the test suite.

> "Why are 'spinning up Chrome' and 'connecting to Chrome' separate commands?"

Good question, we'll dig into that in the [Performance and Stability](#performance-and-stability) section below.

After setting up our `b` instance of Biloba we have one last piece of boilerplate to go:

```go
var _ = BeforeEach(func() {
	b.Prepare()
}, OncePerOrdered)
```

`b.Prepare()` instructs Biloba to get ready for the next test.  This closes all the browser tabs (but one - more on that below), cleans up some internal state, and registers some Ginkgo hooks to get Biloba ready for the next spec.  We use the `OncePerOrdered` decorator to make sure that Biloba doesn't clean up between each `It` in an [`Ordered` Ginkgo container](https://onsi.github.io/ginkgo/#setup-around-ordered-containers-the-onceperordered-decorator).

With the boilerplate out of the way we can now write some specs.  For our `"The Ginkgo Github Page"` specs we start by navigating to Ginkgo's GitHub page and waiting for the DOM to render:

```go
BeforeEach(func() {
    b.Navigate("https://github.com/onsi/ginkgo")
    Eventually("#readme").Should(b.BeVisible())
})
```

here `b.Navigate` tells Chrome to go to `https://github.com/onsi/ginkgo`.  Biloba will also assert that the navigation succeeded (status code 200).  But navigation is not quite enough to ensure the page is ready to test against.  For that we wait for a DOM element to appear (in this case, the DOM element matching the query selector `#readme`):

```go
Eventually("#readme").Should(b.Exist())
```

`b.Exist()` is a Gomega matcher that takes a selector (in this case `"#readme"`) and asserts that it exists.  We use `Eventually` to poll the browser until the assertion succeeds.  Once it does we know the page is ready and our test can run:

```go
It("should have a link to the docs", func() {
    b.Click(b.XPath("a").WithText("Ginkgo Docs"))
    Eventually(b.Location).Should(Equal("https://onsi.github.io/ginkgo/"))
    Eventually(b.XPath("h2").WithTextContains("Getting Started")).Should(b.BeVisible())
})
```
here we find and click on the anchor tag containing the text `Ginkgo Docs`.  This will eventually take us to `https://onsi.github.io/ginkgo/` where we should eventually see an `h2` tag with text containing `Getting Started`.

> "`b.XPath(...)`?"

Yep.  Biloba supports both css queries (provided as raw strings) like `#readme` and `XPath` queries provided via `b.XPath()` and while you can write out `b.XPath("//h2[contains(text(), 'Getting Started')]")`, Biloba provides a mini-DSL to construct such XPath queries for you.

> "Cute.  What else can Biloba do?  Whet my appetite!"

Here's a partial list:
- Create and manage multiple tabs
- Run and assert on arbitrary javascript
- Fill out forms
- Handle uploads, downloads and dialog boxes
- Set and get cookies and local storage
- Forward all `console.log`s to the `GinkgoWriter`
- Automatically emit inline screenshots (or DOM outlines when an agent is running the tests) to the terminal when a test fails...
- ...or whenever a [Ginkgo Progress Report](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) is generated
- Run your specs in parallel with `ginkgo -p`

> "But you're obviously missing X, Y, and Z!"

Biloba is maturing fast.  Send in a PR!  Or, if you prefer, just use [`chromedp`](https://github.com/chromedp/chromedp) directly to accomplish what you need.  Or drop down all the way to [`cdproto`](https://pkg.go.dev/github.com/chromedp/cdproto) to use the [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) directly.   Every Biloba tab exposes its `chromedp` context via `b.Context` - so you can mix and match as needed.

> Why should I use this thing when far more mature tools like [Playwright](https://playwright.dev), [puppeteer](https://pptr.dev), [selenium](https://www.selenium.dev), and [capybara](https://github.com/teamcapybara/capybara) exist?"

If you're building something out in Go, using an LLM agent, happen to know and like Ginkgo, and/or want to experiment with a shiny new toy that's aiming to deliver performant non-flakey automated browser tests... Give Biloba a try - and start opening issues and sending in PRs!

> "Who even writes code these days?  Don't the LLMs do it all for you?"

Sure.  And LLMs do best when they have a solid deterministic non-flaky *fast* feedback loop.  Biloba is blazing fast and its DSL is designed to work well with AI toolchains like Claude Code.  It's under active development and use and is being oriented towards enabling both human and agent workflows.

### Performance and Stability

Biloba has a few tricks up its sleeve to make it easy to write browser-based tests that are performant and stable.

#### Parallelization: How Biloba Manages Browsers and Tabs

First up - Biloba embraces parallelization, and leverages Ginkgo's multi-process parallelization and Chrome's per-tab isolation to minimize the risk of your parallel specs stepping on each other's toes.

A standard Biloba-flavored Ginkgo bootstrap file will spin up Chrome and connect to it in a [`SynchronizedBeforeSuite`](https://onsi.github.io/ginkgo/#parallel-suite-setup-and-cleanup-synchronizedbeforesuite-and-synchronizedaftersuite) like so:

```go
var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
}, func() {
	b = biloba.ConnectToChrome(GinkgoT())
})
```

When running in parallel Ginkgo spins up multiple processes.  The **First Process** is special and is tasked with setting up and tearing down singleton resources.  It will run `biloba.SpinUpChrome` to automatically download (if necessary) and spin up a Chrome process (all using `chromedp` under the hood).  Once this is done all processes connect to that Chrome process and each process creates a new tab.  That tab is represented by `b`: an instance of Biloba that we call the **Root Tab**.  If we're running with 4 parallel processes we end up with one chrome process and 4 separate Root Tabs, each represented by `b` in its own Ginkgo process.

This has several benefits - starting several browsers is slow and resource intensive.  Opening up several tabs is not.  And we can rely on Chrome's per-tab isolation to ensure that our tests do not cross-pollute one another.

And while Chrome tabs are fast and lightweight...nit turns out that creating a new tab brings an overhead over reusing an existing tab.  And so, rather than create a new tab for _every_ spec - Biloba prefers to reuse the same Root Tab - `b` - between specs.  To do this successfully we need to do some cleanup before each spec and that's where this bit of boilerplate comes in:

```go
var _ = BeforeEach(func() {
	b.Prepare()
}, OncePerOrdered)
```

This allows Biloba to clean up the Root Tab between each spec.  It does this by clearing out some internal state, closing any other tabs that may have been opened during the last spec, and navigating the Root tab to `about:blank`.

So... Biloba's Root Tab is special: it can't be closed and is intended to be reused between specs.  You can, of course, create new tabs:

```go
tab := b.NewTab()
```

here `tab` is just another `Biloba` instance - however it points to a fresh, isolated, Chrome tab.  `b` and `tab` now represent separate tab universes - whatever you do in `b` won't affect `tab` and vice versa.  This can be useful for testing flows that require multiple tabs (e.g. a comment posted by a user in one tab should eventually appear to a viewer monitoring a different tab).  Unlike `b` - however - Biloba will close `tab` when `b.Prepare()` is called to prepare for the next spec.

This clearing-of-the-decks between specs allows Biloba to naturally conform to Ginkgo's principle of [Spec Independence](https://onsi.github.io/ginkgo/#mental-model-ginkgo-assumes-specs-are-independent): individual specs are completely independent of one-another and so can be run in any order... even in parallel.  And that is the key to Biloba's performance.  By adhering to [established patterns](https://onsi.github.io/ginkgo/#patterns-for-parallel-integration-specs) a well-written Biloba suite can see substantial performance gains by simply running in parallel with `ginkgo -p`  

Consider Biloba's own test suite: at the time of writing it consists of 185 specs.  These take about 10 seconds to run in series on an M1 Max Macbook Pro.  But only about 2 seconds when run with `ginkgo -p`!  We get performance from parallelization and careful resource reuse; and stability from Chrome's per-tab isolation.

#### Pragmatism: How Biloba Interacts with the DOM

There's an additional approach Biloba takes to optimize for stability and performance.  When it comes to interacting with the DOM, Biloba favors pragmatism over realism.  

> Huh?

Let's explain by way of example.  This is a sketch of how Puppeteer and chromedp (inspired by Puppeteer) click on an element - each step here is (typically) at least one roundtrip between the test suite and chrome:

1. Query the document for a DOM element matching the selector passed to `click`
2. Scroll the element into view
3. Compute the `(x,y)` centroid of the element in page coordinates
4. Tell the page to click on `(x,y)`

There are **several** benefits to this approach:  First and foremost, this is a a _realistic_ emulation of how human users will interact with the site under test.  They will scroll to the element to bring it to view, the will position the cursor over the element, and then they will click.  This can help catch all manner of accidental bugs: perhaps a transparent element is masking the element in question, absorbing the click - if so the test will fail.  Moreover, by scrolling and clicking Puppeteer/chromedp can more literally show you what's going on when running in non-headless mode: the page scrolls, the element is clicked.

But.  All this realism comes at a cost.  One is performance - there are several steps at play here; each a separate roundtrip to chrome that may be invoking javascript or calling the DevTools protocol.  Of course, any single click will be fast enough - but this overhead will start to add up as the test suite grows.

> Whatever.  Computers are plenty fast - so the performance concerns here are relatively minor.

True.  But it's the potential stability issues that raise deeper concerns.  Each step during the `click` event is an asynchronous call to Chrome and so the entire click event does not constitute a single atomic unit - all sorts of stuff can happen between those individual steps.  That DOM element you found during your query?  Well, by the time you're scrolling it into view... it's gone, perhaps because [React](https://reactjs.org) or [Mithril](https://mithril.js.org) or what-have-you was performing an asynchronous redraw.  Sure, the _selector_ would still return a valid element.  But the concrete, specific, element identified by it the first time around is no longer present.  Fail.

Or, perhaps you've computed the `(x,y)` centroid and are about to click on the element when some image download/ajax request/what-have-you completes and causes the page to reflow.  The element moves.  The click misses, perhaps hitting some other DOM element, leading to a fairly difficult to debug Heisenbug.

> But surely that would happen so rarely.

Exactly.  That's why it's a _flakey_ testing bug.

None of this is intended to throw shade at how these frameworks have implemented these actions.  The positive benefits to this approach are clear and there is a whole class of bugs/regressions that approaches like this can catch.  But the cost is real.  Every now and then, in a complex-enough web app, Something Will Go Wrong™.

Biloba takes a different, more pragmatic, approach.  Here's how Biloba implements click:

1. Invoke `window['_biloba'].click(selector)`

that's it.

> Wat

Every time a page loads, Biloba invokes a short piece of javascript to install a global `_biloba` object on `window`.  This object provides simple Javascript simulations for a bunch of common actions.  The `click` function does the following:

- Find the element matching `selector`
- Validate that it is visible
- Validate that it is not disabled
- Click on it via Javascripts `element.Click()`

All of these steps happen synchronously, and atomically, in the browser.  And the implementation of each step is simple and pragmatic.  The visibility check simply asserts the element has a non-zero `offsetWidth` and `offsetHeight` - there's no scrolling or confirming that it isn't hidden behind some other element.  The interactibility check simply asserts that the element doesn't have the `disabled` attribute set.

The result is a fast _simulation_ of a click event that - because of its simplicity (this is not a real user click) and atomicity (nothing asynchronous is going to happen in between each substep because Javascript is single-threaded) - is far less likely to be slow or flakey.

> But that isn't a real user click!  How is this not cheating?

Perhaps "pragmatic" is a better word than "cheating."  Biloba favors pragmatic good-enough simulation over realism.  The higher-level value at play here is prioritizing testing program _logic and behavior_ over precisely validating the correctness of user interactions.  This also happens to lead to faster, less flakey, tests: a virtuous cycle that promotes more logic and behavior testing and less fear and frustration about flakes.

Of course, Biloba's approach and Puppeteer/chromedp's approaches are not mutually exclusive.  You can trivially drop down to chromedp's `Click` implementation... and you may _want_ to do so for a few specs to guard against the kinds of bugs that Biloba's approach might miss ("user failed to click on the element because your CSS media query is borked and prevents it from scrolling into view").  In this way Biloba gives you a bit more choice: you can focus on fast, stable, simulated-testing by default, but then intentionally drop down to perform some sanity user-interaction testing as needed.

This philosophy applies to most of Biloba's DOM interactions: Biloba doesn't type individual characters into input fields.  It simply sets `value` on the associated DOM element and then triggers the relevant Javascript events.  It doesn't scroll to elements and inspect them to tell you they are visible and not occluded.  It simply measures their size to make sure they have a non-zero area.

> OK, cool.  But what if I really need more realistic interactions?  Is Biloba out?

Not at all - you actually get to **choose**.  Biloba defaults to the fast, pragmatic, atomic path described above (and that's what you want for the overwhelming bulk of your specs).  But at any point you can generate a *realistic view* into your tab with `b.Realistic()`: a lightweight handle whose interactions run through **real Chrome DevTools Protocol input** instead of the synchronous Javascript simulations.  In that mode a click scrolls the element into view, waits for it to stop moving, refuses to click through an occluding overlay, moves the real pointer (so hover-gated clicks fire and CSS `:hover` activates), and dispatches a real mouse event.

It's opt-in *per spec*, so the realism (and the cost and timing-sensitivity that come with it) becomes a tool you get to choose how to deploy.  See [Realistic Interactions](#realistic-interactions) for more details.

#### Headless Fidelity: `chrome-headless-shell` by default

The same "pragmatic simulation over slow, flakey exactness" philosophy drives which _browser_ Biloba runs by default.

Chrome ships two headless implementations:

- **`chrome-headless-shell`** - the original, lightweight headless build (a thin wrapper around Chromium's `//content` module).  It's fast, has minimal dependencies, and - crucially - lets one Chrome process drive many isolated browser contexts _concurrently_.
- **"new" headless** (the default meaning of `--headless` in modern Chrome) - the _full, real_ Chrome browser running without a visible window.  It's higher fidelity (pixel-accurate compositing, extensions, the works) but markedly slower per operation, and its real windowing model serializes work on the browser's UI thread - so parallel Ginkgo processes sharing one browser stop scaling.

For automated _logic and behavior_ testing - Biloba's whole reason for being - the lightweight shell is the right tool: in Biloba's own suite it is roughly **an order of magnitude faster** and restores the across-process parallelism that makes a Biloba suite fly.  So, **by default, `SpinUpChrome` drives `chrome-headless-shell`.**  This is the browser-level expression of the same trade Biloba makes everywhere: favor fast, stable, good-enough simulation, and let you opt into realism when you actually need it.

When you _do_ need full-browser realism (precise rendering, a specific Chrome feature, extension testing), opt in:

```go
biloba.SpinUpChrome(GinkgoT(), biloba.HighFidelityHeadless())
```

This runs the full "new" headless Chrome (and Biloba transparently applies the window/viewport workarounds it needs).  You can mix philosophies just like with DOM interactions: keep the bulk of your suite fast on the shell, and run a focused, higher-fidelity suite where it earns its keep.

##### Getting the `chrome-headless-shell` binary

`chrome-headless-shell` is distributed as a standalone binary (via [Chrome for Testing](https://developer.chrome.com/blog/chrome-for-testing)), separate from your regular Chrome install.  Biloba looks for it in this order:

1. an explicit path you provide via `biloba.HeadlessShellPath("/path/to/chrome-headless-shell")`,
2. the `BILOBA_CHROME_HEADLESS_SHELL` environment variable,
3. your `PATH`,
4. the standard download caches (`@puppeteer/browsers` and Biloba's own cache).

If none of those turn it up, Biloba **fails fast with instructions** rather than silently downloading anything (handy for locked-down CI).  The quickest way to install it:

```bash
npx @puppeteer/browsers install chrome-headless-shell@stable
```

Prefer zero-config?  Have Biloba download and cache the binary itself the first time it's needed:

```go
biloba.SpinUpChrome(GinkgoT(), biloba.AutoInstallHeadlessShell())
```

`AutoInstallHeadlessShell` fetches the current Stable `chrome-headless-shell` from Chrome for Testing into Biloba's cache.  It's opt-in precisely because "a test run quietly reaching out to the network" should be a choice you make, not a surprise.

#### Bootstrapping: Three Ways

We'll close out this section on performance and stability with one last deep-dive into how Biloba suites are bootstrapped - and we'll discuss some options you have to trade-off between additional stability/isolation and performance:

As previously discussed, Biloba's default bootstrapping code looks like this:

```go
var b *Biloba

var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
}, func() {
	b = biloba.ConnectToChrome(GinkgoT())
})

var _ = BeforeEach(func() {
	b.Prepare()
}, OncePerOrdered)
```

When running in parallel, this results in a **shared** browser and a **reused** root tab and is the most-performant configuration with decent, typically good-enough, isolation.

You can favor stronger isolation by spinning up a new browser for each parallel process:

```go
var b *Biloba

var _ = BeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
	b = biloba.ConnectToChrome(GinkgoT())
})

var _ = BeforeEach(func() {
	b.Prepare()
}, OncePerOrdered)
```

such a test suite  will take longer to spin up ("Start all the browsers!") and will be more resource intensive, but comes with the benefit of having stronger isolation between each Ginkgo process.

You can also opt to _not_ use a reusable tab for your specs.  That would look like this:

```go
var rootB *Biloba
var b *Biloba

var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT())
}, func() {
	b = biloba.ConnectToChrome(GinkgoT())
})

var _ = BeforeEach(func() {
	rootB.Prepare()
    b = rootB.NewTab() // we make a new tab for every spec and use it in our specs
}, OncePerOrdered)
```

this results in a performance-hit for *every* spec but _theoretically_ yields stronger cleanup/isolation (we say "theoretically" because it would, most likely, be a bug in Chrome for tab state to not be fully cleared out by navigating to a new site).

Of course you can mix and match both of these approaches and have the combination of distinct browsers and non-reusable tabs.  Note that all these scenarios entail simple code changes to the bootstrap file so you can readily try different combinations to see if/how it affects your particular suite.

> Wait a minute.  How does `ConnectToChrome` now how to connect to the browser that `SpinUpChrome` starts?  You aren't passing anything from one function to the other!

Good catch.  `SpinUpChrome` writes the connection information to a known file-location on disk that `ConnectToChrome` reads from.  This avoids us having to write some boilerplate code to connect the two `SynchronizedBeforeSuite` functions and is an example of how Biloba tries to integrate deeply with Ginkgo and Gomega to help your tests be concise and focused on... well... _testing_.  Let's dive into that topic next.

### Ginkgo and Gomega Integration

Biloba exists to help you write automated browser tests.  It's not trying to be a stand-alone library (👋 chromedp) nor is it trying hard to work in non-Ginkgo test suites - it's designed to be fully integrated with Ginkgo's runtime and Gomega's matcher ecosystem.  If some of what you see in this section feels deeply un-Go like... well, that's because it is!

But.  That's OK.  We're here to write and run some tests.  Let's focus on that.

Here are some of the ways Biloba integrates with Ginkgo and Gomega so you can focus on your tests:

1. Most Biloba functions don't return errors.

	Instead, errors are handled for you and treated as test failures.

2. By default, Biloba provides additional information during failures and progress report requests.

	This happens in `b.Prepare()`.
	
	Biloba registers a Ginkgo hook that runs after each spec.  If the spec has failed, a screenshot of every tab associated with that spec is captured.  Screenshots are emitted to the terminal as inline images (Kitty, iTerm2, or Sixel) **only when the terminal supports it** (see [Inline image gating](#inline-image-gating) below).  Biloba can also attach a text DOM outline of every tab on failure; this is off by default for an interactive human but on automatically under CI or an AI agent (see [Failure artifacts](#failure-artifacts)).

	In addition, Biloba registers a Ginkgo ProgressReporter that will emit screenshots whenever a [progress report](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) is requested.  This can happen when a spec times out, or when a spec decorated with `PollProgressAfter(X duration)` has taken longer than `X` to complete.  On MacOS you can get a progress report instantly by sending a `SIGINFO` signal with `^T`.  On Linux you can send a `SIGUSR2` signal.

	Both of these mechanisms make it just a bit easier to debug a failing test by getting visual feedback.

	(Note that the boolean `BilobaConfig` options take an optional bool — `BilobaConfigFailureScreenshots(false)` and `BilobaConfigProgressReportScreenshots(false)` turn those off, and `BilobaConfigFailureOutlines()` turns outlines on.  See [Failure artifacts](#failure-artifacts).)

3. All `console.log/info/warn/etc.` output gets immediately streamed to Ginkgo's GinkgoWriter.

	That means that output will be visible, but only if a test fails or you are running in verbose mode with `ginkgo -v`

4. `console.assert` failures count as test failures.

	This allows you to seamlessly decide whether some assertions are better handled in Javascript vs in Go.

5. Biloba polls by default.  Most DOM interactions keep retrying until they succeed (or time out), and any of them can also hand you a Gomega matcher that _you_ drive with `Eventually`/`Consistently`.

	This means you rarely need a separate readiness check before acting.  A quick example: `b.Click("#submit")` keeps trying to click the element with `id` `submit` - it waits until that element exists, is visible, and is enabled, then clicks it once.  If you'd rather compose the polling yourself, drop the argument and `b.Click()` returns a matcher: `Eventually("#submit").Should(b.Click())` (this, by the way, is exactly what `b.Click("#submit")` is running for you under hte hood.)

	We'll dive into all of this - the configuration knobs (`b.WithTimeout`/`b.WithPolling`/`b.WithContext`) and the `b.Immediate()` escape hatch - in the [Interacting with Elements](#interacting-with-elements) section below.

### Claude Code Skills

Biloba ships a set of [Claude Code](https://claude.com/claude-code) skills as a **plugin**, so an agent writing tests against *your* app has Biloba's idioms on hand.  The Biloba repo doubles as the plugin marketplace, so installation is two commands.  From inside Claude Code:

```
/plugin marketplace add onsi/biloba
/plugin install biloba@biloba
```

(The same can be done non-interactively with `claude plugin marketplace add onsi/biloba` and `claude plugin install biloba@biloba`.)

This installs a family of `biloba:*` skills that activate automatically while you write tests, and can also be invoked explicitly (e.g. `/biloba:explore-unfamiliar-page http://localhost:8080`):

| Skill | What it's for |
|---|---|
| `biloba:overview` | The mental model — the three principles and how they shape your specs. |
| `biloba:setup` | Wiring Biloba into your suite: bootstrap, `chrome-headless-shell`, the bootstrap variations. |
| `biloba:write-tests` | Authoring specs: the dual immediate/matcher API, selecting elements, hermetic tests, multiple tabs. |
| `biloba:realistic-mode` | The realistic interaction track (`b.Realistic()`) for occlusion/hover/drag/scroll/touch-sensitive flows. |
| `biloba:xpath` | Building selectors with the `b.XPath()` DSL. |
| `biloba:api` | A one-line reference for every method and matcher. |
| `biloba:explore-unfamiliar-page` | Orienting to a page you haven't seen, then drafting a starter spec. |
| `biloba:debug-failures` | DOM outlines, screenshots, and the env/config knobs that surface them. |

### `chromedp`: Breaking the Fourth Wall

Biloba is only possible because of all the hard work that's gone into the [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) and the [chromedp](https://github.com/chromedp) [client](https://github.com/chromedp/chromedp) and [Go protocols](https://github.com/chromedp/cdproto).  Biloba wraps all this great stuff to give you a productive testing environment - but it doesn't hide any of it.  You can drop down to `chromedp` and `cdproto` and mix and match Biloba with `chromedp` trivially simply by passing in `b.Context ` to `chromedp`.

For example, Biloba doesn't yet have a first-class wrapper for emulating geolocation.  You can reach for `chromedp` and `cdproto` to do that yourself:

```go
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	return emulation.SetGeolocationOverride().WithLatitude(48.8584).WithLongitude(2.2945).WithAccuracy(10).Do(ctx)
}))
```

When a capability is common enough Biloba grows native support for it (see, for example, [Cookies and Storage](#cookies-and-storage)).  Until then, `b.Context` is always there as an escape hatch.

#### Emulation and device conveniences (drop to chromedp)

Device and environment **emulation** - viewport/device metrics, geolocation, permissions, offline, locale/timezone, color-scheme, and reduced-motion/media - is, by design, *not* wrapped by Biloba: it's session-level state that rarely changes mid-spec, the CDP calls are already ergonomic, and wrapping them would add surface without removing real friction.  Reach through `b.Context` with `cdproto`'s `emulation` and `network` domains.  Here are the common recipes:

```go
import (
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Viewport / device metrics (mobile)
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	return emulation.SetDeviceMetricsOverride(390, 844, 3, true).Do(ctx) // iPhone-ish: w, h, scale, mobile
}))

// Geolocation
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	return emulation.SetGeolocationOverride().WithLatitude(48.8584).WithLongitude(2.2945).WithAccuracy(10).Do(ctx)
}))

// Locale + timezone
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	if err := emulation.SetLocaleOverride().WithLocale("fr-FR").Do(ctx); err != nil { return err }
	return emulation.SetTimezoneOverride("Europe/Paris").Do(ctx)
}))

// Color scheme / reduced motion (and other media features)
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	return emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{
		{Name: "prefers-color-scheme", Value: "dark"},
		{Name: "prefers-reduced-motion", Value: "reduce"},
	}).Do(ctx)
}))

// Offline / throttled network
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
	return network.OverrideNetworkState(true, 0, -1, -1).Do(ctx) // offline=true, latency, down, up
}))
```

(`SetWindowSize` is the one piece of this Biloba *does* wrap natively - see [Window Size](#window-size-screenshots-configuration-and-debugging).)

**Cross-origin iframes** are likewise out of scope: Biloba's `>>>` piercing handles same-origin iframes and open shadow roots, but a cross-origin frame is a separate CDP target.  Until Biloba grows per-target frame support, drive the frame's target directly through chromedp.  Multi-browser (Firefox/WebKit) is a deliberate non-goal - Biloba is Chrome-only by design.

### The rest of these docs...

...will cover the breadth of what Biloba offers today.  The focus will be less on exhaustively documenting every function (that's what the [go docs](https://pkg.go.dev/github.com/onsi/biloba) are for) and more on providing mental models and showcasing examples.

## Navigation

You instruct a Biloba tab to navigate to a url via:

```go
b.Navigate("http://example.com/search?q=foo")
```

this navigates the tab and ensures the response was `http.StatusOK`.  If you need to assert a different response code use `NavigateWithStatus("http://example.com/not-found", http.StatusNotFound)`

`Navigate` is a [waiting command](#interacting-with-elements): it does a single bounded wait (~30s by default) for the navigation to complete, rather than polling.  You can override that deadline, or thread in a cancellable context, with `b.WithTimeout(...)` and `b.WithContext(...)` - `b.WithTimeout(60 * time.Second).Navigate(slowURL)`.  (`WithPolling` and `Immediate` don't apply to a one-shot wait and are rejected.)

The DOM will probably not be ready immediately after navigation so a typical next line will be an `Eventually` that looks something like:

```go
b.Navigate("http://example.com/search?q=foo")
Eventually("#content").Should(b.Exist())
```

This will, typically, indicate that the page has loaded and is ready for testing.

Related to navigation, Biloba also lets you query the current URL for the tab (via `url := b.Location()`) and the title (via `title = b.Title()`).  You can use both of these with `Eventually`.  For example:

```go
b.Navigate("http://example.com/table-of-contents")
Eventually(b.XPath().WithTextStartsWith("Introduction").Should(b.Click()))
Eventually(b.Title).Should(HaveSuffix("Introduction"))
```

this test will:
- navigate to the table of contents url
- keep trying until it successfully clicks on an element that has text that begins with "Introduction"
- assert that the tab eventually ends up on a page whose title ends with "Introduction"

In addition to polling the `b.Location`/`b.Title` functions directly, you can apply the `HaveURL()` and `HaveTitle()` matchers to the tab itself.  This reads more naturally and parallels the [tab matchers](#finding-and-managing-spawned-tabs):

```go
Eventually(b).Should(b.HaveURL("http://example.com/table-of-contents"))
Eventually(b).Should(b.HaveTitle(HaveSuffix("Introduction")))
```

Both accept either a string (for an exact match) or a Gomega matcher.

## Managing Tabs

Biloba encourages you to reuse the root tab (`b`) for most of your specs.  Of course some tests will require you to interact with multiple tabs.  There are, loosely speaking, three different "kinds" of tabs at play in Biloba:

- `b` the reusable root tab.
- new tabs that you explicitly create.  We'll simply call these "Tabs".
- tabs that are created by the browser as the result of a user interaction.  For example clicking on an anchor tag with `target="_blank"`.  We'll call these "Spawned Tabs".

In all three cases the tab is an instance of `*Biloba` and in general all three kinds of tabs are similar except for the following differences:

1. The reusable root tab is never closed.  It is simply reused between specs.  All other tabs are automatically closed by `b.Prepare()`.
2. Explicitly created Tabs are given their own `BrowserContextID`.  This is an important isolation mechanism in Chrome that allows different tabs to be in different isolated universes.  You can think of it as "incognito mode" where each new tab is in its own incognito mode.
3. Spawned Tabs inherit the `BrowserContextID` of the tab that spawned them.

### Creating and Closing Tabs Explicitly

You create Tabs using `tab := b.NewTab()`.  Here's a pseudocode example that shows us testing a multi-user chat application.  It includes an example of reusable helper function to handle logging in to the site, a well as extensive use of `XPath` selectors to perform complex DOM queries:

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

Since each created tab is in its own `BrowserContextID` we don't have to worry about cookie pollution and can log Sally and Jane in on the separate tabs.  As you can see we direct matchers and actions towards a given tab by using the version of the matcher/action on that tab instance (e.g. `tab.HaveClass` vs `b.HaveClass`).  You can also see that non-root tabs can be closed via `tab.Close()`.  Note that `tab.Close()` can return an error - it so happens that when an [active download](#going-the-extra-mile-for-stability) is taking place Biloba may be unable to close a tab.  You can use `Eventually(tab.Close).Should(Succeed())` in such cases to wait for the tab to close.

### Finding and Managing Spawned Tabs

If a browser action results in a _new_ tab being opened you'll need to get a Biloba instance that attaches to that tab in order to be able to use it.  There are three methods that support this:

- `tab.AllSpawnedTabs()` returns a list of tabs (wrapped in Biloba instances) that were spawned by a user action performed on `tab`.
- `tab.HaveSpawnedTab()` returns a matcher that asserts whether or not a spawned tab matches (see below)

`tab.HaveSpawnedTab()` returns a chainable `TabQuery`.  A tab has no single primary key, so you describe the tab you want with refinements - chain any of:

- `WithDOMElement(selector)` matches if the tab has a DOM element satisfying `selector`.
- `WithURL(url)` matches if the tab has a matching url.
- `WithTitle(title)` matches if the tab has a matching title.

The same query plays double duty.  As an assertion you hand it to `Should`/`Eventually` and spell it `HaveSpawnedTab` (or `HaveTab`, below).  As a **predicate** you hand it to the `Find`/`Filter` helpers on the `Tabs` slice returned by `AllSpawnedTabs()`/`AllTabs()` and spell it `TabMatching`, which reads as a description of one tab.  The spellings are interchangeable - they build the same query.

Here's an example that builds off our chat application example from above:

```go
Context("when Sally sends Jane a link", func() {
	BeforeEach(func() {
		b.SetValue("#input", "Hey Jane, check this out: https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		b.Click("#send")
	})
	
	It("allows Jane to open the link in a new tab", func() {
		lastEntryXPath := tab.XPath("#conversation").Descendant().WithClass("entry").Last()
		Eventually(lastEntryXPath).Should(tab.HaveInnerText("Hey Jane, check this out: YouTube"))
		tab.Click(lastEntryXPath.Child("a"))
		Eventually(tab).Should(tab.HaveSpawnedTab().WithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ"))
		youtubeTab := tab.AllSpawnedTabs().Find(tab.TabMatching().WithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ"))
		Eventually("body").Should(youtubeTab.HaveInnerText(ContainSubstring("Never Gonna Give You Up")))
	})
})
```

As you can see, we poll `tab.HaveSpawnedTab()` until the tab appears and then use `tab.AllSpawnedTabs().Find(tab.TabMatching()...)` to get a reference to it. From here we can make assertions against the spawned tab and/or close it.

Note that `b.HaveSpawnedTab` will have failed.  That's because Biloba associates spawned tabs with the `BrowserContextID` of the tab that opened them.  And both `b` and `tab` (which is an explicitly created tab) have _different_ `BrowserContextID`s.

There are analogous `b.AllTabs()` and `b.HaveTab()` functions that let you search through _all_ tabs associated with this Biloba Chrome connection (`b.HaveTab()` returns the same kind of `TabQuery`, just searched against every tab rather than only spawned tabs).  This won't include any tabs opened by other Ginkgo processes running in parallel - but any tabs that are associated with the current process (whether explicitly created Tabs or Spawned Tabs) will be returned by these methods.

## Working with the DOM

Most of what you'll be doing with Biloba will involve working with the DOM: selecting DOM elements, clicking on them, making assertions about their properties, changing their properties, etc...

If you haven't yet, you should pause and read the "[Pragmatism: How Biloba Interacts with the DOM](#pragmatism-how-biloba-interacts-with-the-dom)" section above: it covers Biloba's basic approach to DOM interactions and how it differs from other browser automation frameworks.

Assuming you've read that section, we'll dive into problem number one: telling Biloba _which_ DOM element you want to interact with.

### Selecting DOM Elements

Biloba gives you **three** pathways for telling it which element you mean, and throughout this chapter you'll see the word `selector` stand in for any of them:

- **CSS selectors** - a raw `string`, exactly what you'd pass to `document.querySelector()`.
- **Semantic locators** - built with the `b.By*` constructors, matching the way a *user* (or the accessibility tree) perceives an element: by role, name, visible text, form label, and so on.
- **XPath queries** - built with `b.XPath()`, for the structural/axis long tail CSS can't express.

All three flow through every Biloba action and matcher (and through [realistic mode](#realistic-interactions)).  A `string` is interpreted as CSS; a `Locator` or an `XPath` is interpreted as itself.

#### Which one should I use?

- **CSS - the default.**  For an app you own, prefer CSS targeting a **stable, intentional hook**: an `#id` or a `[data-testid]` you put on the element *on purpose* as a test contract.  Avoid coupling tests to *styling* classes (`.btn-primary`, `.col-md-6`) - those exist for visual reasons, get renamed in redesigns, and reintroduce exactly the brittleness you're trying to avoid.  CSS is the fastest pathway, it's just a raw string (no builder to learn), it supports modern selectors like [`:has()`](https://developer.mozilla.org/en-US/docs/Web/CSS/:has), and it pierces shadow/iframe boundaries via the `>>>` combinator.
- **Locators - reach for these second**, in two cases.  (a) When you *want* to assert the user-perceivable thing - a button's accessible name, a heading's level - which doubles as a free accessibility-regression guard.  (b) When adding a hook isn't worth it and the visible label or text is the natural, readable identifier (`b.ByText("Sign in")`, `b.ByLabel("Email")`).  Locators are the most resilient and readable for user-facing elements, at the cost of being the slowest engine.
- **XPath - the rare power tool**, for axis/relationship/ordinal queries CSS can't express (an *ancestor*, a *following-sibling*, "the `ul` that has a child `li` saying X") or exact `text()`-node matching.  It's native and fast but verbose, and - unlike CSS and locators - it does **not** pierce shadow roots or iframes.

| Pathway | Best for | Speed | Pierces open shadow? |
|---|---|---|---|
| **CSS** (string) | `#id` / `[data-testid]`, structure, `:has()` | fastest | yes, via `>>>` |
| **Locator** (`b.By*`) | role / text / label / testid, a11y | slowest | yes, automatically |
| **XPath** (`b.XPath`) | axes / relationships / ordinals | fast | no |

**A note on performance.**  All three are a single atomic round-trip into the browser, so the only difference is in-page CPU: CSS ≳ XPath (both ride native browser engines) ≫ Locators (an interpreted full-document ARIA scan - `b.ByText`/`b.ByLabel` are the heaviest since they read every element's text).  This only matters on large DOMs under tight `Eventually` polling; for typical pages all three are effectively instant.  Don't let it drive your choice - pick the pathway that reads best and resists churn.

#### Selecting by CSS

A `string` selector is interpreted as a CSS query - exactly what you'd pass to `document.querySelector()`:

```go
b.Click("button.submit")          // the first <button class="submit">
b.Click("#go")                    // by id - a stable, intentional hook
b.Click("[data-testid=save]")     // by a test-id you added on purpose
Eventually("tr:has(td.overdue)").Should(b.Exist())  // modern :has()
```

`b.Click("button.submit")` selects the **first** `<button>` with class `submit` and clicks it.  Because Biloba only ever drives Chrome you have the whole modern CSS grammar available, including [`:has()`](https://developer.mozilla.org/en-US/docs/Web/CSS/:has) for "the element that *contains* X" - a structural query that historically required XPath.  If you'd like to learn more about CSS query selectors the [MDN docs](https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_Selectors) are fantastic.

Remember the recommendation above: lean on `#id` and `[data-testid]` hooks you place intentionally, not on styling classes that exist for looks and get renamed.

##### Piercing Shadow DOM and iframes

A plain `querySelector` can't see inside a web component's shadow DOM or inside an `<iframe>` - the DOM is encapsulated.  Biloba's CSS selectors understand a `>>>` combinator that crosses one such boundary:

```go
// reach a button inside <my-widget>'s shadow root
b.Click("my-widget >>> button.submit")

// reach an element inside a same-origin iframe
Eventually("#editor-frame >>> .toolbar .save").Should(b.Click())

// chain it to descend through several boundaries
b.HaveInnerText("app-shell >>> settings-panel >>> .title")
```

Each `>>>` steps across exactly one boundary: the element to its left is the host (a shadow host or an iframe) and the selector to its right is resolved inside that host's shadow root or document.  `>>>` works with every selector-based method (actions, matchers, and the `*Each`/count forms).

This pierces **open shadow roots** and **same-origin iframes**.  It cannot reach into **closed** shadow roots or **cross-origin** iframes - the browser does not expose their contents to JavaScript, so a selector targeting them simply won't match (drop down to chromedp's frame handling for cross-origin frames).  `>>>` is a CSS-only feature; XPath selectors do not cross boundaries.  (Locators pierce open shadow roots automatically - see below.)

#### Selecting by Locator

The most robust, readable selectors for user-facing elements describe them the way a *user* perceives them - by accessible role and name, visible text, form label, placeholder, alt text, title, or test id - rather than by brittle structure.  Build a `Locator` with one of the `b.By*` constructors; like any selector it flows through every Biloba action and matcher (and through [realistic mode](#realistic-interactions)):

```go
b.Click(b.ByRole("button").WithName("Save"))                  // accessible role + accessible name
Eventually(b.ByRole("heading").WithNameContains("Getting")).Should(b.BeVisible())
b.SetValue(b.ByLabel("Email"), "jane@example.com")            // the <input> labelled "Email"
Eventually(b.ByText("Welcome back, Jane")).Should(b.Exist())  // the smallest element with that exact text
b.Click(b.ByTextContains("Sign"))
```

##### The constructors

- **`b.ByRole(role)`** matches the element's [ARIA role](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Roles) - explicit `role="..."` or the common implicit role of a tag (`button`, `link`, `heading`, `checkbox`, `radio`, `textbox`, `combobox`, `list`, `listitem`, `option`, ...).  Refine a role locator with:
  - **`.WithName(name)` / `.WithNameContains(name)`** - also match the **accessible name** (computed from `aria-labelledby` → `aria-label` → associated `<label>` → `alt` → `placeholder` → `value` → text content → `<figcaption>`/`<caption>` → `title`).
  - **`.Level(n)`** - for `role="heading"`, the heading level (an `aria-level`, or the digit of an `<h1>`-`<h6>` tag): `b.ByRole("heading").Level(2).WithName("Getting Started")`.
  - the **ARIA-state filters** **`.Checked()`**, **`.Disabled()`**, **`.Expanded()`**, **`.Pressed()`**, **`.Selected()`** - narrow to elements in that state (the corresponding property or `aria-*="true"`): `b.ByRole("checkbox").Checked()`, `b.ByRole("button").Disabled()`.
- **`b.ByText(text)`** / **`b.ByTextContains(text)`** match the *smallest* element whose visible text equals / contains `text`.
- **`b.ByLabel(text)`** / **`b.ByLabelContains(text)`** match the form control whose accessible label equals / contains `text`.
- **`b.ByPlaceholder(text)`** / **`b.ByPlaceholderContains(text)`** match the `<input>`/`<textarea>` by its `placeholder`.
- **`b.ByAltText(text)`** / **`b.ByAltTextContains(text)`** match an element (e.g. an `<img>`) by its `alt` text.
- **`b.ByTitle(text)`** / **`b.ByTitleContains(text)`** match an element by its `title` attribute.
- **`b.ByTestID(id)`** matches an element by its test-id attribute.  The attribute name is the package variable `biloba.TestIDAttribute`, which defaults to `"data-testid"` (Playwright's convention).  If your app uses a different convention set it once, e.g. in a `SynchronizedBeforeSuite`:

```go
biloba.TestIDAttribute = "data-qa"
b.Click(b.ByTestID("submit-button"))
```

##### Composition

Locators are immutable - every method returns a new `Locator` - and they **compose**.  Crucially, the filters and combinators that take a selector accept **any** selector (a CSS string, an `XPath`, or another `Locator`), so the three pathways mix freely:

```go
// visible-text filter: pick a container by some text inside it (Playwright's filter({hasText}))
b.Click(b.ByRole("listitem").ContainingText("Product 2"))
b.ByRole("listitem").Within("#products").NotContainingText("Remove")

// descendant filter: "...that has a matching descendant" (Playwright's filter({has}))
b.ByRole("listitem").Containing(".del")                                  // a CSS descendant
b.ByRole("listitem").Containing(b.ByRole("button").WithName("Remove"))   // a Locator descendant
b.ByRole("listitem").Within("#products").NotContaining(".del")

// set combination: intersection / union, in document order
b.Click(b.ByRole("button").And(".primary"))                             // matches BOTH (a CSS string!)
b.ByRole("button").WithName("Save").Or(b.ByRole("button").WithName("Submit"))

// scope: restrict to descendants of a matching container (any selector)
b.Click(b.ByRole("button").WithName("Delete").Within("#dialog"))

// ordinal: a single element by index among the matches
b.ByRole("listitem").Within("#fruits").First()   // == .Nth(0)
b.ByRole("listitem").Within("#fruits").Nth(2)    // the third match (0-based)
b.ByRole("listitem").Within("#fruits").Last()    // the final match
```

- **`.ContainingText(t)` / `.NotContainingText(t)`** keep (or drop) elements whose visible text contains `t`.
- **`.Containing(sel)` / `.NotContaining(sel)`** keep (or drop) elements that have a descendant matching `sel`.
- **`.And(sel)` / `.Or(sel)`** intersect / union with another selector.
- **`.Within(scope)`** restricts matches to descendants of an element matching `scope`.  If `scope` matches nothing the locator matches nothing - the clean way to disambiguate "the Save button *in this dialog*".
- **`.Nth(i)` / `.First()` / `.Last()`** pick a single element by ordinal (out-of-range → no match).

Because the combinators accept any pathway, you can write things like `b.ByRole("button").And(".primary")` or `b.ByRole("listitem").Containing(b.ByText("Delete")).Within("#cart")` - reaching for whichever pathway reads best at each step.

Locators **pierce open shadow roots** - `b.ByRole("button").WithName("Submit")` will find a button inside a custom element's open shadow DOM with no `>>>` ceremony (closed roots and cross-origin frames are skipped, matching the rest of Biloba).

Coverage is a pragmatic ARIA subset rather than the full specification - it handles explicit roles plus the common implicit ones, and the common accessible-name sources.  For anything it can't express, CSS `:has()` and the XPath DSL are right there.

#### Selecting by XPath

XPath is the power tool for the structural long tail - axis and relationship queries CSS can't express (an *ancestor*, a *following-sibling*, "the `ul` that has a child `li` saying X"), ordinals, and exact `text()`-node matching.  You pass a Biloba `XPath` object in as `selector`.  Specify the query manually:

```go
b.Click(b.XPath("//button[contains(concat(' ',normalize-space(@class),' '),'submit')]"))
```

or build it with Biloba's mini-XPath DSL:

```go
b.Click(b.XPath("button").WithClass("submit"))
```

The DSL is documented in detail in [The XPath DSL](#the-xpath-dsl) reference section at the end of this chapter - it generates fairly hairy XPath from a simple series of chained calls.  If you'd like to learn more about XPath queries the [MDN docs](https://developer.mozilla.org/en-US/docs/Web/XPath) will give you a good mental model while the [XPath Cheatsheet at devhints.io](https://devhints.io/xpath) is a fantastic, concise reference.

Note that XPath does **not** cross shadow or iframe boundaries.  For "the element that says X" prefer a text locator (`b.ByText`); for "the element that contains X" CSS `:has()` and locator composition usually read better.  Reach for XPath when those genuinely can't express the relationship you need.

Before we go further and explore the catalog of DOM interactions Biloba provides we should pause and point out an important design decision in Biloba.

When you're interacting with the DOM you **don't** ask Biloba to fetch a DOM element and then take action on it:

```go
/* === INVALID === */
el := b.FindElement(selector)
el.Click()
```

you're always passing in a selector to the action:

```go
b.Click(selector)
```

This design decision helps reduce flakiness in your test suite.  It's possible that the concrete DOM element returned by a hypothetical `FindElement` function is gone (perhaps it was asynchronously re-rendered by your front-end view layer) by the time you attempt to `Click()` it.  Instead, Biloba runs a single synchronous snippet of JavaScript in the browser that fetches the element and then performs the action on it.

Finally - some Biloba methods use the **first** element returned by the `selector` while others use **every** element returned by the selector.  The difference is usually clear based on the name of the method.

Now that we know how to `select` DOM elements - let's dig into what we can do with them.

### Interacting with Elements

Now that we can _select_ elements, let's act on them.  Before we walk the catalog of interactions, one idea shapes nearly all of them: **Biloba polls by default.**

#### Poll by default

Browser tests are full of small waits: a button isn't clickable until the page hydrates, an input doesn't exist until a modal opens, text doesn't settle until a render completes.  The classic source of flakiness is acting a beat too early.  So Biloba's DOM methods don't act once and give up - they keep retrying until they succeed or a deadline passes.

This shows up in the **dual immediate/matcher API** that runs through most of Biloba's action and value methods, keyed on how many arguments you pass:

- **Fully applied → Biloba polls for you.**  `b.Click("#go")` keeps trying to click `#go` until it exists, is visible, and is enabled - then clicks it once.  `b.SetValue("#x", 3)`, `b.Type("#search", "gophers")`, and `text := b.GetInnerText("#title")` all behave the same way: they wait for the element to be ready, then do the thing (and the value-getters return the value).  If the deadline passes first the spec fails with Gomega's familiar `Timed out after…` message.
- **Under applied → you get a matcher to poll.**  Drop the selector and the same method returns a Gomega matcher you wrap in `Eventually`/`Consistently`/`Expect` yourself: `Eventually("#go").Should(b.Click())`, `Eventually("#x").Should(b.SetValue(3))`.  Reach for this when you want to compose the assertion - a custom `Consistently`, a `SatisfyAll`, or a polling style of your own.

Because the fully-applied form already polls, you usually don't need a separate readiness check:

```go
b.Navigate("http://example.com/homepage")
b.Click("#login") // waits for #login to be clickable, then clicks - no preceding Eventually needed
```

#### Tuning the poll: `WithTimeout`, `WithPolling`, `WithContext`

By default Biloba's polling inherits Gomega's global `Eventually` settings (the timeout and interval you set with `SetDefaultEventuallyTimeout`/`SetDefaultEventuallyPollingInterval`).  To override them for a single interaction, ask for a lightweight view of the tab - exactly like [`b.Realistic()`](#realistic-interactions) it's a shallow clone-with-a-flag, so you chain it inline:

```go
b.WithTimeout(5 * time.Second).Click("#slow-to-appear")
b.WithPolling(50 * time.Millisecond).Click("#busy")
b.WithContext(ctx).GetInnerText("#title")                            // a cancelled ctx aborts the wait
b.WithTimeout(10*time.Second).WithPolling(time.Second).Click("#go")  // they compose
```

These mirror Gomega's `Eventually(...).WithTimeout(...).WithPolling(...).WithContext(...)` - they configure the poll Biloba runs under the hood.

#### Acting once: `b.Immediate()` (the escape hatch)

Occasionally you really do want act-once / fail-fast behavior - to assert that something is clickable *right now*, with no waiting.  `b.Immediate()` gives you that:

```go
b.Immediate().Click("#go") // click once; fail immediately if #go isn't ready this instant
```

Treat this as a footgun you reach for rarely.  Acting without polling is the classic way to reintroduce flakiness - the very thing poll-by-default exists to prevent.  When you simply want to bound the wait, prefer `b.WithTimeout(...)` over `b.Immediate()`.

> The matcher form is driven by the `Eventually`/`Expect` you wrap it in, so passing any of these knobs to a bare-matcher form (e.g. `b.WithTimeout(d).Click()` with no selector) is an error - configure the `Eventually`, not the matcher.

#### Which methods honor which knobs

Not every Biloba method polls, so not every method accepts these knobs.  Biloba sorts into four buckets, and misapplying a knob is a loud error (so you find out immediately rather than via a silent no-op):

| Bucket | Examples | `WithTimeout` / `WithContext` | `WithPolling` | `Immediate` |
|---|---|---|---|---|
| **Polling** - actions & value-getters | `Click`, `SetValue`, `Type`, `GetProperty`, `GetValue`, `GetInnerText`, `InvokeOn` | honored | honored | honored |
| **Waiting commands** - one bounded wait | `Navigate`, the `CaptureScreenshot*` family | honored (overrides the built-in deadline) | error | error |
| **Snapshot / state queries** | `HasElement`, `Count`, `Title`, every `Current*ForEach`, `GetCookies` | error | error | error |
| **One-shot mutations & raw JS** | `SetWindowSize`, the `*Immediately` family, `Run`, `RunAsync` | error | error | error |

The intuition: a method polls (and accepts every knob) when it is *waiting for the DOM to reach a state*.  A **waiting command** like `Navigate` does a single bounded wait for one event, so it keeps a purpose-built default deadline (~30s for navigation, ~5s for screenshots) that `WithTimeout` can override - but there's no repeated probe for `WithPolling` to tune.  A **snapshot** reads "what's true right now" and so never waits; when you want to wait for a snapshot to change, gate it on a polling matcher first (`Eventually(sel).Should(b.HaveCount(n))`, *then* read).  A **one-shot mutation** (and the raw-JS `Run`/`RunAsync`) just does its thing once.

### Existence, Counting, Visibility, and Interactibility

You can check if a tab has an element matching `selector` using  `b.Exist()` which returns a matcher:

```go
Expect(selector).To(b.Exist()) // assert that the element is there right now
Eventually(selector).Should(b.Exist()) // assert that the element exists, eventually
```

if you want to assert the existence of `selector` on a different tab you would:

```go
Eventually(selector).Should(tab.Exist())
```

note that we use `tab`'s `Exist()` matcher here instead of the reusable root tab `b`.

`Exist()` is as simple as it gets - it succeeds if the `selector` query returns an element.

---

You can count the number of elements that match a selector with:

```go
b.Count(selector)
```

but be aware - this returns its result immediately without polling.  If you need to wait for the DOM to settle and make sure some concrete number of elemnts are present use `HaveCount()`:

```go
Expect("a").To(b.HaveCount(7))
Eventually("img.thumbnail").Should(b.HaveCount(BeNumerically(">", 10)))
```

if no elements match the `selector`, `Count/HaveCount` return `0`.  Obviously.

---

To assert that an element is visible use `BeVisible()`:

```go
Eventually(selector).Should(b.BeVisible())
```

Biloba's visibility check performs the following javascript as one atomic unit:

- query the selector and grab the first matching element
- fail if no element is returned (validate existence)
- check the element's visibility and return the result

Biloba's visibility check is simple and pragmatic.  The element must satisfy:

```js
el.offsetWidth > 0 || el.offsetHeight > 0
```

This will catch cases where the DOM element has `display:none` or if it's parent is hidden.  It will not cover the case where the DOM element is transparent or is occluded by some other element.

Since `BeVisible()` validates existence you do not need to have an `Eventually(selector).Should(b.Exist())` before checking `BeVisible()`

`BeVisible()` operates on the **first** element found by `selector`.  To assert that **every** matching element is visible use `EachBeVisible()` (it requires **at least one** match and that every match be visible - it fails, rather than passing vacuously, when nothing matches):

```go
Eventually(selector).Should(b.EachBeVisible())
```

---

To assert that an element can be interacted with use `BeEnabled()`:

```go
Eventually(selector).Should(b.BeEnabled())
```

This performs the following javascript as one atomic unit:

- query the selector and grab the first matching element
- fail if no element is returned (validate existence)
- check that the element is not disabled and return the result

Biloba's disabled check is simply:

```js
!el.disabled
```

As with `BeVisible()` you don't need to assert existence before asserting `BeEnabled()` - existence is implicitly validated by `BeEnabled()`

`BeEnabled()` operates on the **first** element found by `selector`.  To assert that **every** matching element is enabled use `EachBeEnabled()` (it requires **at least one** match and that every match be enabled - it fails, rather than passing vacuously, when nothing matches):

```go
Eventually(selector).Should(b.EachBeEnabled())
```

---

`BeVisible()` checks that an element has a non-zero footprint - but an element can be visible and still be impossible to actually click: another element might be sitting on top of it (a modal overlay, a sticky header, a transparent catch-all), or it might be scrolled out of view.  To assert that an element could really receive a click use `BeClickable()`:

```go
Eventually(selector).Should(b.BeClickable())
```

This performs the following javascript as one atomic unit:

- query the selector and grab the first matching element
- fail if no element is returned (validate existence)
- fail if the element is not visible or is disabled
- compute the element's center point and check that `document.elementFromPoint(...)` returns that element (or a descendant) - i.e. nothing is covering it - and that the center lies within the viewport

Because `elementFromPoint` is synchronous this stays a single atomic check with no extra roundtrips, and - true to Biloba's [pragmatism](#pragmatism-how-biloba-interacts-with-the-dom) - it is deterministic and fails fast: it does **not** wait for animations to settle.

`BeClickable()` is a guard you opt into.  Biloba's plain `Click()` does **not** run this check - it dispatches `el.click()` directly and will happily click an element that is covered or off-screen (that's the fast, pragmatic default).  Use `BeClickable()` when you specifically want to catch occlusion, or use [realistic interactions](#realistic-interactions) when you want a click that actually routes around it.

### Contents and Classes

You can get the `innerText` of an element with `GetInnerText()`:

```go
text := b.GetInnerText(selector) //returns string
```

Like all of Biloba's value-getters, `GetInnerText` [polls](#interacting-with-elements): it waits until an element matching `selector` is present, then returns its `innerText` (an empty `innerText` is a perfectly valid result - it does not wait for the text to become non-empty).  If the element never appears it times out and fails the spec.  If you want to make an assertion on the text - and, especially, if you want to poll until the text matches an assertion - use `HaveInnerText()`:

```go
Eventually(selector).Should(b.HaveInnerText("Expected text goes here"))
```

you can pass `b.HaveInnerText` a string to require an exact match, or an appropriate Gomega Matcher:


```go
Eventually(selector).Should(b.HaveInnerText(ContainSubstring("text")))
Eventually(selector).Should(b.HaveInnerText(HavePrefix("Expected")))
//etc...
```

Both `HaveInnerText` and `GetInnerText` always operate on the **first** element matching `selector.

`HaveInnerText` requires an _exact_ match (modulo any Gomega matcher you provide).  This can be annoying when templating introduces incidental whitespace - leading/trailing spaces, newlines, or runs of spaces that you don't care about.  For those cases reach for `HaveText()`, which trims and collapses all internal whitespace runs down to single spaces _before_ matching:

```go
//if the element's innerText is "\n  Hello   there\n\n  Biloba!\n"
Eventually(selector).Should(b.HaveText("Hello there Biloba!")) //passes
Eventually(selector).Should(b.HaveText(ContainSubstring("there Biloba"))) //passes
```

Like `HaveInnerText`, `HaveText` accepts either a string (for an exact, post-normalization match) or a Gomega matcher, and operates on the **first** element matching `selector`.

`innerText` reflects the _rendered_ text - it depends on layout and CSS - which is exactly what you want when you're asserting on what the user actually sees.  But it has a sharp edge in headless Chrome: because it's computed from layout, freshly-added or just-changed dynamic content can come back stale or partial before a paint settles - and an `Eventually(...).Should(b.HaveInnerText(...))` can then spin until it times out even though the content is plainly in the DOM.  For those cases reach for `GetTextContent()`/`HaveTextContent()` (and `CurrentTextContentForEach()`/`EachHaveTextContent()`), which read the element's `textContent` instead - computed straight from the DOM tree, so it's layout-independent and robust against that timing:

```go
text := b.GetTextContent(selector) //returns string
Eventually(selector).Should(b.HaveTextContent("Expected text goes here"))
Eventually(selector).Should(b.HaveTextContent(ContainSubstring("text")))
```

The tradeoff: `textContent` is _not_ the rendered text.  It includes the text of hidden elements and of `<script>`/`<style>` tags, does not collapse whitespace, and does not reflect CSS `text-transform`.  So reach for `GetInnerText`/`HaveInnerText` (or `HaveText`) when you specifically want the visible, normalized text, and reach for `GetTextContent`/`HaveTextContent` (or a plain existence assertion) when you want a robust check on dynamic content.  The whole `TextContent` family mirrors the `InnerText` family exactly, including the `ForEach`/`Each` variants below.

You can fetch the content for a bunch of elements simultaneously with `CurrentInnerTextForEach()`:

```go
texts := b.CurrentInnerTextForEach(selector) // returns []string
```

returns a slice of strings for all elements matching selector.  For example:

```go
list := b.CurrentInnerTextForEach("ol.movies li")
```

will return the individual inner texts for each list element under all `<ol>`s with class `movies`.  If no elements are found `list` will be an empty slice.  The `Current*ForEach` getters are **snapshots** - they read the matches as they are *right now* and never poll.  When the elements appear asynchronously, gate on their count first with `Eventually("ol.movies li").Should(b.HaveCount(n))` and *then* read - but be careful to avoid flakes here as the DOM may have changed between the two atomic operations. 

You can assert on the set of inner texts with `b.EachHaveInnerText()` like so:

```go
Expect(selector).To(b.EachHaveInnerText("A", "B", "C")) //uses Gomega's HaveExactElements matcher to assert the texts match, in order
Eventually(selector).Should(b.EachHaveInnerText(ContainElement("B"))) //passes the entire slice to the matcher
```

use `b.EachHaveInnerText` with `Eventually` in lieu of `CurrentInnerTextForEach` if you want to poll and assert that the inner texts of these DOM elements eventually match your expectation - this approach gives you an atomic operation that is less susceptible to flakiness.

Like every `Each*` matcher, `EachHaveInnerText` requires **at least one** match: it fails (rather than passing vacuously) when nothing matches, which keeps it honest under `Eventually`/`Consistently`.  To assert that *nothing* matches a selector, use `Eventually(selector).Should(b.HaveCount(0))` (or `ShouldNot(b.Exist())`) instead.

**Two text-assertion recipes worth knowing**:

- *The ordered collection of an element group's text* - assert the visible text of every match, in document order - is exactly `EachHaveInnerText` with a slice (or `EachHaveTextContent` for the layout-independent variant): `Expect(".step").To(b.EachHaveInnerText("Pick", "Pay", "Done"))`.
- *Negation - "no element (in this scope) says X"* - is cleanest as a [text locator](#selecting-by-locator) + `ShouldNot(b.Exist())`, rather than a JS scan.  Scope it with `.Within` when you only care about a region:

  ```go
  Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())
  ```

---

You can assert that an element has a given set of CSS classes using the `HaveClass()` matcher.  You can either pass `HaveClass` a Gomega matcher or a string.  The matcher will receive the entire list of classes associated with the object as a slice of strings.  That means you can do things like:

```go
Expect(selector).To(b.HaveClass(ConsistOf("blue", "heading", "published")))
Expect(selector).To(b.HaveClass(ContainElements("blue", "heading")))
```

When passed a single string:

```go
Eventually(selector).Should(b.HaveClass("published"))
```

the behavior is equivalent to:

```go
Eventually(selector).Should(b.HaveClass(ContainElement("published")))
```

i.e. the class list should include `published`.  `HaveClass` always operates on the **first** element found by `selector`.

To assert that **every** matching element has a given class use `EachHaveClass(string)` (it requires **at least one** match and that every match have the class - it fails, rather than passing vacuously, when nothing matches):

```go
Eventually(selector).Should(b.EachHaveClass("published"))
```

---

A handful of additional matchers cover common assertions on the **first** element matching `selector`:

`HaveAttribute()` asserts on an element's HTML _attribute_ (via `getAttribute`).  This is distinct from `HaveProperty` ([below](#properties)), which asserts on a javascript _property_ - the two frequently diverge (e.g. the `href` attribute is the raw `"/about"` whereas the `href` property is the resolved absolute URL).  Pass just a name to assert the attribute exists, or a name and an expected value (string or Gomega matcher):

```go
Eventually(selector).Should(b.HaveAttribute("href")) //the attribute is present
Eventually(selector).Should(b.HaveAttribute("href", "/about")) //exact value
Eventually(selector).Should(b.HaveAttribute("href", HaveSuffix("about"))) //matcher
```

When you want an attribute value in a Go variable for *control-flow* (rather than to assert on) use the getter `b.GetAttribute(selector, name)` - the attribute sibling of [`b.GetProperty`](#properties).  It returns the raw markup attribute as type `any`:

```go
href := b.GetAttribute("#link", "href") // "/about" - the raw attribute, not the resolved property
theme := b.GetAttribute("html", "data-theme")
```

Like the property getters, `GetAttribute` is a **two-axis** poller: it waits until an element matching `selector` is present **and** the named attribute is present, then returns it.  If you want an *absent* attribute to come back as `nil` rather than blocking the poll, wrap the name in [`b.AllowMissing`](#properties):

```go
b.GetAttribute("#link", b.AllowMissing("data-role")) // nil if data-role isn't set, no waiting
```

To fetch several attributes from one element at once use `b.GetAttributes(selector, names...)`, which returns a [`Properties`](#properties) map (and polls the same way, with `AllowMissing` available per name):

```go
a := b.GetAttributes("#link", "href", "data-role")
a.GetString("href") // "/about"
```

`b.CurrentAttributeForEach(selector, name)` is the **snapshot** for-each variant: it returns a `[]any` with the attribute for every matching element *right now* (`nil` entries where the attribute is absent, an empty slice when nothing matches) - mirroring `b.CurrentPropertyForEach`.  Its plural sibling `b.CurrentAttributesForEach(selector, names...)` returns a [`SliceOfProperties`](#properties).  Neither polls (there is no `AllowMissing` axis), so gate on a count first when the elements appear asynchronously:

```go
Expect(b.CurrentAttributeForEach(".notice", "data-name")).To(HaveExactElements("henry", "bob", BeNil()))
```

`BeChecked()` asserts that a checkbox or radio button is checked (it's sugar for `b.HaveProperty("checked", true)`):

```go
Eventually("input[type='checkbox']").Should(b.BeChecked())
Eventually("input[type='radio']").ShouldNot(b.BeChecked())
```

`BeFocused()` asserts that the element is the document's `activeElement`:

```go
Eventually(selector).Should(b.BeFocused())
```

`HaveComputedStyle(property, expected)` asserts on the element's computed CSS style (via `getComputedStyle`).  Biloba's notion of visibility is deliberately pragmatic (non-zero `offsetWidth`/`offsetHeight`) - when you need to assert on an explicit style use `HaveComputedStyle`.  `expected` can be a string or a Gomega matcher:

```go
Eventually(selector).Should(b.HaveComputedStyle("display", "none"))
Eventually(selector).Should(b.HaveComputedStyle("color", "rgb(255, 0, 0)"))
Eventually(selector).Should(b.HaveComputedStyle("color", ContainSubstring("255")))
```

### Properties

Biloba provides a bunch of methods for getting, setting, and asserting on properties:

You use `GetProperty/SetProperty/HaveProperty` to work with a **single** property on a **single** element (the first returned by `selector`).  You use `CurrentPropertyForEach/SetPropertyForEachImmediately/EachHaveProperty` to work with a **single** property for **all** elements matching `selector`.  You use `GetProperties` to fetch **multiple** properties for a **single** element and `CurrentPropertiesForEach` to fetch **multiple** properties for **all** elements matching `selector`.

All of these methods follow the following rules:

- The **single**-element getters and setters (`GetProperty`, `GetProperties`, `SetProperty`) [poll](#interacting-with-elements).  The getters poll until the element is present *and every named property is defined* (see [`AllowMissing`](#properties) below for the escape hatch); `SetProperty` polls until the element exists and the property is settable.  They fail the spec only if the deadline passes.
- The plural `Current*ForEach` getters are **snapshots** - they read whatever matches *right now* and never poll.  They return an empty slice if no element matches; otherwise a slice matching the number of elements found, with `nil` standing in for any element that lacks the requested property.  Gate on a count first (`Eventually(sel).Should(b.HaveCount(n))`) when the elements appear asynchronously.
- `SetPropertyForEachImmediately` acts on the current set immediately (no poll, no matcher form) - its `*Immediately` suffix is a deliberate "make sure you mean it" smell.
- All methods support `.` property delimiters.  For example you can access `data` attributes using `dataset.key`.  `Set*` methods will fail if the delimiter chain cannot be traversed (e.g. setting `foo.bar.baz` fails if either `foo` or `bar` are not defined on the element.  But `dataset.newKey` will succeed as `dataset` _is_ defined).  The snapshot `Current*ForEach` getters do not fail, but simply return `nil` if the delimiter chain cannot be traversed.
- All properties are returned from JavaScript without type conversions: numbers will be `float64`, booleans will be `bool`, and strings will be `string`.  Arrays will be `[]any` and maps `[any]any`.  Anything `null`/`undefined` will be `nil`.  There are, however, two exceptions:
	- JavaScript properties that are [iterable](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Iteration_protocols) will be turned into `[]any` when returned (this allows `GetProperty(selector, "classList")` to return a slice).  
	- JavaScript properties of type `DOMStringMap` will be turned into `map[any]any` - this allows you get all `data` attributes via `GetProperty(selector, "dataset")`.

Let's show these in use to cover some additional nuances.  You can get any JavaScript property defined on an element via `GetProperty()` for example:

```go
property := b.GetProperty(selector, "href") //returns type any
```

this returns the property value of the **first** element matching `selector`.  The value will have type `any` and the actual type will depend on what was stored in the property in JavaScript.  `GetProperty` polls: it waits until an element matching `selector` is present **and** the requested property is defined, then returns it (failing the spec only if the deadline passes).

That "and the property is defined" axis has a **sharp edge** worth knowing: a property that simply doesn't exist on the element *type* - asking for `disabled` on a `<div>`, say, where `"disabled" in div` is `false` - never becomes defined, so the poll will block until it times out.  When you genuinely expect a property may be absent, wrap its name in [`b.AllowMissing`](#properties) (below) to get the old return-`nil`-immediately behavior:

```go
b.GetProperty("div.comment", b.AllowMissing("dataset.poster")) // nil if it's absent, no waiting
```

You can fetch subproperties using `.` notation:

```go
property := b.GetProperty(selector, "dataset.name") //returns any
```

will return the `data-name` attribute defined on the element.

To make assertions on properties, and to poll, use the `HaveProperty` matcher.  There are two ways to use `HaveProperty` - to check that the property is defined at all simply provide the property:

```go
Eventually(selector).Should(b.HaveProperty("href"))
Eventually(selector).ShouldNot(b.HaveProperty("dataset.name"))
```

To assert on the _value_ of the property, you can pass in a second argument. If you pass in a Gomega matcher, the returned property value will be matched against it (which can be a convenient way to not have to worry about types and let Gomega deal with them for you).  Alternatively you can pass in anything else to perform a `DeepEqual` match:

```go
Eventually(selector).Should(b.HaveProperty("href", HaveSuffix("toc.html"))) //an assertion on a string
Eventually(selector).Should(b.HaveProperty("hidden", BeFalse())) //an assertion on a bool
Eventually(selector).Should(b.HaveProperty("classList", HaveKeyWithValue("0", "blue"))) //an assertion on a map
Eventually(selector).Should(b.HaveProperty("dataset.name", "henry")) // an assertion on a string
Eventually(selector).Should(b.HaveProperty("dataset.missing", BeNil())) // an assertion on an undefined property
```

You can also set properties with `b.SetProperty()`.  When passed three arguments `b.SetProperty` acts (and, like Biloba's other actions, [polls by default](#interacting-with-elements) until the element exists and the property is settable):

```go
b.SetProperty(selector, "href", "http://www.example.com/")
b.SetProperty(selector, "dataset.name", "Bob")
```

when passed two arguments, it returns a matcher that you can poll yourself:

```go
Eventually(selector).Should(b.SetProperty("dataset.name", "George"))
```

`SetProperty` fails if a delimited property (e.g. `foo.bar.baz`) can't be accessed.

To operate on every element returned by the `selector` use `CurrentPropertyForEach/EachHaveProperty/SetPropertyForEachImmediately`.  Here's how they work:

```go
b.CurrentPropertyForEach(".notice", "id")
```

will return a **snapshot** **slice** of type `[]any` that contains the `id` property of all elements matching the `.notice` selector *right now* (no polling).  If no elements are found, an empty slice is returned.  If any elements don't have the requested property, the value in the slice for that element will be `nil`.  You can make assertions on the returned value like so:

```go
Expect(b.CurrentPropertyForEach(".notice", "id")).To(HaveExactElements("A", BeNil(), "C"))
Expect(b.CurrentPropertyForEach(".notice", "dataset.name")).To(ContainElement("Bob"))
Expect(b.CurrentPropertyForEach(".does-not-exist", "foo")).To(BeEmpty())
```

(note that you must use `BeNil` instead of `nil` in Gomega's collection matchers)

Alternatively, you can use `EachHaveProperty` to make an assertion directly and/or to poll:

```go
//assert that every .notice has a dataset.name defined on it
Eventually(".notice").Should(b.EachHaveProperty("dataset.name"))

//require an exact match - note that you can specify nil to assert that an element does not have this property
Eventually(".notice").Should(b.EachHaveProperty("dataset.name", "Bob", "George", nil, "John"))

//use a matcher - this ensures that there are is at least a .notice with name Bob and one with name George
Eventually(".notice").Should(b.EachHaveProperty("dataset.name", ContainElements("Bob", "George")))

//if you don't care about order, use ConsistOf 
Eventually(".notice").Should(b.EachHaveProperty("dataset.name", ConsistOf(BeNil(), "John", "Bob", "George")))

// if you want all attribute values to be the same, use Gomega's `HaveEach`:
Eventually(".notice").Should(b.EachHaveProperty("disabled", HaveEach(BeFalse())))
```

You can use `SetPropertyForEachImmediately` to set the specified property to the specified value for **all** matched elements.  Since we're pointing at the set of _all_ elements matched by a selector it acts immediately on the current set rather than polling (hence the `*Immediately` suffix), and so has no matcher variant.  You can use it like this:

```go
b.SetPropertyForEachImmediately(b.XPath("li").WithText("Seventeen"), "count", 17)
b.SetPropertyForEachImmediately(".notice", "dataset.name", "John")
```

Now all elements matching `<li>Seventeen</li>` will have a `count` property set to `17`; and all elements with class `notice` will have a `name` data attribute with value `John`.  If no elements match... nothing happens.  The only way `SetPropertyForEachImmediately` fails is if you provide a delimited property that it cannot traverse (e.g. `foo.bar.baz` - if either `foo` or `bar` do not already exist).

Often it can be more convenient, and efficient, to work with multiple properties at once.  You can do this with `GetProperties` (polling) and `CurrentPropertiesForEach` (snapshot).  Unlike the other property-related methods in this section these return type `biloba.Properties` and `biloba.SliceOfProperties` to help with managing types (which can quickly get unwieldy when you're working with `[]map[string]any`).

You use `GetProperties` to get multiple properties for a `selector` at once:

```go
props := b.GetProperties(".notice", "classList", "tagName", "disabled", "offsetWidth", "dataset.name")
```

Like `GetProperty`, this polls until an element matches `selector` and every requested property is defined (wrap names you expect may be absent in [`b.AllowMissing`](#properties), and recall the [sharp edge](#properties) above - a property like `disabled` that doesn't exist on the element type would otherwise block the poll).  The object returned, `props`, will have `type Properties map[string]any` - you can access defined properties with map notation: e.g. `props["classList"]`.  However this will always return type `any`.  You can, instead, use `Properties`' various getters to force a type conversion:

```go
props.GetString("tagName") //returns a string
props.GetInt("offsetWidth") //returns an integer
props.GetFloat64("offsetWidth") //returns a float64
props.GetBool("disabled") //returns a bool
props.GetStringSlice("classList") //returns []string - any `nil` entries in the original []any slice are converted to the empty string ""
```

all of these always return the zero or empty value if the requested property does not exist or came back as `nil` from JavaScript.  e.g. `props.GetFloat64("offsetHeight")` will return `0.0` in our example since we did not request `offsetHeight` in our call to `GetProperties`.  If you choose the wrong type, Biloba will panic - which Ginkgo will catch and fail the test.

Lastly, to fetch multiple properties from multiple elements use the snapshot for-each getter:

```go
propsForEach := b.CurrentPropertiesForEach(".notice", "classList", "tagName", "disabled", "offsetWidth", "dataset.name")
```

here `propsForEach` is `type SliceOfProperties []Properties` and will have zero length if no elements are found (it reads the current set and does not poll - gate on a count first when the elements appear asynchronously).  You can, of course, use index notation to access a particular property and then fetch a particular key: `propsForEach[0].GetString("tagName")` **or** you can generate a typed slice of a particular key for all elements:

```go
propsForEach.GetString("tagName") //returns a []string
propsForEach.GetInt("offsetWidth") //returns a []integer
propsForEach.GetFloat64("offsetWidth") //returns a []float64
propsForEach.GetBool("disabled") //returns a []bool
propsForEach.GetStringSlice("classList") //returns [][]string
```

if you ask for a key that isn't defined you'll get the empty slice of the relevant type:

```go
propsForEach.GetBool("missing") //returns []bool{}
```

also - while constructing the slice - `SliceOfProperties` calls the relevant type-getter on `Properties`.  that means `nil` values are turned into their zero/empty value in the slice.  For example, say our DOM looks like this:

```html
<div class="notice" id="new-user-1">Hi Newcomer</div>
<div class="notice extrovert" id="jane-127", data-name="jane">Hey Jane!</div>
<a class="notice introvert" id="molly-4" data-name="molly" href="nod.html">Sup</a>
```

then the following will succeed:

```go
p := b.CurrentPropertiesForEach(".notice", "id", "classList", "tagName", "data.name", "href")
Expect(p.GetString("tagName")).To(Equal([]string{"DIV", "DIV", "A"}))
Expect(p.GetString("data.name")).To(Equal([]string{"", "jane", "molly"})
Expect(p.GetString("id")).To(Equal([]string{"new-user-1", "jane-127", "molly-4"})
Expect(p.GetString("href")).To(Equal([]string{"", "", "nod.html"})
Expect(p.GetStringSlice("classlist")).To(Equal([][]string{{"notice"}, {"notice", "extrovert"}, {"notice", "introvert"}})
```
of course you can also use Gomega's collection matchers which obviate the need for all this type extraction.  if you use `p.Get` you'll get `[]any`:

```go
Expect(p.Get("classList")).To(ContainElement(ConsistOf("notice", "extrovert")))
```

You can look up the first element whose key matches a value or matcher, or filter the :

```go
p.Find("id", "jane-127") //returns the matching `Properties`
p.Find("id", ContainSubstring("new-user")) //returns the matching `Properties`
p.Filter("tagName", "DIV") //returns `SliceOfProperties` containing only elements that match
p.Filter("id", Not(ContainSubstring("new-user"))) //returns `SliceOfProperties` containing only elements that match
```

`Find` returns the matching `Properties` object or `nil` if none is found; `Filter` returns `SliceOfProperties` with matching elements (possibly empty if none matched).  This lets you fetch all the properties you might need to assert on and then efficiently dig through the `SliceOfProperties` in your test to make assertions.

### Geometry

Some specs need to assert on **layout**: where an element ended up, how far it sits from the top of a scroll container, whether a panel scrolled to the bottom.  The temptation is to reach for `b.Run` and a hand-rolled `getBoundingClientRect()` blob — but that read happens *once*, and layout settles asynchronously, so it's the single most common residual flake source (see [`biloba:flaky-specs`](#claude-code-skills)).  Biloba's geometry getters fold readiness in and poll by default, exactly like [`GetProperty`](#properties): they wait until the element is present **and actually laid out** (a non-degenerate box, `width` and `height` > 0) before reading, so you never measure a zero box mid-layout.

`b.BoundingBox(selector)` returns the first match's viewport-relative `Box` (`Top`, `Left`, `Width`, `Height`, `Bottom`, `Right`, `CenterX`, `CenterY` — all CSS pixels):

```go
box := b.BoundingBox(".hero .sec")
Ω(box.Width).Should(BeNumerically("==", 320))
```

`b.ScrollOffset(selector)` treats the match as a scroll container and returns its `ScrollOffset` (`Top`, `Left`, plus `MaxTop`/`MaxLeft`, the largest reachable offsets — so `Top == MaxTop` means "scrolled to the bottom").

`b.OffsetTopWithin(selector, container)` returns how far the element's top sits below the container's top — `element.top - container.top` — which is the measurement a "scrolled near the top of the pane" spec actually wants.  `b.OffsetLeftWithin` is its horizontal sibling.

Like the rest of the [dual API](#interacting-with-elements), each getter polls-and-reads-once and has a **matcher** counterpart you hand to `Eventually` when you want to assert on geometry that settles asynchronously — this is the form to reach for when the value is converging:

```go
// poll until the box is laid out and satisfies a sub-matcher (compose with Gomega's HaveField):
Eventually(".hero .sec").Should(b.HaveBoundingBox(HaveField("Top", BeNumerically("<", 120))))
Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 0))))

// poll until the element settles near the top of its scroll container:
Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120)))
```

`HaveOffsetTopWithin`/`HaveOffsetLeftWithin` take the container plus an expected matcher (or a plain value, compared with `Equal`).  All of the getters honor `WithTimeout`/`WithPolling`/`WithContext` and `Immediate()`; the matcher forms are configured through the `Eventually`/`Expect` that polls them.

> A geometry poll that times out *consistently* under load — not intermittently — usually means the **product** computed a position once and never reconciled, not a test that needs a wider timeout.  The DOM you're polling is real, but if the page never re-runs the computation `Eventually` can't save you: the value is stably wrong.  The [poll trajectory](#outline) attached on failure is the tell — a flat line is a product bug, a monotone approach is latency, a dip-then-rebound is a late reflow.

### Form Elements

Biloba provides three methods to help you get and set the values of input elements. `b.GetValue` gets values, `b.SetValue` sets values, and `b.HaveValue` matches against values.  All three operate on the **first** element that matches their `selector`.

For most input elements you use them like this:

```go
val := b.GetValue("#my-text-input")
b.SetValue("#my-text-input", "your new value")
Eventually("#my-text-input").Should(b.HaveValue("some other value"))
Eventually("#my-text-input").Should(b.HaveValue(ContainSubstring("other")))
```

`GetValue` [polls](#interacting-with-elements) until it finds a DOM element matching the selector, then returns that element's value even if the element is hidden or disabled (an empty value is a valid result - it does not wait for the value to become non-empty).  Similarly, `HaveValue` will fail to match if it can't find an element - but will proceed if the element is hidden or disabled.  There is also a snapshot for-each getter, `b.CurrentValueForEach(selector)`, which returns a `[]any` of the values for every matching element *right now* (no polling).

`SetValue`, on the other hand, requires that the element exist, be visible, and be enabled - and, like Biloba's other actions, it **polls by default** until all three hold:

```go
b.SetValue("#my-temporarily-hidden-numeric-input", 3) // waits until the input is present, visible, and enabled, then sets it
```

You can also use `b.SetValue` _as a matcher_ - drop the value's selector and poll it yourself:

```go
Eventually("#my-temporarily-hidden-numeric-input").Should(b.SetValue(3))
```

If `SetValue` has two arguments it acts (polling by default); if it has one argument it returns a matcher that you drive with `Eventually`/`Consistently`.  (If you specifically want the old act-once behavior - fail immediately when the input isn't ready this instant - reach for `b.Immediate().SetValue(...)`.)

> What about types?  I see you sending both strings and integers to SetValue.

Good question.  `SetValue` will take most things and translate them to valid javascript to send to the input element.  You can stick with strings if you'd like, but you don't need to.

Similarly, `GetValue` returns `any`.  That means you'll need to do a type-check or type-assertion if you want to use the return value directly:

```go
val := b.GetValue("#my-text-input").(string)
```

but you probably won't need to as you can just use `b.HaveValue` and let Gomega manage the types for you:

```go
Expect("#my-text-input").To(b.HaveValue(WithPrefix("hello")))
```

> Why doesn't `GetValue` just always return `string`.  After all, these are input elements.  Their values are always strings.

Not quite.  And this is where `GetValue`, `SetValue`, and `HaveValue` do some extra lifting to try to make your life easier.

In general, most input types take and return strings (e.g. `text`, `numeric`, `color`, `date`, `email`, etc... inputs).  `textarea`s do as well.  As do `select` elements that don't have `multiple` set.  For all these you can `GetValue`, `SetValue`, and `HaveValue` with strings.  (Recall that the value of a `select` element is `value` attribute on the selected `option`).

Because the value of a `<select>` is the `value` attribute of the selected `<option>` - and **not** its visible label - `b.SetValue("#model", "claude-sonnet-4-6")` matches on the option value.  When it's more natural to pick by the label the user sees, wrap the argument in `b.ValueLabel`:

```go
b.SetValue("#model", b.ValueLabel("Sonnet")) // selects the <option> whose visible text is "Sonnet"
```

`ValueLabel` works for single- and multi-select elements (for a `<select multiple>` pass a slice whose entries are `ValueLabel`s; you may mix labels and raw values).  To read a label back, assert on the option's `textContent`; to read the selection, assert on the `<select>`'s `value` property.

But `checkboxes`, `radio` buttons, and `<select multiple>` elements all behavior differently.  Biloba rationalizes all these differences for you through `GetValue`/`SetValue`/`HaveValue`

When Biloba sets a value it does the following:
- focus the element
- update its value (either by setting `el.value` or `el.checked` or `el.selected` etc.)
- dispatch an `input` event
- dispatch a `change` event

`SetValue` updates `value` through the element's _native_ prototype setter rather than a plain `el.value = v`.  That matters for **controlled** React/Vue/Solid inputs, whose `value` is bound to component state: a raw assignment gets reconciled away by the framework's value tracker (the change looks fake), whereas the native setter makes it look genuine so `onChange` fires and state updates.  You don't need to make an input uncontrolled for Biloba's sake.

For text inputs `SetValue` focuses the element and dispatches `input`/`change`, but it does **not** blur the element afterwards.  So an `onBlur` handler - commit-on-blur, an inline editor that unmounts on blur - will **not** fire as a side effect of `SetValue`.  When you _do_ want that, pair it with [`b.Blur`](#hovering-focusing-and-scrolling): `b.SetValue("#name", "New"); b.Blur("#name")` (the text input is left focused, so the blur fires).  (The `<select>` path still blurs - that's load-bearing for its `change` semantics.)

That should get _most_ web applications to realize that a form input has been set.  Some applications, though, are wired up to real keyboard events (search-as-you-type fields, rich-text editors, hotkeys).  `SetValue` does **not** fire `keydown`/`keypress`/`keyup` - it sets the value directly.  For those cases reach for [Keyboard Input](#keyboard-input) (`b.Type` and `b.SendKeysToWindowImmediately`), which dispatch genuine key events.

#### Working with Checkboxes

When `selector` refers to a checkbox:

- `b.GetValue(selector)` returns a boolean denoting whether or not the checkbox is selected
- `b.SetValue(selector, true/false)` and `Expect(select).To(b.SetValue(true/false))` will check the box if passed `true` and uncheck the box if passed `false`.  If you want to toggle the box use `b.Click(selector)` instead.
- `b.HaveValue()` will receive a boolean.  So you can use `b.HaveValue(true)` or `b.HaveValue(BeTrue())` to assert the box is checked (and `false`/`BeFalse()` to assert it is not checked).

If you want to set/get the Checkboxes' `intermediate` property - or if you prefer to work with the `checked` property directly, use `b.GetProperty/b.SetProperty/b.HaveProperty`.


### Working with Radio buttons

Radio buttons are a bit trickier.  Recall that the browser groups radio buttons by their `name` attribute and that the value of the radio button is associated with its `value` attribute and whether or not the radio button is selected is determined by its `checked` property.

When `selector` refers to a radio button (any radio button in a given radio button group) then:

- `b.GetValue(selector)` returns the `value` attribute of the `checked` radio button in the named group associated with `selector`
- `b.SetValue(selector, "value")` and `Expect(select).To(b.SetValue("value"))` will check the radio button in the associated named group that has value "value".
- `b.HaveValue()` will receive the `value` attribute of the `checked` radio button.

Let's look at a concrete example.  Let's say you've got:

```html
<input type="radio" name="jedi" value="luke" id="luke"><label for="luke">Luke</label>
<input type="radio" name="jedi" value="yoda" id="yoda" checked><label for="yoda">Yoda</label>
<input type="radio" name="jedi" value="obi-wan" id="obi-wan"><label for="obi-wan">Obi-Wan</label>
```

Then you can interact with this group of radio buttons like so:

```go
val := b.GetValue("[name='jedi']") //val will be "luke"
Expect("[name='jedi']").To(b.HaveValue("luke"))
Expect("#yoda").To(b.HaveValue("luke")) // #yoda refers to a radio button in the 'jedi' group, Biloba will find the value of the checked checkbox in the group and return it

//we can use have property to validate that luke is checked and obi-wan is not
Expect("#luke").To(b.HaveProperty("checked", true))
Expect("#obi-wan").To(b.HaveProperty("checked", false))
Expect("[name='jedi']").To(b.SetValue("obi-wan")) //We set the value to obi-wan...
Expect("[name='jedi']").To(b.HaveValue("obi-wan")) //..so the value of the group is now obi-wan
//...and the undelrying checked properties have changed
Expect("#luke").To(b.HaveProperty("checked", false))
Expect("#obi-wan").To(b.HaveProperty("checked", true))
```

Note that the `HaveProperty("checked", ...)` calls are just there for illustrative purposes.  With Biloba you don't have to worry about managing the radio buttons individually - just reference the group and get/set it's value.

Lastly, `SetValue` will fail if the value does not exist (i.e. no matching radio button in the named group has the specified value) or if the radio button with the desired value is hidden or disabled.

### Working with Multi-Select Inputs

`<select multiple>` inputs can have more than one associated `<option>` `selected`.  Recall that each `<option>` has a `value` attribute that denotes its value.  Biloba handles multi-select inputs by returning and setting `[]string{}` values.  When `selector` refers to a `<select mulitple>` element:

- `b.GetValue(selector)` returns a `[]string` that contains the `value` attributes of all of the `selected` options.  If none are selected then `[]string{}` is returned
- `b.SetValue(selector, []string{"value1", "value2"})` and `Expect(select).To(b.SetValue([]string{"value1", "value2"}))` will ensure that exactly the options with values of `"value1"` or `"value2"` are selected.
- `b.HaveValue()` will receive the same `[]string` slice returned by `selector` and match against it.


Let's look at a concrete example.  Let's say you've got:

```html
<select id="away-team" mulitple>
	<option value="picard">Picard</option>
	<option value="riker" selected>Riker</option>
	<option value="geordi" selected>Geordi</option>
	<option value="data">Data</option>
</select>
```

Then you can interact with this group of radio buttons like so:

```go
Expect("#away-team").To(b.HaveValue(ConsistOf("riker", "geordi"))) // we use gomega's ConsistOf to make assertions on the return slice of values

b.SetValue("#away-team", []string{"picard", "riker"}) // we set a different selection
Expect("#away-team").To(b.HaveValue(ConsistOf("picard", "riker"))) // note that we didn't have to deselect Geordi - Biloba does that for us

b.SetValue("#away-team", []string{}) // we clear the selection
Expect("#away-team").To(b.HaveValue(BeEmpty())) // nothing is selected
```

One last note, `SetValue` will fail if it can't find an option with the specified value _or_ if the option it finds is `disabled`.

### Clicking on Things

You can click on elements with `b.Click()`.  If you run

```go
b.Click(selector)
```

Biloba [polls](#interacting-with-elements): it waits until the **first** element matching `selector` exists, is visible, and is enabled, and then it calls `element.click()`.  If the deadline passes before all three hold, `b.Click` fails the spec.

You can, alternatively, use `b.Click()` as a matcher:

```go
Eventually(selector).Should(b.Click())
```

this polls the browser until `selector` points to an element that exists, is visible, and is enabled - at which point the element is clicked (just once) and the assertion succeeds.  Reach for this form when you want to compose the polling yourself.

Because the fully-applied form already polls, you don't need a separate `Eventually` to wait for the page to load or the element to appear:

```go
b.Navigate("http://example.com/homepage")
b.Click("#login") // waits for #login to be clickable, then clicks
```

> **Driving a *cycling* control to a target state? Don't click inside an `Eventually` body.**  It's tempting to write `Eventually(func(g Gomega) { if state != want { b.Click("#toggle") }; g.Expect(state).To(Equal(want)) })` to push a multi-state toggle (a 3-way theme switch, a sort-direction cycler) into a particular state.  This is a footgun: `Eventually` re-runs its body every polling interval, so it **rapid-fires a click each poll** before the state has settled - bursting extra clicks that sail past your target and can land on unrelated UI mid-rerender.  The body of an `Eventually`/`Consistently` must be **idempotent**; a side effect that fires every poll is a bug.  The correct shape is *click once, then wait for the change before reconsidering*:
>
> ```go
> // click-until-condition: one effect, one wait, repeat
> for b.GetAttribute("html", "data-theme") != "dark" {
>     before := b.GetAttribute("html", "data-theme")
>     b.Click("#theme-toggle")
>     Eventually(func() any { return b.GetAttribute("html", "data-theme") }).ShouldNot(Equal(before))
> }
> ```
>
> Each iteration clicks exactly once and then blocks until the control actually advances, so you never burst clicks past your target.

You can also click on every element matching `selector` using:

```go
b.ClickEachImmediately(selector)
```

unlike `Click`, `ClickEachImmediately` acts on the current set immediately - it does not poll and has no matcher variant (hence the `*Immediately` suffix).  It clicks on all the matching elements that are also visible and enabled; elements that are not visible or enabled are silently skipped, and if nothing matches it's a no-op.  Gate it on a count first (`Eventually(selector).Should(b.HaveCount(n))`) when the elements appear asynchronously.

#### Pointer Options: Offsets and Modifiers

`Click`, `DblClick`, `RightClick`, `MiddleClick`, and `Tap` all accept **pointer options** after the selector.  Two kinds are available, and they compose:

```go
b.Click("#canvas", b.At(30, 40))            // click at a point offset from the top-left corner
b.Click("#row", b.Shift())                  // shift-click
b.Click("#row", b.Ctrl(), b.Meta())         // ctrl+meta-click
b.Click("#canvas", b.At(30, 40), b.Shift()) // both at once
```

`b.At(offsetX, offsetY)` targets a point measured in CSS pixels from the element's **top-left corner** (matching Playwright's `position` option), instead of the center.  `b.Shift()`, `b.Ctrl()`, `b.Alt()`, and `b.Meta()` hold a keyboard modifier down during the click (`Meta` is Command on macOS and the Windows key elsewhere) - these are the same modifier options you can hold during [keyboard input](#holding-modifiers-shift-enter-cmd-a).

Because options are a distinct type from selectors, they work in the **matcher form** too - just drop the selector, and `Eventually`/`Expect` supplies it:

```go
Eventually("#canvas").Should(b.Click(b.At(30, 40), b.Shift()))
```

**Clicking away (dismissing popovers/menus).**  To dismiss an open popover, dropdown, or menu the way a user does - by clicking empty space - `b.Click(sel, b.At(x, y))` *is* the blessed idiom: target a large background region (a layout gutter, the `<body>`, a backdrop) and click an offset that lands on the backdrop rather than on any interactive child.  This replaces the `b.Run("...dispatchEvent(new MouseEvent('click'))...")` reach-arounds:

```go
b.Click("body", b.At(5, 5))      // click the top-left gutter to close an open menu
Eventually("#menu").ShouldNot(b.BeVisible())
```

**A fidelity note.**  A plain `b.Click(sel)` calls `element.click()` - the most faithful path, because it fires the browser's native default actions (form submit, `<label>` association, and the like).  But `element.click()` carries no coordinates and no modifier state, so the moment you add *any* option the fast path switches to dispatching a synthetic `mousedown`/`mouseup`/`click` sequence carrying the real `clientX`/`clientY` and the modifier flags - so an app reading `e.clientX`/`e.shiftKey` (a `<canvas>` painter, a map, a custom slider) sees exactly what you targeted.  That's a deliberate fidelity-for-control trade: if you need a maximally-faithful click, pass no options (or use [realistic mode](#realistic-interactions), which always uses real CDP input and honors the options natively).  `Tap` ignores keyboard modifiers - they don't apply to touch - but honors `b.At`.

#### Double-Clicking and Right-Clicking

Alongside `Click`, Biloba provides `b.DblClick` and `b.RightClick`, following the same dual immediate/matcher convention, the same visible+enabled checks, and the same [pointer options](#pointer-options-offsets-and-modifiers):

```go
b.DblClick("#row")                       // fires two click events plus a dblclick
b.RightClick("#row")                     // fires mousedown/mouseup/contextmenu

Eventually("#row").Should(b.DblClick())  // matcher forms poll until clickable
Eventually("#row").Should(b.RightClick())
```

Like `Click` and `Hover`, the default (fast) versions are pragmatic simulations: `DblClick` calls `element.click()` twice and dispatches a `dblclick` event; `RightClick` dispatches `mousedown`/`mouseup`/`contextmenu` with the secondary button.  In [realistic mode](#realistic-interactions) both scroll into view, wait for the element to stop moving, refuse to click through an overlay, and dispatch real CDP mouse input (a genuine double-click and a genuine right-button click that fires the browser's native context menu event).

#### Dragging and Dropping

`b.DragTo` drags one element onto another:

```go
b.DragTo("#card", "#column")             // drags #card's center onto #column's center
```

`DragTo` is **pointer-based**: it drives a drag with `pointerdown`/`pointermove`/`pointerup` (plus the matching `mouse` events) from the source's center to the target's center.  This is what modern drag-and-drop libraries - [@dnd-kit](https://dndkit.com), Sortable, and friends - listen for, so `DragTo` exercises them.  In [realistic mode](#realistic-interactions) it scrolls both elements into view, checks their actionability, and drives the same drag with real CDP mouse input.

`DragTo` also has a **matcher form** whose subject is the *source*: pass only the target, and poll the source with `Eventually`.  This waits until both elements are present and the drag can be performed - folding the readiness wait into the action, instead of asserting both endpoints exist first:

```go
Eventually("#card").Should(b.DragTo("#column"))
```

`DragTo` does **not** drive native HTML5 drag-and-drop (the `draggable` attribute and the `dragstart`/`dragover`/`drop` event family).  Synthesizing the native protocol convincingly requires the real OS-level drag machinery, which is outside Biloba's atomic model.  If you need to test a native HTML5 `draggable` interaction, drop down to chromedp via `b.Context`.

#### Scrolling the Mouse Wheel

`b.ScrollWheel` scrolls the mouse wheel over an element:

```go
b.ScrollWheel("#scroll-box", 0, 200)     // scrolls down 200px
b.ScrollWheel("#scroll-box", 50, 0)      // scrolls right 50px
```

Positive `deltaY` scrolls down and positive `deltaX` scrolls right (the standard wheel convention).  The fast version dispatches a synthetic `wheel` event at the element's center (so the app's `wheel` handlers fire) and then - because synthetic wheel events don't actually move the page - manually scrolls the nearest scrollable ancestor, *unless* a handler called `preventDefault()` (mirroring how a real browser suppresses scrolling).  In [realistic mode](#realistic-interactions) it scrolls the element into view and dispatches a real CDP wheel event, which is genuine trusted input and so scrolls the page itself.

`ScrollWheel` is a dual method: when you drop the selector it returns a matcher you can poll, so you can fold readiness-waiting into the scroll:

```go
Eventually("#scroll-box").Should(b.ScrollWheel(0, 200))
```

It fails the spec if the element can't be found (or, in realistic mode, isn't actionable).

#### Middle-Clicking

`b.MiddleClick` middle-clicks (auxiliary-clicks) an element, following the same dual immediate/matcher convention, visible+enabled checks, and [pointer options](#pointer-options-offsets-and-modifiers) as `Click`:

```go
b.MiddleClick("#row")                       // fires mousedown/mouseup/auxclick

Eventually("#row").Should(b.MiddleClick())  // matcher form polls until clickable
```

The fast version dispatches `mousedown`/`mouseup`/`auxclick` events with the middle button (a middle click fires `auxclick`, not `click`).  In [realistic mode](#realistic-interactions) it scrolls into view, waits for stability, checks for occlusion, and dispatches a real middle-button click.

To shift-click, ctrl-click, and so on, hold modifiers with `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` - see [Pointer Options](#pointer-options-offsets-and-modifiers).

#### Tapping (Touch)

`b.Tap` taps (touches) an element, following the same dual immediate/matcher convention and visible+enabled checks as `Click`:

```go
b.Tap("#row")                       // dispatches a synthetic touch tap

Eventually("#row").Should(b.Tap())  // matcher form polls until tappable
```

The fast version simulates a touch tap at the element's center: it dispatches `pointerdown`/`pointerup` (with `pointerType: 'touch'`) and `touchstart`/`touchend` `TouchEvent`s, then a culminating `click` (a tap normally ends in a click).  In [realistic mode](#realistic-interactions) it scrolls into view, waits for stability, checks for occlusion, and dispatches a real CDP touch (`touchStart`/`touchEnd`) - genuine trusted touch input.

### Hovering, Focusing, and Scrolling

Alongside `Click`, Biloba provides a few more first-class interactions, all following the same dual immediate/matcher convention:

```go
b.Focus("input.search")          // focuses the first matching element (must be visible and enabled)
b.Blur("input.search")           // blurs the first matching element (fires its blur/onBlur handler)
b.Hover(".menu")                 // fires hover events at the first matching element (must be visible)
b.ScrollIntoView("#footer")      // scrolls the first matching element into view
```

Each also returns a matcher when called with no arguments, so you can poll:

```go
Eventually("input.search").Should(b.Focus())
Eventually("input.search").Should(b.Blur())
Eventually(".menu").Should(b.Hover())
Eventually("#footer").Should(b.ScrollIntoView())
```

`b.Blur` is handy for firing commit-on-blur handlers after a `SetValue` - `b.SetValue("#name", "New"); b.Blur("#name")` - since `SetValue` no longer blurs text inputs for you.  A blur event only fires if the element is actually focused; `SetValue` leaves the text input focused, so that pairing works.

`Hover` is, like `Click`, a pragmatic simulation: it synchronously dispatches the pointer/mouse events associated with hovering (`pointerover`, `mouseover`, `pointerenter`, `mouseenter`, `mousemove`).  This triggers JavaScript hover handlers - for example a menu that opens on `mouseenter` - but it does **not** activate CSS `:hover` styling, which only responds to a real pointer.  If you need to exercise CSS `:hover`, use [realistic interactions](#realistic-interactions) (or drop down to chromedp's input domain yourself).

### Selecting Text

A whole class of UIs - annotation tools, rich-text editors, "highlight a phrase → floating menu → Define" affordances - key off the user *selecting text*.  Biloba gives you first-class primitives for this so you don't have to hand-roll `document.createRange()` + `window.getSelection()`:

```go
b.SelectText("#passage")         // selects all of the element's text
b.SelectRange("#passage", 4, 9)  // selects characters [4, 9) - across the element's text nodes
b.ClearSelection()               // clears the active selection
```

Both `SelectText` and `SelectRange` produce a genuine `window.getSelection()` range - the same object the browser builds when a user drags across text - and then dispatch a `mouseup` on the element, since selection-driven toolbars typically open on `mouseup`.  `SelectRange`'s offsets count characters into the element's text content (start inclusive, end exclusive) and are mapped across nested text nodes, so selecting across a `<strong>` in the middle of a paragraph works as you'd expect.  It fails the spec if the range is out of bounds.

`SelectRange` covers offset-based selection; when you'd rather select a _word_ - say, to drive a "highlight a phrase → floating menu → Define" affordance - hand `SelectText` the substring instead.  It selects the first occurrence by default, and a `b.Occurrence(n)` option (1-based) disambiguates a word that repeats:

```go
b.SelectText("#passage", "fox")                    // selects the first "fox"
b.SelectText("#passage", "fox", b.Occurrence(2))   // selects the second "fox"
```

This saves you the hand-rolled `TreeWalker`/`Range` dance for "select the Nth occurrence of a word and fire the selection menu".  The matcher form **requires** an explicit `b.Occurrence(n)` so it can't be confused with the existing select-all immediate form (`b.SelectText("#passage")`):

```go
Eventually("#passage").Should(b.SelectText("fox", b.Occurrence(2)))
```

Like the other interactions they follow the dual immediate/matcher convention (`SelectRange`'s matcher form drops the selector):

```go
Eventually("#passage").Should(b.SelectText())
Eventually("#passage").Should(b.SelectRange(4, 9))
```

To assert on *what* ended up selected, read the selection back as a plain expression:

```go
b.SelectRange("#passage", 4, 9)
Eventually("window.getSelection().toString()").Should(b.EvaluateTo("quick"))
```

These are pragmatic simulations on both tracks (they build the range directly rather than dragging a real pointer); for a genuine pointer-drag selection drop to chromedp via `b.Context`.

### Realistic Interactions

Biloba's interactions are fast, atomic [pragmatic simulations](#pragmatism-how-biloba-interacts-with-the-dom) by default - and for the overwhelming majority of your specs that is exactly what you want.  But the realism Biloba trades away (occlusion, scroll-into-view, genuine CSS `:hover`) is occasionally the very thing you want to guard against regressing.  Rather than make you hand-roll chromedp for those cases, `b.Realistic()` gives you a view of the tab whose interactions are performed with **real Chrome DevTools Protocol input**:

```go
rb := b.Realistic()
rb.Click("#submit")                    // scrolls into view, refuses to click through an overlay, dispatches a real mouse click
Eventually(".menu").Should(rb.Hover()) // moves the real mouse, activating CSS :hover
```

`b.Realistic()` returns a `*Biloba` that shares this tab's Chrome connection and state - it's the *same tab*, just with `Click` and `Hover` routed through CDP.  The default `b` is untouched, so the rest of your suite keeps Biloba's fast behavior.  This is meant to be used per-spec, for the handful of "smoke tests" where realism matters; realistic interactions cost real CDP roundtrips and can reintroduce the timing sensitivity Biloba's atomic model avoids - that's the deliberate cost, which is why it's opt-in.

In realistic mode:

- **`Click`** scrolls the element to the center of the viewport, **waits for its box to stop moving**, verifies it is enabled and is the topmost element at its center point (so an occluding overlay or an off-screen element does **not** click through - the matcher form keeps polling, the immediate form fails the spec), moves the real pointer to it (so hover-gated clicks register), then dispatches a real `mousePressed`/`mouseReleased`.  This is the inverse of plain `Click`, which clicks the element directly regardless of what's on top of it.  Clicks through `>>>` same-origin iframe boundaries are translated to top-level viewport coordinates so the real mouse lands in the right place.  [Pointer options](#pointer-options-offsets-and-modifiers) are honored natively: `b.At(x,y)` retargets to the offset point (translated to the viewport and bounds-checked), and `b.Shift()`/etc. hold a real CDP modifier bitmask down.
- **`ClickEachImmediately`** clicks every matching element with real input, scrolling and re-measuring each in turn, and skipping any that are hidden, disabled, off-screen, or obscured.
- **`DblClick`** / **`RightClick`** / **`MiddleClick`** apply the same scroll/stability/occlusion machinery as `Click`, then dispatch a real double-click (two click sequences with an incrementing click-count, so Chrome fires a genuine `dblclick`), right-button click (firing the browser's native `contextmenu`), or middle-button click (firing `auxclick`) - all honoring pointer options.
- **`DragTo`** scrolls and measures stable, actionable points for *both* the source and target, then drives a real CDP pointer drag - press at the source, several interpolated moves toward the target, release at the target - so pointer-based drag-and-drop libraries see genuine pointer input (it still does not drive native HTML5 `draggable`).
- **`ScrollWheel`** scrolls the element into view, measures a stable, actionable point, then dispatches a real CDP wheel event there - genuine trusted input that actually scrolls the page (unlike the synthetic fast `ScrollWheel`, which dispatches a `wheel` event and then manually scrolls the nearest scrollable ancestor).
- **`Tap`** applies the same scroll/stability/occlusion machinery as `Click`, then dispatches a real CDP touch (`touchStart`/`touchEnd`) at the element's center (or `b.At` offset) - genuine trusted touch input (unlike the synthetic fast `Tap`, which dispatches touch/pointer events plus a `click`).
- **`Hover`** scrolls into view and moves the real mouse to the element's center, which - unlike the synthetic `Hover` - activates genuine CSS `:hover` (e.g. a menu that only appears via a `:hover` rule).
- **`SetValue`** drives form controls with real input: a text input is focused with a real click, cleared, and typed with real key events (then blurred to fire `change`); a checkbox is toggled with a real click (and left alone if it's already in the desired state).  Native pickers - radio groups, `<select>`, and multi-selects - fall back to the fast JS path, because they can't be driven by a real pointer (Playwright's `selectOption` sets them programmatically too).
- **`Type`** / **`SendKeysToWindowImmediately`** already use real CDP key events; in realistic mode they additionally scroll the element into view before typing.

All keep Biloba's dual immediate/matcher API (`rb.Click("#go")` vs `Eventually("#go").Should(rb.Click())`).  For anything else you can still [drop down to chromedp](#chromedp-breaking-the-fourth-wall) via `b.Context`.

#### Using realistic mode across a spec or a suite

`b.Realistic()` is the single mechanism for opting into realism; it composes at whatever scope you need:

- **One interaction** - call it inline; the handle is cheap to make: `b.Realistic().Click("#submit")`.
- **A whole spec** - grab a handle once and use it throughout: `rb := b.Realistic()`.
- **A group of specs** - swap the shared tab in a `BeforeEach`, gated on a Ginkgo [label](https://onsi.github.io/ginkgo/#spec-labels) so you can run or skip the realistic smoke tests as a set:

```go
var _ = Describe("checkout (realistic smoke)", Label("realistic"), func() {
    var rb *biloba.Biloba
    BeforeEach(func() { rb = b.Realistic() })

    It("rejects a click through the cookie banner", func() {
        rb.Click("#purchase")
        // ...
    })
})
```

Then `ginkgo --label-filter='realistic'` runs only the realistic lane and `--label-filter='!realistic'` skips it (handy for keeping the slow/flake-prone realism checks out of the fast inner loop).

#### The interaction capability matrix

Biloba's interactions run on two tracks: the fast default (`b`) is an atomic Javascript simulation optimized for speed and stability; the realistic track (`b.Realistic()`) drives real Chrome DevTools Protocol input.  This table is the honest contract - what each track actually does, and where the fast track deliberately is *not* realistic.  (Selection - `b.ByRole`/`ByText`/`ByLabel`, CSS, XPath, `>>>` - is track-agnostic: it works identically through either handle.)

| Interaction | Fast track (default `b`) | Realistic track (`b.Realistic()`) |
|---|---|---|
| `Click` | visible + enabled, then `el.click()` - **clicks through overlays and off-screen elements** | scroll into view → stability wait → topmost-at-point (occlusion) check → real pointer move + `mousePressed`/`mouseReleased` |
| `Click(…, b.At(x,y))` | synthetic `click` at the offset's `clientX/clientY` (any option switches off `el.click()`) | real CDP click at the offset point (iframe-translated), occlusion/viewport-checked |
| `Click(…, b.Shift()…)` | synthetic click carrying the modifier flags | real click with the modifier bitmask held in CDP |
| `ClickEachImmediately` | `el.click()` on every visible+enabled match | real click on each, re-measured; skips hidden/disabled/off-screen/obscured |
| `DblClick` | two `el.click()`s + a `dblclick` event | real double-click (incrementing click-count) with full actionability |
| `RightClick` / `MiddleClick` | synthetic `mousedown`/`mouseup` + `contextmenu` / `auxclick` | real right/middle-button click (native `contextmenu` / `auxclick`) |
| `DragTo(src,tgt)` | synthetic pointer drag sequence | real CDP pointer drag (press → interpolated moves → release). Neither drives native HTML5 `draggable` |
| `ScrollWheel(dx,dy)` | `wheel` event, then manually scrolls the nearest scrollable ancestor | real trusted CDP wheel event |
| `Tap` | synthetic touch + pointer events + `click` | real CDP touch (`touchStart`/`touchEnd`) |
| `Hover` | synthetic pointer/mouse events - **does not** trigger CSS `:hover` | real pointer move - activates genuine CSS `:hover` |
| `SetValue` | sets `value`/`checked` + fires `input`/`change` | text: real click-focus + typed keys + blur; checkbox: real click; radio/`<select>`: JS (native pickers can't be driven by a real pointer) |
| `Type` / `SendKeysToWindowImmediately` | real CDP key events (both tracks) | same, plus scroll-into-view first |
| `Focus` | `el.focus()` (both tracks - real engines focus without a side-effecting click) | `el.focus()` |
| `ScrollIntoView` | real `scrollIntoView()` (both tracks) | real `scrollIntoView()` |
| `SetUpload` | CDP `DOM.setFileInputFiles` (both tracks - cannot be simulated in JS) | same |

A couple of deliberate gaps are worth calling out, both reachable via [chromedp](#chromedp-breaking-the-fourth-wall) on `b.Context`:

- **Occlusion on the fast track.** Plain `Click` intentionally clicks through overlays (the atomic, no-scroll default).  When you want to *assert* an element is genuinely clickable without paying for full realistic mode, use the deterministic [`b.BeClickable()`](#existence-counting-visibility-and-interactibility) matcher (visible + enabled + topmost-at-its-center); it stays opt-in rather than changing `Click`'s default, so existing click-through behavior is never silently broken.
- **Native HTML5 drag-and-drop, native `<select>` realism, cross-origin iframes, and device/mobile emulation** are not driven by either track by design - drop to chromedp for those (see the [emulation recipes](#emulation-and-device-conveniences-drop-to-chromedp)).

### Uploading Files

To attach files to a file input (`<input type="file">`) use `b.SetUpload`:

```go
b.SetUpload("input[type=file]", "/absolute/path/to/avatar.png")
b.SetUpload("#attachments", "./a.txt", "./b.txt") // the input needs the `multiple` attribute for more than one file
```

Setting a file input's files is one of the very few things that genuinely **cannot** be simulated in JavaScript - the browser forbids it for security reasons.  So, unlike most Biloba interactions, `SetUpload` reaches through the Chrome DevTools Protocol (`DOM.setFileInputFiles`) instead of running a snippet in the page.  It fires the input's `change` event just as a real selection would, so your `onchange` handlers run:

```go
b.SetUpload("#avatar", avatarPath)
Eventually("#preview").Should(b.HaveAttribute("src", ContainSubstring("blob:")))
```

`SetUpload` fails the spec if no element matches the selector.  The paths are resolved by the Chrome process, so they must exist on the machine running Chrome.

`SetUpload` is a dual method: drop the selector and it returns a matcher you can poll, so you can wait for the file input to mount before attaching files.  In the matcher form a single file is just a path; multiple files are passed as a `[]string` (bare variadic paths would be indistinguishable from the immediate selector+paths form):

```go
Eventually("#avatar").Should(b.SetUpload(avatarPath))
Eventually("#attachments").Should(b.SetUpload([]string{aPath, bPath}))
```

### Keyboard Input

`b.SetValue` sets an input's value directly and dispatches `input`/`change` events.  That satisfies most applications, but some are wired up to **real keyboard events** - search-as-you-type fields, rich-text editors, and hotkey handlers all listen for `keydown`/`keypress`/`keyup`.  Biloba cannot synthesize those atomically in JavaScript (the browser forbids synthetic key events from actually typing into the page), so it drops down to `chromedp`'s input domain for you.  There are two methods:

- **`b.Type`** is the element-targeted keyboard method - it focuses an element and sends text, named keys, and modifiers to it.  Like Biloba's other actions it [polls](#interacting-with-elements).
- **`b.SendKeysToWindowImmediately`** is the focus-free method - it fires keys at whatever currently has focus (or the document/window for global hotkeys), with no selector and no polling.

#### Typing into an element with `b.Type`

`b.Type` focuses an element and then sends genuine keystrokes - one `keydown`/`keypress`/`keyup` sequence per character.  It is the one keyboard method you reach for to drive a specific element: it takes plain text, named keys from the `biloba.Keys` namespace, and held modifiers, in any mix.

```go
b.Type("input.search", "gophers")                    // type "gophers"
b.Type("input.search", "gophers", biloba.Keys.Enter) // type "gophers" then press Enter (submit)
b.Type("input.search", biloba.Keys.Enter)            // press Enter into the search box
```

Biloba waits until the **first** element matching `selector` exists, is visible, and is enabled, focuses it, and then types the payload (polling by default; use `b.Immediate()` to act once, or `b.WithTimeout(...)` to bound the wait).  Unlike `SetValue`, `Type` **appends** to whatever is already in the field (it types as a user would) and triggers any key-event listeners.

`Type` chooses between its two forms by its arguments (after held modifiers are stripped out):

- **A selector followed by a payload** (two or more arguments, the first a `string`/`XPath`) → the **immediate** form above: the first argument is the element, the rest is what to type.
- **Just a payload** - a single string, or one or more named `Keys` → the **matcher** form: it returns a Gomega matcher you poll yourself, with the selector supplied by `Eventually`:

```go
Eventually("input.search").Should(b.Type("gophers"))
Eventually("#editor").Should(b.Type(biloba.Keys.Enter))
```

(One consequence of that rule: the matcher form can't mix *leading text with trailing keys* - `b.Type("hello", biloba.Keys.Enter)` is read as the immediate form with selector `"hello"`.  That's fine, because the immediate form already polls; reach for the matcher form only when you need a custom `Consistently` or composition.)

The available named keys are exposed on `biloba.Keys`.  These cover the editing, navigation, lock, and function keys you reach for in a browser test: `Backspace`, `Tab`, `Enter`, `Escape`, `Space`, `Delete`, `Insert`; the navigation cluster `ArrowUp`/`ArrowDown`/`ArrowLeft`/`ArrowRight`, `Home`, `End`, `PageUp`, `PageDown`; the locks `CapsLock`, `NumLock`, `ScrollLock`; the misc control keys `ContextMenu`, `PrintScreen`, `Pause`, `Help`, `Clear`; and the function keys `F1` through `F24`.  Each is a `biloba.Key` - for an exotic key that isn't listed (media, IME, launch keys) you can drop down to `chromedp` via `b.Context` and the [chromedp/kb](https://pkg.go.dev/github.com/chromedp/chromedp/kb) package.

#### Sending keys to the focused element with `b.SendKeysToWindowImmediately`

When you want to fire keys at whatever already has focus - a global hotkey handled at the document level, or a follow-up on an element you've just focused - use `b.SendKeysToWindowImmediately`.  It is **focus-free**: there's no selector, the keys land on the document's `activeElement` (or on `document`/`window` if nothing is focused).

```go
b.Click("#editor")                                // focuses the editor
b.SendKeysToWindowImmediately(biloba.Keys.Escape) // Escape, sent to the focused editor
b.SendKeysToWindowImmediately("/")                // a "/" hotkey handled at the document level
```

As the name says, it acts **immediately and never polls** - only you know what *should* be focused when it fires.  When the target appears asynchronously, gate it on a readiness anchor first and then send once:

```go
Eventually("input.search").Should(b.BeFocused()) // wait until it really has focus
b.SendKeysToWindowImmediately(biloba.Keys.Enter) // then send
```

(To type into a *specific* element, use `b.Type`, which focuses it for you and polls.)

#### Holding modifiers (Shift-Enter, Cmd-A)

To hold a keyboard modifier down while typing or sending keys, pass `b.Shift()`, `b.Ctrl()`, `b.Alt()`, or `b.Meta()` alongside the keys (in any position).  These are the **same** modifier options you hold during a [pointer interaction](#pointer-options-offsets-and-modifiers) (`Meta` is Command on macOS and the Windows key elsewhere):

```go
b.Type("textarea", biloba.Keys.Enter, b.Shift())            // Shift-Enter (e.g. soft newline)
b.Type("input", "a", b.Meta())                              // Cmd-A (select all)
Eventually("textarea").Should(b.Type("a", b.Meta()))        // the matcher form takes modifiers too
b.SendKeysToWindowImmediately(biloba.Keys.Enter, b.Meta())  // Cmd-Enter to the focused element (e.g. submit)
```

The modifier flags ride on every dispatched key event, so an app reading `e.shiftKey`/`e.metaKey`/`e.ctrlKey`/`e.altKey` in a `keydown` handler sees exactly the combo you sent.  This is the path to reach for when your app is wired to hotkeys.

### Invoking JavaScript on and with selected elements

At the end of the day, Biloba can give you a pile of DOM methods and matchers but you'll still come across a usecase that isn't implemented.  For that, you can head straight to JavaScript and get the job done yourself.  The [Running Arbitrary Javascript](#running-arbitrary-javascript) chapter below discusses how to run JavaScript with Biloba in _general_.  But in this section we focus on how to use Biloba to run JavaScript against selected DOM elements (which - of course, you can do with arbitrary JavaScript, but the API outlined here does the work of selecting elements for you using the same `selector` infrastructure we've discussed throughout this chapter).

You can invoke a method defined on a DOM element (e.g. `focus()` or `scrollIntoView()`) with:

```go
b.InvokeOn(selector, methodName, <optional args>)
```

`InvokeOn` operates on the **first** matching element and returns whatever the called method returns.  Like the other value-getters it [polls](#interacting-with-elements): it waits until an element matching `selector` is present before invoking the method (a method that is undefined or throws surfaces as a spec failure - at the deadline under polling, or immediately under `b.Immediate()`).  You can also pass arguments in - some examples:

```go
b.InvokeOn("#submit", "click") //though you should really just use b.Click("#submit")
b.InvokeOn("input[type='text']", "focus") //finds the first matching element then calles el.focus()
b.InvokeOn("input[type='text']", "scrollIntoView") //finds the first matching element then calles el.scrollIntoView()
b.InvokeOn("h1.title", "append", " - Hello") //calls el.append(" - Hello")
r := b.InvokeOn(".notice", "getAttributeNames") // r has type any but is a slice of strings containing all attribute names
b.InvokeOn(".notice", "setAttribute", "data-age", "17") // calls el.setAttribute("data-age", "17")
Expect(b.InvokeOn(".notice", "getAttribute", "data-age")).To(Equal("17")) // will now pass
```

Similarly, you can use `InvokeOnEachImmediately` to invoke a method and arguments on **all** matching elements.  Like the rest of the `*Immediately` family it acts on the current set immediately (no poll), nothing happens if no elements match, and there is no way, currently, to specify different arguments for different matching elements.

The upshot is that `InvokeOn/InvokeOnEachImmediately` find elements then call `el[methodName](...args)`.  This works well if the element has a relevant method defined on it.

If you want to do something more complex with the element - or you want to call several methods atomically - you can use `InvokeWith/InvokeWithEachImmediately`.  These take a callable snippet of JavaScript and invoke it - passing in the element along with any optional arguments you've provided, and returning the result.  `InvokeWith` polls for its element just like `InvokeOn`; `InvokeWithEachImmediately` is the immediate snapshot for-each variant.  Here's an example:

```go
countCharacters := `(el) => len(el.innerText)`
Expect(b.InvokeWith(".notice", countCharacters)).To(Equal(12.0))
Expect(b.InvokeWithEachImmediately(".notice", countCharacters)).To(HaveExactElements(12.0, 4.0, 73.0))

appendLi := `(el, text) => {
	let li = document.createElement('li')
	li.innerText = text
	el.appendChild(li);
}`
b.InvokeWith("ul", appendLi, "Another Item") //runs on the first <ul>
b.InvokeWithEachImmediately("ul", appendLi, "Another Item For All") //runs on all <ul>s
```

### The XPath DSL

XPath queries provide a powerful way to select DOM elements.  As mentioned above, if you're new to XPath queries you can check out the [MDN docs](https://developer.mozilla.org/en-US/docs/Web/XPath) or the [XPath Cheatsheet at devhints.io](https://devhints.io/xpath).  This will not be an exhaustive XPath tutorial.

XPath queries can be a bit fiddly to write and read.  To promote their use, Biloba provides a mini-DSL for constructing them.

You start building an XPath query with `b.XPath()` and then chain together additional XPath components.

If you call `b.XPath()` with no arguments the query will begin with `//*` (i.e. "find any element in the document").  If you call with a tag-name e.g. `b.XPath("div")` the query will begin with `//div`.  You can also simply provide a full fledged query (anything that begin with `/` or `./`) like so: `b.XPath("//div[@id='foo']")`.

Once you've started your query you layer on additional components using the DSL.  Here's a few examples:

```go
// find a div
b.XPath("div")

//by id
// find any element with id "submit"
b.XPath().WithID("submit")

//by CSS class
// find any element with class "red"
b.XPath().WithClass("red")
// find a button with CSS classes "red" and "highlight"
b.XPath().WithClass("red").WithClass("highlight")

// attributes
// find a disabled button
b.XPath("button").HasAttr("disabled") 
// find an input with attribute name that begin with "astro"
b.XPath("input").WithAttr("type", "text") 
// find an input with attribute name that begin with "astro"
b.XPath("input").WithAttrStartsWith("name", "astro") 
// find any element with attribute name that contains the string "bueller"
b.XPath().WithAttrContains("name", "bueller") 

// textual content
// find a button with text "Next"
b.XPath("button").WithText("Next") 
// find an li with text that starts with "Table of Contents"
b.XPath("li").WithTextStartsWith("Table of Contents") 
// find an quote elment with text that contains "have a dream"
b.XPath("quote").WithTextStartsWith("have a dream") 

// boolean logic
// note we need to use b.XPredicate() to build the predicate to pass to the boolean operators
// find a button with text Add Comment that is not disabled
b.XPath("button").WithText("Add Comment").Not(b.XPredicate().HasAttr("disabled"))
//find a radio button that is disabled
b.XPath("input").WithAttr("type", "radio").HasAttr("disabled")
//find a radio button that is disabled using And
b.XPath("input").And(b.XPredicate().WithAttr("type", "radio"), b.XPredicate().HasAttr("disabled"))
//find a div that is either red with the text Error or orange with the text Warning but without attr "fire-drill"
b.XPath("div").Or(
	b.XPredicate().And(
		b.XPredicate().WithClass("red"),
		b.XPredicate().WithText("Error"),
	),
	b.XPredicate().And(
		b.XPredicate().WithClass("orange"),
		b.XPredicate().WithText("Warning"),
	),
).Not(b.XPredicate().HasAttr("fire-drill"))
```

> Hey now,  that "mini-dsl" of yours ain't looking so "mini" any more.

Yes, yes, all that `XPredicate()` stuff gets verbose and all those boolean operations can pile on.  But it still beats:

```
//div[((contains(concat(' ',normalize-space(@class),' '),' red ')) and (text()='Error')) or ((contains(concat(' ',normalize-space(@class),' '),' orange ')) and (text()='Warning'))][not(@fire-drill)]
```

Back to some more examples.  Let's look at how you can navigate the DOM tree with XPath:

```go
//moving down the DOM hierarchy
//find any (direct) child of a <div> with class "comments"
b.XPath("div").WithClass("comments").Child()
//find any (direct) <p> child of a div with class "comments"
b.XPath("div").WithClass("comments").Child("p")
//find any (direct) <p> child with class "higlight" and text "User" of a div with class "comments"
b.XPath("div").WithClass("comments").Child("p").WithClass("highlight").WithText("User")

//find any descendant of the <div> with id "top"
b.XPath("div").WithID("top").Descendant()
//find any <li> descendant of the <div> with id "top"
b.XPath("div").WithID("top").Descendant("li")
//find any <a> descendant with "target='_blank'" of the <div> with id "top"
b.XPath("div").WithID("top").Descendant("a").WithAttr("target", "_blank")

//moving up the DOM hierarchy
//find the (direct) parent of a <div> with class "comments"
b.XPath("div").WithClass("comments").Parent()

//find any ancestor of the <div> with id "bottom"
b.XPath("div").WithID("bottom").Ancestor()
//find any <section> ancestor of the <div> with id "bottom"
b.XPath("div").WithID("bottom").Ancestor("section")
//find any <section> ancestor with class "outer" of the <div> with id "bottom"
b.XPath("div").WithID("bottom").Ancestor("section").WithClass("outer")

//moving left and right across the DOM hierarchy
//find the next sibling of the <li> with class "red"
b.XPath("li").WithClass("red").FollowingSibling()
//find the next sibling <quote> of the <li> with class "red"
b.XPath("li").WithClass("red").FollowingSibling("quote")
//find the next sibling <li> with class "blue" of the <li> with class "red"
b.XPath("li").WithClass("red").FollowingSibling("li").WithClass("blue")
//ditto but for the preceding sibling
b.XPath("li").WithClass("red").PrecedingSibling()
b.XPath("li").WithClass("red").PrecedingSibling("quote")
b.XPath("li").WithClass("red").PrecedingSibling("li").WithClass("blue")

//selecting elements with children that satisfy a property
//note that we need to use b.RelativeXPath - which constructs a "./" XPath - when using WithChildMatching
//find the <ul> that has a child <li> with text "igloo"
b.XPath("ul").WithChildMatching(b.RelativeXPath("li").WithText("igloo"))

//indexing into elements
//find the last child <li> of the second <ul> on the page
b.XPath("ul").Nth(2).Descendant("li").Last()
```

#### Matching text with the XPath DSL

For "the element that says X" prefer a [text locator](#selecting-by-locator) - `b.ByText`/`b.ByTextContains` match on *visible* text (what the user actually perceives) rather than a raw XPath `text()` node.  Reach for the XPath DSL's text predicates only when you need to scope an exact `text()` match to a specific tag:

```go
b.XPath("button").WithText("Submit")       // only <button> elements
b.XPath("button").WithTextContains("Subm") // only <button> elements
```

Under the hood, `b.XPath()` returns an object of `type XPath string`.  You can use `fmt.Println(x)` to see the XPath query generated by the DSL.

And since the DSL always returns an `XPath` string you can do things like this (from our example above):

```go
//build out a partial XPath query that selects descendants of the div with id "user-list"
userXPath := b.XPath("div").WithID("user-list").Descendant()

//we can now select specific users by adding onto userXPath:
Eventually(userXPath.WithText("Sally")).Should(b.HaveClass("online"))
Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
```


## Cookies and Storage

A huge fraction of browser suites need to seed some browser state before exercising the app - most commonly an authentication cookie or a `localStorage` entry - so they can skip the login flow and get straight to the behavior under test.  Biloba provides first-class helpers for cookies, `localStorage`, and `sessionStorage`.

Everything in this section is scoped to the tab's isolated `BrowserContextID`.  Since each Biloba tab lives in its own incognito-like browser context, cookies and storage set on one tab never leak into another - so parallel specs (and multi-tab specs) stay isolated for free.

> **You must navigate to a real origin first.**  Cookies and web storage are associated with an origin, and `about:blank` has an _opaque_ origin that cannot hold either.  Always `b.Navigate(...)` to a real URL before setting cookies or storage.

### Cookies

Set, read, and clear cookies with `b.SetCookie`, `b.GetCookies`, and `b.ClearCookies`:

```go
BeforeEach(func() {
	b.Navigate("http://localhost:8080/home")

	// seed the login cookie so we skip the login flow
	b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})

	// clear all cookies after each spec so state doesn't leak
	DeferCleanup(b.ClearCookies)
})

It("is logged in", func() {
	b.Navigate("http://localhost:8080/home")
	Eventually("#user-name").Should(b.HaveInnerText("Joe"))
})
```

A `biloba.Cookie` only requires a `Name` and `Value`.  Whenever you don't provide a `Domain`, Biloba associates the cookie with the tab's current URL (an explicit `Path` still applies on top of that origin).  Because of this, **the tab must be on a real origin when you set such a cookie** - a cookie can't attach to `about:blank`, so `SetCookie` fails the spec with a clear message if you try (navigate first, or set the `Domain` explicitly).  You can also set `Domain`, `Path`, `Secure`, `HTTPOnly`, and `SameSite` (one of `"Strict"`, `"Lax"`, or `"None"`) explicitly.  Provide an `Expires` time to set a persistent cookie - leave it as the zero `time.Time` to set a session cookie:

```go
b.SetCookie(biloba.Cookie{
	Name:    "user",
	Value:   "Joe",
	Domain:  "localhost",
	Expires: time.Now().Add(180 * 24 * time.Hour),
})
```

You can pass multiple cookies to a single `SetCookie` call.  `b.GetCookies()` returns a `biloba.Cookies` slice for all the cookies in the tab's browser context (the returned `Cookie`s have their `Session` field set to `true` for session cookies and a populated `Expires` for persistent ones).

#### Asserting on Cookies

`b.HaveCookie(name)` is a matcher you assert against the tab itself.  It passes if the tab has a cookie whose name matches - `name` may be a string (exact match) or a Gomega matcher:

```go
Eventually(b).Should(b.HaveCookie("session"))
Expect(b).To(b.HaveCookie(ContainSubstring("my_guid")))
```

You can chain refinements onto the matcher to further constrain the _same_ cookie.  Each refinement takes a string (exact match) or a Gomega matcher:

```go
Expect(b).To(b.HaveCookie("session").WithValue("abc123").WithPath("/"))
Expect(b).To(b.HaveCookie(ContainSubstring("my_guid")).WithValue(ContainSubstring("ABCD-1")))
Expect(b).To(b.HaveCookie("session").WithDomain("localhost").WithSecure())
```

The available refinements are `WithValue`, `WithPath`, `WithDomain`, `WithSameSite`, and the boolean flag refinements `WithSecure(...)` and `WithHTTPOnly(...)`.  The flag refinements take an optional bool: called with no argument they assert the flag is `true` (`WithSecure()` is shorthand for `WithSecure(true)`), and `WithSecure(false)` asserts the cookie is _not_ Secure.  All of the refinements must hold for a single cookie - if two cookies each satisfy some-but-not-all of the refinements the matcher does not pass.

The same query plays double duty.  As an assertion you hand it to `Should`/`Eventually` and spell it `HaveCookie` (above); as a **predicate** you hand it to the `Find`/`Filter` helpers on the `Cookies` slice returned by `b.GetCookies()` and spell it `CookieMatching`.  `Find` returns the matching `Cookie` and a bool reporting whether one was found:

```go
cookie, ok := b.GetCookies().Find(b.CookieMatching("session").WithPath("/admin"))
admins := b.GetCookies().Filter(b.CookieMatching(ContainSubstring("session")))
```

`b.HaveNumCookies(expected)` asserts on the number of cookies on the tab.  `expected` may be an int (exact match) or a Gomega matcher:

```go
Expect(b).To(b.HaveNumCookies(2))
Expect(b).To(b.HaveNumCookies(BeNumerically(">", 0)))
```

### Local and Session Storage

`b.LocalStorage()` and `b.SessionStorage()` return typed handles for interacting with the corresponding web-storage area:

```go
b.LocalStorage().Set("count", 3)

var count int
b.LocalStorage().Get("count", &count) // count == 3
```

Both handles expose the same methods: `Set(key, value)`, `Get(key, ...pointer)`, `GetAll()`, `Remove(key)`, `Clear()`, and `Length()`.

**Type handling:** values are JSON-encoded on `Set` and JSON-decoded on `Get`, so you can round-trip any JSON-serializable Go value:

```go
b.LocalStorage().Set("user", map[string]any{"name": "Joe", "admin": true})

var user struct {
	Name  string
	Admin bool
}
b.LocalStorage().Get("user", &user)
```

`Get` takes an optional pointer argument to decode into a specific type (just like [`b.Run`](#running-arbitrary-javascript)).  Without it, `Get` returns the decoded value as type `any` (so numbers come back as `float64`).  `Get` returns `nil` for a missing key.  Values written to storage by the page itself that aren't valid JSON (e.g. a plain `localStorage.setItem("k", "v")`) are returned as their raw string.

#### Asserting on Storage

`b.HaveLocalStorageItem` and `b.HaveSessionStorageItem` are matchers you assert against the tab.  With a single argument they pass if the key exists; with a second argument they pass if the stored value matches (a string for an exact match, or a Gomega matcher):

```go
Expect(b).To(b.HaveLocalStorageItem("user"))            // key exists
Expect(b).To(b.HaveLocalStorageItem("user", "Joe"))     // exact value
Eventually(b).Should(b.HaveLocalStorageItem("count", BeNumerically(">", 0)))

Expect(b).To(b.HaveSessionStorageItem("token", ContainSubstring("ABCD")))
```

`b.HaveNumLocalStorageItems` and `b.HaveNumSessionStorageItems` assert on the number of items in the corresponding storage area.  `expected` may be an int (exact match) or a Gomega matcher:

```go
Expect(b).To(b.HaveNumLocalStorageItems(2))
Expect(b).To(b.HaveNumSessionStorageItems(BeNumerically(">", 0)))
```

## Dialogs and Downloads

Biloba provides support for handling JavaScript dialogs (`alert`, `prompt`, etc.) and file downloads.  There is some amount of complexity here, though so each warrants its own section:

### Handling Dialogs

JavaScript is single-threaded.  Which means that when a dialog box (one of `alert`, `beforeunload`, `confirm`, and `prompt`) pops up... absolutely nothing can be executed until the dialog is handled.

Biloba and the Chrome DevTools Protocol more generally operates by _doing things_ - none of which can be done so long as a dialog is open and unhandled.

That, unfortunately, means that this sort of imperative pattern is not possible:

```go
/* === INVALID === */
b.Click("button")
Eventually(b).Should(b.HaveAlertDialog("Hello there!"))
b.AcceptAlertDialog()
```

as soon as the dialog box appears, nothing else can run.

So, instead, the handling of Dialog boxes needs to be configured _before_ they come up.

To make sure your suite doesn't accidentally get blocked by a dialog box Biloba has a set of default rules for dialog boxes:

- `alert` dialog are automatically acknowledged
- `confirm` and `prompt` dialogs are automatically cancelled
- `beforeunload` is automatically accepted

You can override these by registering a series of dialog handlers.  You can have as many handlers as you'd like and more recently registered handlers get first dibs on new dialog boxes.  All handlers are reset by `b.Prepare()` so you'll need to re-register them between specs (which you typically do, anyway, in a `BeforeEach` or `It`).

You register a handler by calling any of the following:

- `b.HandleAlertDialogs()`
- `b.HandleBeforeUnloadDialogs()`
- `b.HandleConfirmDialogs()`
- `b.HandlePromptDialogs()`

each of these return a `DialogHandler` that you then further configure in the following ways:

1. `MatchingMessage(message)` allows you to specify a message matcher so that the handler only applies for a given message.
2. `WithResponse(response)` allows you to provide a response - either `true` or `false` to indicate that you want Biloba to accept or cancel the dialog
3. `WithText(text)` allows you to provide text for Biloba to enter into prompt dialogs.  If no text is provided and the response is set to `true`, `Biloba` will use the prompt's default text.

Here's what this looks like in practice:

```go
//accept any confirm dialogs that ask if the user is ready to proceed
b.HandleConfirmDialogs().MatchingMessage("Are you ready to proceed?").WithResponse(true)

//cancel any confirm dialogs that ask if the user is would like to delete something
b.HandleConfirmDialogs().MatchingMessage(ContainSubstring("delete")).WithResponse(false)

//cancel any confirm dialogs that ask if the user is would like to delete something, unless that something is a pink bunny
//note that order matters here, the latter registration takes precedence
b.HandleConfirmDialogs().MatchingMessage(ContainSubstring("delete")).WithResponse(false)
b.HandleConfirmDialogs().MatchingMessage("Do you want to delete the pink bunny?").WithResponse(true)

//deny any prompts asking for a preferred dessert
b.HandlePromptDialogs().MatchingMessage("What dessert would you prefer?").WithResponse(false)

//accept any prompts asking for a dinner preference, and use the default prompt
b.HandlePromptDialogs().MatchingMessage("What dinner entree would you prefer?").WithResponse(true)

//accept any prompt asking for a beverage preference, and provide the text "coffee"
b.HandlePromptDialogs().MatchingMessage("What beverage would you prefer?").WithResponse(true).WithText("Coffee")

//deny any onbeforeunload dialogs - note that `MatchingMessage` will not work with `beforeunload` dialogs as a custom message cannot be set.
b.HandleBeforeUnloadDialogs().WithResponse(false) 
```

if you hang on to the `DialogHandler` reference you can remove it later like so:

```go
coffeeResponder := b.HandlePromptDialogs().MatchingMessage("What beverage would you prefer?").WithResponse(true).WithText("Coffee")
b.RemoveDialogHandler(coffeeResponder)
```

Remember that, in practice, you need to set up your handler _before_ you invoke an action that will bring up the dialog.  Otherwise the default handlers will kick in:

```go
/* === INVALID === */
b.Click("#specify-beverage")
b.HandlePromptDialogs().MatchingMessage("What beverage would you prefer?").WithResponse(true).WithText("Coffee")
Ω("#beverage").Should(b.HaveInnerText("Coffeee")))
```

do this instead:

```go
b.HandlePromptDialogs().MatchingMessage("What beverage would you prefer?").WithResponse(true).WithText("Coffee")
b.Click("#specify-beverage")
Ω("#beverage").Should(b.HaveInnerText("Coffeee")))
```

Note that dialogs are scoped to a tab.  You'll need to register separate handlers for any new tabs you create (or are spawned) in your spec.

### Inspecting Handled Dialogs

In addition to handling dialogs you can also get a list of all dialogs that have appeared in the current spec (this list is cleared out between specs by `b.Prepare()`).

You get the list of dialogs like so:

```go
dialogs = b.Dialogs()
```

you can then filter this list by message and type, and ask for the most recent dialog.  For example:

```go
//get the most recent dialog
b.Dialogs().MostRecent()

//get the most recent prompt dialog
b.Dialogs().OfType(biloba.DialogTypePrmopt).MostRecent()

//get all confirm dialogs that mention deleting things
b.Dialogs().OfType(biloba.DialogTypeConfirm).MatchingMessage(ContainSubstring("delete"))
```

The `Dialog` struct is fairly straightforward:

```go
type Dialog struct {
	Type           page.DialogType 
	Message        string // the message displayed to the user
	DefaultPrompt  string // the default prompt provided to the user (prompt type only)
	HandleResponse bool   // the response Biloba provided while handling this dialog
	HandleText     string // the text Biloba provided while handling this dialog
	Autohandled    bool   // true if no registered handler was found and Biloba's default handlers were used
}
```

and you can make assertions on these various fields a needed.

### Managing Downloads

Actions performed on a tab can result in a file being downloaded.  Biloba automatically tracks these downloads for you and makes them available to query and explore.  Downloads are scoped by tab so you'll need to invoke these methods on the correct tab object to identify them.  Biloba automatically attaches download hooks whenever a tab is created or a spawned tab is attached-to (and, of course, to the reusable root tab).

When a download begins, Biloba starts associates it with its originating tab and starts tracking it.  Downloads eventually will either complete or be cancelled.  Biloba tracks those events as well and updates its download object appropriately.  Once completed Biloba gives you access to the contents of the download, as well as the filename proposed by the browser.

To get a list of all downloads associated with a tab, use:

```go
downloads := b.AllDownloads()
```

this will include active (in-progress) downloads a well as completed and cancelled downloads.

To grab just the completed downloads, use:

```go
downloads := b.AllCompleteDownloads()
```

You can combine this eventually to assert that a download has occurred:

```go
b.Click("#download")
Eventually(b.AllCompleteDownloads).Should(HaveLen(1))
```

Of course, asserting on length can be brittle and lead to unsatisfying failure messages.  Instead, you should use `b.HaveDownloaded` like so:

```go
// we should get a download with the specified filename
Eventually(b).Should(b.HaveDownloaded("hello.pdf"))

// ...optionally refined by URL and/or content
Eventually(b).Should(b.HaveDownloaded("hello.pdf").WithContent([]byte("hello world")))

// ...or matched on content alone (a download has no single primary key, so the filename is optional)
Eventually(b).Should(b.HaveDownloaded().WithContent(ContainSubstring("hello world")))
```

`b.HaveDownloaded()` returns a chainable `DownloadQuery`.  The optional filename argument and the `WithURL`/`WithContent` refinements each take a string/[]byte (exact match) or a Gomega matcher, and only **complete** downloads are considered.  Note that the filename is the _suggested_ filename provided by the browser; the actual filename on disk is opaque and not something you'll need to worry about.

Once the download has completed you can get a reference to it by accessing `b.AllCompleteDownloads()` directly or using its `Find()` method - which takes the same query, spelled `b.DownloadMatching` to read as a predicate:

```go
dl := b.AllCompleteDownloads()[0]
dl := b.AllCompleteDownloads().Find(b.DownloadMatching("hello.pdf"))
```

Once you've got a reference to the download you can do the following:

```go
url := dl.URL //get the originating URL of the download
fname := dl.Filename //get the recommended filename fo the download

dl.IsComplete() //true if complete
dl.IsCancelled() //true if cancelled
dl.IsActive() //true if the download is still in progress

dl.Content() //returns the downloaded content as a []byte array.
```

Behind the scenes, Biloba is setting up temporary directories, managing GUIDs, and keeping track of downloads for you.  And all these pieces get cleaned up between spec runs and after the suite ends.

#### Going the Extra Mile for Stability

There are a couple of gotchas that exist in Chrome that Biloba tries to paper over for you:

1. We'll spare you the details, but it turns out that the bit of configuration that tells Chrome to emit download events gets blown away if you close a tab in the same `BrowserContextID` as another tab.

	OK, fine.  Some details: since Biloba's created tabs all live in separate `BrowserContextID` universes this is typically not a problem.  However if you spawn a tab, and then close it, the download configuration for all other tabs in that `BrowserContextID` (including, potentially, the reusable root tab) will be blown away.  This will immediately cancel any in-flight downloads and prevent any future downloads from happening.  Biloba prevents this by preventing a spawned tab from closing _until_ all downloads associated with its `BrowserContextID` are complete.  Once closed, Biloba reregisters the download configuration for all the sibling tabs.

	**Good news**: you can forget that you read that last paragraph and just use Biloba's APIs without worrying about such things.  Just make sure to use `Eventually(tab.Close).Should(Succeed())` to retry closing a spawned tab in the presence of downloads.

2. Did you know that Chrome limits downloads to a maximum of 10 downloads per tab within any given second?  This can lead to all sorts of fascinating flaky specs.  All is well - until you add that extra spec that does that extra download and the spec randomization happens to align it with the other 5 specs that do a bunch of downloads and they all run within a second and **bam**: failure.

    Biloba prevents this from happening.  It keeps track of the number of downloads in a sliding time window and rate-limits any actions that might generate more downloads for you.  There are limits to what Biloba can do, though, without cluttering up the API.  If you have a single action (example a button click) that generates 8 different downloads it may be possible for flaky specs to resurface.

	If demand warrants (and someone someday opens a GitHub issue) - we can add a `b.BlockUntilSafeToDownload(8)` method.  Or some such.


## Running Arbitrary Javascript

We covered running JavaScript that's scoped to a particular element using `b.InvokeOn/b.InvokeWith` - but Biloba will also happily allow you to run arbitrary JavaScript on the page for any tab.  This can often be a convenient shortcut to make a more complex assertion in JavaScript, or to make assertions on the state of your web application that may be overly difficult/complex to make simply by interrogating the DOM alone.

> **Before you reach for `b.Run`, check for a first-class matcher.**  `b.Run` is the escape hatch - but the things people reinvent with it usually already exist, and the matcher version polls cleanly under `Eventually` where a raw `b.Run` does not.  The single most-reinvented one is **counting elements**: if you're writing `b.Run("document.querySelectorAll(sel).length", &n)`, you want [`b.HaveCount`](#existence-counting-visibility-and-interactibility) - `Eventually(sel).Should(b.HaveCount(7))` (or `b.HaveCount(BeNumerically(">", 10))`).  Likewise, reach for [`b.GetAttribute`](#contents-and-classes)/[`b.GetProperty`](#properties) instead of `getAttribute`/property reads, [`b.HaveInnerText`/`b.HaveTextContent`](#contents-and-classes) instead of reading `innerText`/`textContent`, and a [text locator](#selecting-by-locator) + `ShouldNot(b.Exist())` instead of scanning text in JS.  Keep `b.Run` for genuinely app-specific state.

To run JavaScript:

```go
b.Run(`<your script>`)
```

some examples:

```go
// print something to the GinkgoWriter
b.Run(`console.log("number of records:", app.numRecords)`)

//make an assertion.  The Ginkgo test will fail if this assertion fails
b.Run(`
	var count = document.querySelectorAll("h1").length
	console.assert(count == 17, "%o", {count: count, msg: "is not 17"})
`)

// set up some internal state - this shortcut might be much faster than
// logging in and loading data from a server and will help us focus on testing
// how the web app renders the fixture data vs all the network/auth plumbing instead
b.Run(`
	app.load(` + jsonEncodedFixtureData + `)
	mithril.redraw()
`)
Eventually("#doc-name").Should(b.HaveInnerText("My Fixture Data"))
```

You can also get the result of a JavaScript invocation.  There are two ways to do this.  You can just use the return value which will have type `any`:

```go
result := b.Run("[1+2, 4]") // here result has type `any` and can be hard to work with...
Expect(result).To(Equal([]float64{3.0, 4.0}) // fails - type mismatch
Expect(result).To(HaveExactElements(3.0, 4.0)) // works
```

Because the result is JSON-decoded, **numbers always come back as `float64`** - even a scalar.  So `b.Run("1+3")` returns `float64(4)`, and `Eventually("count").Should(b.EvaluateTo(4))` (an `int`) will *not* match.  Either assert with a numeric matcher - `b.EvaluateTo(BeNumerically("==", 4))` - or decode into a typed pointer as shown below.

or you can provide a typed pointer for Biloba to decode the result into:

```go
var result []int
b.Run("[1+2, 4]", &result)
Expect(result[0] + result[1]).To(Equal(7))
```

You can also save a step by using Biloba's `EvaluateTo` matcher:

```go
Expect("[1+2], 4]").To(b.EvaluateTo(HaveExactElements(3.0, 4.0)))
```

When you run a script purely for its side effects you don't need a decode target at all - just omit it:

```go
b.Run("app.redraw()") // the return value is discarded
```

(passing `nil` explicitly - `b.Run("app.redraw()", nil)` - does the same thing.)  If you *do* pass a non-nil pointer the script must return a JSON-serializable value; a script that evaluates to `undefined` will fail with a directive error reminding you to either drop the decode target or `return` a value.  This is a common stumble with side-effect-only snippets - a bare `(() => { installObserver() })()` evaluates to `undefined` - make sure toomit the target for these.

That covers how we get values _out_ of a JavaScript invocation.  What if you want to _provide_ arguments to a JavaScript function?  Biloba has a nifty little helper for that: `b.JSFunc()`.

`b.JSFunc(script)` takes an **invocable** snippet of JavaScript.  Invocable simply means that:

```go
"(" + script + ")()"
```

is a valid JavaScript invocation.  `JSFunc` lets you populate the JavaScript invocation with arguments using `.Invoke()`.  Here are some examples:

```go
//this will run console.log(1, [2,3,4], "hello", true, null)
b.Run(b.JSFunc("console.log").Invoke(1, []int{2, 3, 4}, "hello", true, nil))

//here we save off a reusable JSFunc:
adder := b.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
//and use it to sum up numbers:
var result int
b.Run(adder.Invoke(1, 2, 3, 4, 5, 10), &result)
Ω(adder.Invoke(1, 2, 3.7, 4, 5)).Should(b.EvaluateTo(15.7))
```

`Invoke` simply takes the arguments you pass to it, JSON encodes them, and then invokes the function.  In the case of our `adder` example the literal JavaScript code that is invoked looks like:

```js
((...nums) => nums.reduce((s, n) => s + n, 0))(...[1,2,3,4,5,10])
```

If you want to refer to an existing JavaScript variable or add a JavaScript expression to your function invocation, use `b.JSVar`:

```go
adder := b.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
//first we define a variable
b.Run("var counter = 10")
//and now we reference it using b.JSVar
Ω(adder.Invoke(1, 2, 3.7, 4, 5, b.JSVar("counter"))).Should(b.EvaluateTo(25.7))
//JS expressions work too
Ω(adder.Invoke(1, 2, 3.7, 4, 5, b.JSVar("counter * 2"))).Should(b.EvaluateTo(35.7))
```

For that last expression the evaluated JavaScript is:

```js
((...nums) => nums.reduce((s, n) => s + n, 0))(...[1,2,3,4,5,counter * 2])
```

if you hadn't used `b.JSVar` the invocation

```go
/* === INVALID === */
Ω(adder.Invoke(1, 2, 3.7, 4, 5, "counter * 2")).Should(b.EvaluateTo(35.7))
```

would have turned into

```js
/* === INVALID === */
((...nums) => nums.reduce((s, n) => s + n, 0))(...[1,2,3,4,5,"counter * 2"])
```

which would evaluate to `"15counter * 2"` 🤦‍♀️

You can always inspect the generated JavaScript with `fmt.Println(b.JSFunc(...).Invoke(...))` a `Invoke` simply returns a string.

> Wait slow down - what happened in that example up there.  Why was `adder` able to access `counter`?

Great catch. all this Javascript runs on the global window object.  That means you can do stuff like this:

```go
var _ = Describe("rendering documents", func() {
	BeforeEach(func() {
		// short-cut to set up the fixture data
		b.Run(`
			var document = {
				"Name": "My Document",
				"Content": "The quick brown fox"
			}
			app.load(document)
		`)
	})

	It("has a green status if the content is short enough", func() {
		Expect("app.contentStatus()").To(b.EvaluateTo("green"))
	})

	It("has a red status if the content is too long", func() {
		b.Run(`
			document.Content += " jumps over the lazy dog"
			app.update(document)
		`)
		Expect("app.contentStatus()").To(b.EvaluateTo("red"))
	})
})
```

> Wait.  Are you writing Javascript unit tests in Ginkgo?

You said it, not me.

### Running asynchronous Javascript

`b.Run` evaluates a _synchronous_ expression.  Modern web applications, however, often expose `async` APIs (`fetch`, dynamic `import`, `await app.load()`).  To work with these use `b.RunAsync` (and its error-returning sibling `b.RunErrAsync`).

`RunAsync` runs your script as the **body of an async function**.  That means you can `await` freely and `return` the value you care about:

```go
// await an application API
b.RunAsync(`return await app.load()`)

// fetch from the network and decode the response
users := b.RunAsync(`
	const response = await fetch("/api/users")
	return await response.json()
`)
```

Everything you've learned about `b.Run` applies: you can decode the result into a typed pointer...

```go
var users []User
b.RunAsync(`return await app.load()`, &users)
```

...and a non-promise value works just fine too (`b.RunAsync("return 1 + 2")` returns `3`).

The key difference from `b.Run` is the `return`.  Because your script is the body of a function (not a bare expression) you must `return` the value you want out of it - a script that forgets to `return` will resolve to `undefined`.  The flip side: a top-level `return` in `b.Run` is a syntax error (`Illegal return statement`), because `b.Run` evaluates a bare expression.  When you hit that, reach for `b.RunAsync` (which wraps your script in a function body) rather than wrapping it in an IIFE yourself - Biloba's failure message points you here.

If the script throws, or the promise you `await` (or `return`) rejects, the spec fails (use `RunErrAsync` if you'd like to handle the error yourself):

```go
// fails the spec with the rejection's message
b.RunAsync(`return await Promise.reject(new Error("boom"))`)
```

One last thing before we leave the subject of running Javascript.  The Chrome DevTools basically run these JavaScript snippets via `eval`.  There is one small gotcha.  The following:

```go
var result map[string]int
b.Run("{a:1, b:2}", &result)
```

will fail with a syntax error.  That's because `eval("{a:1, b:2}")` interprets the `{}` as a [block statement and not an object literal](https://github.com/chromedp/chromedp/issues/1275#issuecomment-1459079119).  You can work around this with:

```go
var result map[string]int
b.Run("({a:1, b:2})", &result)
```

## Stubbing and Observing the Network

Real browser tests usually talk to a backend.  This is a good thing as you _really_ want rich and expressive tests that are running against the _actual_ backend.  Such an approach _can_ lead to slow (every spec waits on real network round-trips) and flaky (the backend has to be up, seeded, and deterministic) tests if you aren't disciplined.  In general you should lean into discipline and stick with a real backend; investing effort to make it more performant and stable.

If all else fails, or if setting up an edge condition is too tricky from the outside-in, Biloba lets you **stub** responses, **observe** the requests a tab makes, and **wait for the network to settle** - so your specs can be fast, deterministic, and hermetic.

### Stubbing requests

`b.StubRequest` intercepts requests whose URL matches and fulfills them with a canned response instead of letting them hit the network:

```go
b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
	Body:    `[{"name": "Jane"}, {"name": "Bob"}]`,
	Headers: map[string]string{"Content-Type": "application/json"},
})
b.Navigate("/app")
Eventually(".user").Should(b.HaveCount(2))
```

The first argument is a URL matcher: a string (exact match) or any Gomega matcher (`ContainSubstring`, `HaveSuffix`, `MatchRegexp`, …).  The `StubResponse` lets you set the `Status` (defaults to `200`), the `Body`, and any `Headers`.

Requests that don't match any stub are passed through to the real network, so you can stub just the endpoint you care about and let everything else load normally:

```go
// only the users API is faked; the page's HTML, JS, and other XHRs load for real
b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Status: 500})
```

A few things to keep in mind:

- **Stubs are per-tab and reset by `Prepare()`.**  Each spec starts with no stubs.
- **Registering the first stub turns on request interception for that tab.**  Under the hood Biloba pauses and resumes *every* request the tab makes so it can decide whether to fulfill or pass each one through.  That has a small per-request cost, so interception is only enabled once you register a stub (and is torn down at the next `Prepare()`).
- **Stubbed requests are still observed.**  `HaveMadeRequest` and `AllRequests` (below) see stubbed requests just like real ones.

`StubRequest` is one of a family of network handlers.  All of them are registered the same way - on a tab, scoped to it, cleared by `Prepare()` - and consulted in **registration order, first-match-wins**.  A request that matches no handler passes through to the real network.  The rest of the family is below.

### Aborting requests

`b.AbortRequest` fails any request whose URL matches, simulating a network failure: the page's `fetch`/XHR rejects exactly as it would if the request couldn't be made.  Use it to exercise your app's error paths:

```go
b.AbortRequest(ContainSubstring("/api/users"))
b.Click("#load-users")
Eventually("#error").Should(b.HaveText("Couldn't reach the server"))
```

The `url` argument is a string (exact match) or any Gomega matcher.  Like all the network handlers, aborts are per-tab and reset by `Prepare()`.

### Modifying requests (continue with overrides)

`b.ModifyRequest` lets the request reach the real network but rewrites parts of it on the way out.  It returns a chainable builder; only the overrides you set are applied:

```go
b.ModifyRequest(ContainSubstring("/api/users")).
	WithURL("/api/v2/users").          // rewrite the destination (not observable by the page)
	WithMethod("POST").                // override the HTTP method
	WithHeader("Authorization", tok).  // set a header (call repeatedly to add more)
	WithBody(`{"name":"Jane"}`)        // override the request body
```

Each `WithHeader` accumulates, so you can build up several headers.  Anything you don't set passes through unchanged.

### Modifying responses

`b.ModifyResponse` intercepts the **real** response coming back and hands the page a modified version of it.  Use the chainable form to override pieces of the response:

```go
b.ModifyResponse(ContainSubstring("/api/users")).
	WithStatus(503).
	WithHeader("Content-Type", "text/plain").
	WithBody("service unavailable")
```

Or supply a transform with `Using` that receives the real response (status, headers, and body) and returns a replacement `StubResponse` - handy when you want to tweak the real payload rather than replace it wholesale:

```go
b.ModifyResponse(ContainSubstring("/api/users")).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
	// e.g. inject a field into the real JSON, or corrupt it to test a parse-error path
	return biloba.StubResponse{Status: r.Status, Headers: r.Headers, Body: strings.Replace(r.Body, `"active"`, `"disabled"`, 1)}
})
```

Response interception is a heavier mode than the request-stage handlers: the tab pauses each matching request twice (once on the way out, once when the response arrives) so Biloba can read the real body.  As with the others, it's per-tab, reset by `Prepare()`, and first-match-wins.

### Observing requests

Biloba records every request each tab makes.  Use `b.HaveMadeRequest(url)` to assert (and poll for) a request - `url` is a string (exact match) or a Gomega matcher:

```go
b.Navigate("/app")
b.Click("#load-users")
Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))
```

`HaveMadeRequest` returns a chainable query.  Refine it to a more specific request by chaining `WithMethod` (every refinement applies to the same request):

```go
Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")).WithMethod("POST"))
```

The same query plays double duty.  As an assertion you hand it to `Should`/`Eventually` (above) and spell it `HaveMadeRequest`, which reads as a claim about the tab.  As a **predicate** you hand it to the `Find`/`Filter` helpers on the `Requests` slice returned by `b.AllRequests()` (each `*Request` has `URL`, `Method`, `Headers`, and `ResourceType`) and spell it `RequestMatching`, which reads as a description of one request.  The two spellings are interchangeable - they build the same query:

```go
req := b.AllRequests().Find(b.RequestMatching(ContainSubstring("/api/users")).WithMethod("GET"))
Expect(req.Method).To(Equal("GET"))

apiCalls := b.AllRequests().Filter(b.RequestMatching(ContainSubstring("/api/")))
```

The recorded requests are scoped to a single tab and are reset by `Prepare()`, so each spec starts with a clean slate.

### Waiting for the network to settle

`BeNetworkIdle` passes when a tab has no in-flight requests.  Pair it with `Eventually` to wait for a burst of activity to finish:

```go
b.Click("#refresh")
Eventually(b).Should(b.BeNetworkIdle())
```

In keeping with Biloba's pragmatism, "idle" means the in-flight count has reached zero - Biloba does not wait for a quiet period (à la `networkidle0`).  If you need to wait for one specific request to complete, assert on its effect directly instead.

`BeNetworkIdle` tracks **HTTP** requests only - its in-flight count is keyed on the `Network.requestWillBeSent`/`Network.loadingFinished` request IDs Chrome reports.  A long-lived **WebSocket** does not register as an in-flight request, so it will not keep `BeNetworkIdle` perpetually busy (nor will `BeNetworkIdle` wait for a particular WS frame to arrive - wait on that frame's observable effect instead).

## Window Size, Screenshots, Configuration, and Debugging

There are a few other odds and ends to cover, let's dive in

### Window Size

Biloba's default window size is `1024x768`.  You can change this for an entire suite by passing in the `StartingWindowSize(width, height)` configuration to `SpinUpChrome`:

```go
var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(GinkgoT(), biloba.StartingWindowSize(640, 480))
}, func() {
	b = biloba.ConnectToChrome(GinkgoT())
})
```

Now every tab that is opened will default to `640x480`.  Note that this is a global property of the Chrome _process_.

Once you have a tab, you can resize it using:

```go
b.SetWindowSize(width, height)
```

this also accepts any [`chromedp.EmulateViewportOption`](https://pkg.go.dev/github.com/chromedp/chromedp#EmulateViewportOption) after `height`.  `b.SetWindowSize` automatically sets up a Ginkgo `DeferCleanup` to reset the window size when the spec ends.  You can query the window size of a tab at any time using `b.WindowSize()`.

One quick hack to speed up a test suite is to use the _smallest_ viable window size to run the tests.  You can then pass `BilobaConfigFailureScreenshotsSize(width, height)` to `ConnectToChrome(...)` to configure the size of Biloba's automatically generated screenshots.  Biloba will scale the window up on failure, take a screenshot, then scale it back down to proceed with other tests.  As an anecdotal data-point a 30% speed-up was observed for a Biloba test suite against a complex web-app running in parallel when the screen-size was minimized in this way.

### Capturing Screenshots

As discussed above, Biloba automatically emits screenshots when a spec fails or a progress report is requested.  (It can also attach a text [DOM outline](#outline) on failure — off for an interactive human, on automatically under CI or an AI agent.  See [Failure artifacts](#failure-artifacts) for how the defaults are resolved.)

You can also manually capture a screenshot of a tab:

```go
b.CaptureScreenshot()
```

returns a `[]byte` array (typically a `.png`) representation of the browser.  And

```go
b.CaptureImgcatScreenshot()
```

returns a `string` representation in [iTerm's ImgCat format](https://iterm2.com/documentation-images.html).  If you'd like to have your spec emit additional screenshots at specific points in time the recommended pattern is to use Ginkgo's `AddReportEntry` for example:

```go
AddReportEntry("some description", b.CaptureImgcatScreenshot())
```

You can use the `ReportEntryVisibilityFailureOrVerbose` decorator to only emit the screenshot if the spec fails:

```go
AddReportEntry("some description", b.CaptureImgcatScreenshot(), ReportEntryVisibilityFailureOrVerbose)
```

#### Saving screenshots to files

If you want to save a screenshot to a file (for example, to open it in an image viewer or to share it), use:

```go
path := b.CaptureScreenshotToFile("/path/to/screenshot.png")
```

This writes the PNG to the given path (creating any missing parent directories), prints the absolute path to the test output, and returns the absolute path.  This is particularly useful in environments that can render image files directly — for example, [Claude Code](https://claude.ai/claude-code) will render PNG files it reads from disk.

#### Capturing a single element

The capture methods above shoot the whole tab.  To capture just the first element matching a [selector](#working-with-the-dom) — clipped to its bounding box — use the `...Of` variants:

```go
b.CaptureScreenshotOf("#chart")                       // []byte (PNG), clipped to the element
b.CaptureImgcatScreenshotOf("#chart")                 // string, iTerm imgcat format
path := b.CaptureScreenshotOfToFile("#chart", "/tmp/chart.png") // PNG file, returns the absolute path
```

These accept any Biloba selector (CSS, `XPath`, `Locator`, or a `>>>`-pierced shadow-DOM/iframe selector).  An element below the fold is captured without scrolling, and a same-origin `>>>`-pierced iframe element is translated into top-level page coordinates.  Each fails the spec if no element matches or the element has zero area.

#### Writing failure screenshots to a directory automatically

Pass `BilobaConfigScreenshotsToDir(dir)` to `ConnectToChrome` to have Biloba automatically write each tab's failure screenshot to a PNG file in the given directory:

```go
b = biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigScreenshotsToDir("/tmp/screenshots"))
```

When a spec fails, Biloba writes `screenshot-<spec>-<tab>.png` to the configured directory and prints the absolute path alongside any inline imgcat output.  The directory is created if it does not already exist, and the files survive the spec (unlike Ginkgo's `TempDir()`) so they can be opened after the run.

#### Inline image gating

Biloba emits inline image escape sequences **only when the terminal supports them**.  It auto-detects the terminal and picks the best protocol it can: the [Kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/) where available (best quality; kitty, Ghostty), the [iTerm2 `OSC 1337` protocol](https://iterm2.com/documentation-images.html) (iTerm2, WezTerm, Konsole, …), or [Sixel](https://en.wikipedia.org/wiki/Sixel) (VS Code's integrated terminal, plus older terminals).

Detection uses environment variables that terminals set for themselves — `TERM_PROGRAM` (`iTerm.app`, `vscode`, `WezTerm`, `ghostty`, `rio`), `KITTY_WINDOW_ID`, `TERM=xterm-kitty`, `LC_TERMINAL=iTerm2`, and `KONSOLE_VERSION`.  Note that VS Code maps to Sixel — its integrated terminal renders Sixel but not the iTerm2 protocol.  When none of these match, inline images are off unless you opt into probing or force a protocol.

You can also control the behavior explicitly:

- `BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel` — force a specific protocol regardless of the detected terminal.  `BILOBA_INLINE_SCREENSHOTS=none` disables inline images entirely (useful in CI or terminals such as Claude Code where the base64 blob is pure noise).
- `BILOBA_PROBE_TERMINAL=true` — when env-var detection finds nothing, actively query the terminal (Primary Device Attributes) for Sixel support.  This is opt-in because it briefly puts the controlling TTY into raw mode; it lets Sixel-capable terminals that don't advertise themselves via environment variables (xterm, foot, mlterm, …) light up.
- Pass `BilobaConfigInlineScreenshots(false)` to `ConnectToChrome` to disable inline images programmatically for a specific connection.

When inline images are disabled:

- The inline-image escape sequence is **never emitted**, eliminating ~70 KB of unreadable output per tab per failure.
- If `BilobaConfigScreenshotsToDir` is configured, the file path is still printed and included in the failure report so that tools that can render PNG files (e.g. Claude Code's `Read` tool) can show the screenshot.
- The DOM text outline (see [Outline](#outline)), if you've enabled it, is attached on failure regardless of this setting.

#### Failure artifacts: humans, CI, and agents

When a spec fails, the *kind* of artifact that's useful depends on who's looking.  A human at a terminal wants a screenshot rendered inline; a CI log or an AI agent wants text it can read and image *files* it can open — not a base64 blob smeared across the output.  So Biloba picks a sensible default based on where it's running, and lets you override any piece of it.

**Biloba detects the environment automatically.**  Out of the box, with no configuration:

| | Interactive (human) | Automation (CI or an AI agent) |
|---|---|---|
| Screenshot on failure | yes, inline | yes, written to a directory |
| DOM outline on failure | no | yes |
| Inline image blob | yes (if terminal supports) | no |

"Automation" is detected when `CI` is set, or when an AI coding agent is detected (Claude Code, Cursor, Gemini CLI, Codex, … via [agentdetection](https://github.com/jehiah/agentdetection), which reads signals like `CLAUDECODE` and `AI_AGENT`).  Under automation, screenshots are written to `./biloba-screenshots` by default; point that elsewhere with `BILOBA_SCREENSHOTS_DIR` (handy on CI, where you'd then upload that directory as a build artifact).

So a typical agent or CI run needs **zero configuration** — just run the suite, and failures come back as a DOM outline plus screenshot files on disk.

**Explicit configuration always wins, per knob.**  Anything you set in the suite overrides just that piece of the environment-derived default; everything you leave alone still follows it.  Each toggle takes an optional bool (no argument means `true`):

- `BilobaConfigFailureOutlines()` / `BilobaConfigFailureOutlines(false)` — force outlines on (e.g. for an interactive debugging run) or off (e.g. suppress them under CI).
- `BilobaConfigInlineScreenshots()` / `BilobaConfigInlineScreenshots(false)` — force the inline image blob on or off.
- `BilobaConfigScreenshotsToDir(dir)` — write screenshots to `dir` (this also makes inline and on-disk complementary).
- `BilobaConfigFailureScreenshots(false)` — turn failure screenshots off entirely.
- `BilobaConfigPollTrajectory(false)` — turn off the [poll-trajectory](#outline) artifact (it is on by default for everyone).

For example, a CI user who only wants screenshots in a specific folder sets `BilobaConfigScreenshotsToDir("./artifacts")` (or `BILOBA_SCREENSHOTS_DIR`) and *still* gets the automation default of outlines-on — they only overrode the directory.

### Outline

`b.Outline()` returns the current page DOM as indented, human-readable text — a compact structural view of what's on the page.  This is the primary tool for understanding _why_ a selector didn't match when a spec fails:

```go
fmt.Println(b.Outline())
```

might produce something like:

```
<div id="app">
  <h1>
    Welcome
  </h1>
  <button id="submit" disabled="">
    Save
  </button>
</div>
<script>…</script>
```

`Outline()` automatically prunes the content of `<script>`, `<style>`, and `<svg>` elements (keeping the tags, replacing bodies with `…`) to keep the output compact even on complex SPAs. Runs of whitespace inside text nodes are collapsed to a single space. Output is capped at ~32 KB; if truncated, a `... [truncated]` marker is appended.

When you're debugging a failure whose interesting DOM lands past the cap, override it with the `BILOBA_OUTLINE_MAX` environment variable: set it to a byte count (e.g. `BILOBA_OUTLINE_MAX=131072`) to raise the cap, or to `0`/`off` to disable truncation entirely and emit the whole DOM.

**Attachment on failure.** Biloba can attach a DOM Outline for every open tab when a spec fails.  This gives you a readable, text-based view of the page state, which is especially useful in environments that cannot render images.  It is **off for an interactive human** (the screenshot is the more useful artifact) but **on automatically under CI or an AI agent**; force it either way with `BilobaConfigFailureOutlines()` / `BilobaConfigFailureOutlines(false)` (see [Failure artifacts](#failure-artifacts)).  When enabled, the entry appears under "DOM Outline for: '<title>'" in the Ginkgo report.

**Console errors on failure.** Biloba streams the page's `console` output to the `GinkgoWriter` as it happens, but on a failure the originating `console.error` (say, the exception behind a React error boundary) is easily lost in the timeline.  So whenever a spec fails Biloba *also* replays every `console.error`/`console.assert` the page logged during the spec, gathered across all tabs, under **"Console errors logged before this failure"** at the **top** of the failure block - usually the fastest path to the root cause.  (This requires no configuration and rides along with the failure-artifact hook.)

**Poll trajectory on failure.** When an `Eventually(...)` over a *polled read* times out, the message Gomega prints is a snapshot of the final value — `Timed out … Expected <int>: 120` — and that single number hides three completely different root causes:

| What the polled value did over the deadline | Root cause | Fix |
|---|---|---|
| held at `587` across every poll | the product computed it once and never reconciled | fix the product |
| `587 → 540 → … → 130`, didn't quite land | latency — it nearly made it | widen the timeout |
| reached `~24`, then rebounded to `300` | a late reflow shoved it back | bounded `ResizeObserver` |

The *trajectory* is the diagnosis, so Biloba records it.  Every polled read — a [`b.Run`](#running-arbitrary-javascript)/`b.RunAsync` evaluation, a value getter like [`b.GetProperty`](#properties), or a [geometry getter](#geometry) — appends its `(elapsed, value)` to a small per-tab recorder keyed by the probe.  Biloba tracks the **most recently polled entity** (when the probe changes, the prior series resolved and moved on), and on failure attaches that series, run-length-collapsed so a string of identical values folds into one row:

```
Poll trajectory
Probe: Run document.querySelector("#card").getBoundingClientRect().top
18 samples over 2.00s, 1 distinct values — flat (value never changed: the page is not re-evaluating this probe):
  +0.00s  587   (held ×18 through +2.00s)
```

A flat line points straight at "compute-once product bug, no source-reading required"; a monotone staircase reads as latency; a dip-then-climb reveals the late reflow.  This is **on by default** and rides the same failure-artifact hook as the outline and screenshot (so a passing spec pays only a few nanoseconds per poll to record, and emits nothing).  Turn it off with `BilobaConfigPollTrajectory(false)`.

You can also call `b.Outline()` directly in a spec to capture a snapshot at any point:

```go
AddReportEntry("DOM before click", b.Outline(), ReportEntryVisibilityFailureOrVerbose)
b.Click("#submit")
```

### Accessibility Outline

`b.A11yOutline()` is a companion to `b.Outline()`.  Instead of the raw DOM, it returns the page's **accessibility tree** as indented text - one line per node, showing each node's ARIA role and accessible name:

```go
fmt.Println(b.A11yOutline())
```

```
RootWebArea "My App"
  heading "Welcome"
    StaticText "Welcome"
  textbox "Search"
  button "Submit"
```

This is the same role/name view a screen reader works from (and that reasoning models increasingly rely on), so it's often *more* useful than raw HTML for understanding a page: nodes that are ignored for accessibility (and presentational `InlineTextBox` noise) are elided, while semantics like roles, names, and values are surfaced.  Use it when you want to reason about what the page *means* rather than how it's marked up.  Like `Outline()`, the output is capped at ~32 KB.  It is not auto-attached on failure - call it explicitly when you want it.

### Configuration

Both `SpinUpChrome` and `ConnectToChrome` support a variety of configuration options.

`SpinUpChrome(GinkgoT(), ...)` accepts a set of `SpinUpOption`s:

- `biloba.HighFidelityHeadless()` runs the full ("new") headless Chrome instead of the default `chrome-headless-shell` (see [Headless Fidelity](#headless-fidelity)).
- `biloba.AutoInstallHeadlessShell()` downloads `chrome-headless-shell` via Chrome for Testing if it can't be found locally, instead of failing with instructions.
- `biloba.HeadlessShellPath(path)` points Biloba at a specific `chrome-headless-shell` binary (the `BILOBA_CHROME_HEADLESS_SHELL` environment variable does the same).
- `biloba.StartingWindowSize(width, height)` sets the default window size for all tabs.
- `biloba.ChromeFlags(...)` passes raw [`chromedp.ExecAllocatorOption`s](https://pkg.go.dev/github.com/chromedp/chromedp#ExecAllocatorOption) through to the Chrome process, letting you control [all manner of Chrome settings](https://github.com/chromedp/chromedp/blob/696afbda1c13788a234e9ebc0f4cd5e19e744f02/allocate.go#L56-L84).

For example, to watch the browser by running headful (which implies high fidelity):

```go
SpinUpChrome(GinkgoT(), biloba.ChromeFlags(chromedp.Flag("headless", false)))
```

and sit back and watch those windows appear and disappear as you run your specs.  (You can also just set `BILOBA_INTERACTIVE=true` - see [Debugging](#debugging).)

`ConnectToChrome(GinkgoT(), ...)` supports a more limited set of options that are more specific to Biloba.  Here's a quick summary:

The boolean options take an optional bool — calling them with no argument means `true`, and you pass `false` to turn the feature off (e.g. `BilobaConfigFailureScreenshots(false)`).

- `BilobaConfigDebugLogging(...bool)` will send all Chrome DevTools protocol traffic to the `GinkgoWriter`.  This can be useful when debugging specs and/or implementing your own more advanced `chromedp` behavior.  Fair warning, though: these logs are verbose!
- `BilobaConfigWithChromeConnection(cc ChromeConnection)` allows you to specify your own Chrome connection settings (typically a `WebSocketURL`)
- `BilobaConfigFailureScreenshots(...bool)` controls Biloba's screenshots on failure (on by default)
- `BilobaConfigFailureOutlines(...bool)` controls the DOM outline attached on failure (off for an interactive human, on under automation - see [Failure artifacts](#failure-artifacts))
- `BilobaConfigProgressReportScreenshots(...bool)` controls Biloba's screenshots when progress reports are requested (on by default)
- `BilobaConfigInlineScreenshots(...bool)` controls the inline-image blob in failure and progress-report output (on for a supported interactive terminal, off under automation - see [Inline image gating](#inline-image-gating))
- `BilobaConfigFailureScreenshotsSize(width, height)` specifies the window size to use when generating a screenshot on failure
- `BilobaConfigProgressReportScreenshotSize(width, height)` specifies the window size to use when generating a screenshot when progress reports are requested
- `BilobaConfigScreenshotsToDir(dir)` writes each tab's failure screenshot to a PNG file in the given directory and prints the absolute path to test output (see [Saving screenshots to files](#capturing-screenshots))

### Debugging

The configuration outlined above has to be added to your code.  But sometimes you just want to focus a single failing test, run chrome with headless mode turned off, watch as the test fails, and then play with the browser.  You can do this by setting the `BILOBA_INTERACTIVE=true` environment variable:

```bash
BILOBA_INTERACTIVE=true ginkgo
```

Biloba will run with `headless` set to `false` and will emit the failure message when a spec fails and then pause until you send a `^C` signal to end the suite.  You should generally do this with a small handful of focused spec and only in serial (running in non-headless mode in parallel is... a lot).

{% endraw  %}
