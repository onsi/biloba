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

> Why should I use this thing when far more mature tools like [puppeteer](https://pptr.dev), [selenium](https://www.selenium.dev), and [capybara](https://github.com/teamcapybara/capybara) exist?"

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

#### Headless Fidelity: `chrome-headless-shell` by default {#headless-fidelity}

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

5. Biloba never polls.  Instead, it can return Gomega matchers that _you_ poll with Eventually.

	This allows you to be explicit about when an interaction should succeed immediately vs when an interaction needs to poll while the browser gets into the right state.

	We'll dive into this more in the [Working with the DOM](#working-with-the-dom) chapter below, but as a quick example: `b.Click("#submit")` will immediately click the element with `id` `submit`.  This will only pass if the element exists, is visible, and interactible when `b.Click` is called.  But, perhaps the page is still loading.  Rather than have a separate polling readiness check you can simply write: `Eventually("#submit").Should(b.Click())`.

	When called with an argument, `b.Click` is invoked immediately and will fail the test if it fails.  When invoked without an argument, `b.Click` returns a Gomega matcher that can be polled.

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

### The rest of these docs...

...will cover the breadth of what Biloba offers today.  The focus will be less on exhaustively documenting every function (that's what the [go docs](https://pkg.go.dev/github.com/onsi/biloba) are for) and more on providing mental models and showcasing examples.

## Navigation

You instruct a Biloba tab to navigate to a url via:

```go
b.Navigate("http://example.com/search?q=foo")
```

this navigates the tab and ensures the response was `http.StatusOK`.  If you need to assert a different response code use `NavigateWithStatus("http://example.com/not-found", http.StatusNotFound)`

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

Biloba supports two mechanisms for selecting DOM elements.  CSS selectors (i.e. what you'd pass into `document.QuerySelector()`) and XPath Queries (i.e. what you'd pass into `document.evaluate`).  Throughout this chapter you'll use the word `selector` in code snippets and examples.  If you pass a `string` in as `selector`, Biloba interprets that string as a CSS query.  For example:

```go
b.Click("button.submit")
```

will select the **first** `<button>` DOM element with class `submit` and click on it.  If you'd like to learn more about CSS query selectors the [MDN docs](https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_Selectors) are fantastic.

To perform an XPath query you pass a Biloba `XPath()` object in as `selector.  `XPath` objects can be constructed using `b.XPath()`.  You can specify the XPath manually:

```go
b.Click(b.XPath("//button[contains(concat(' ',normalize-space(@class),' '),'submit')]"))
```

or using Biloba's mini-XPath DSL:

```go
b.Click(b.XPath("button").WithClass("submit"))
```

We'll dive into the XPath DSL in more details at the end of this chapter.  As you can see from this example, it can help generate some fairly complex XPath queries with a much simpler series of invocations.

If you'd like to learn more about XPath queries the [MDN docs](https://developer.mozilla.org/en-US/docs/Web/XPath) will give you a good mental model while the [XPath Cheatsheet at devhints.io](https://devhints.io/xpath) is a fantastic, concise, reference.  If you're more familiar with CSS queries it's definitely a worthwhile investment of effort to learn XPath - as a whole class of queries that aren't possible with CSS queries are straightforward with XPath.

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

#### Piercing Shadow DOM and iframes {#piercing}

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

This pierces **open shadow roots** and **same-origin iframes**.  It cannot reach into **closed** shadow roots or **cross-origin** iframes - the browser does not expose their contents to JavaScript, so a selector targeting them simply won't match (drop down to chromedp's frame handling for cross-origin frames).  `>>>` is a CSS-only feature; XPath selectors do not cross boundaries.

Now that we know how to `select` DOM elements - let's dig into what we can do with them.

### Existence, Counting, Visibility, and Interactibility

You can check if a tab has an element matching `selector` with:

```go
hasEl := b.HasElement(selector) //returns bool
```

this runs immediately on the reusable root tab.  To check a different tab, call `tab.HasElement(selector)`.  **The DOM method always operates on the tab it is invoked on.**

As discussed [above](#ginkgo-and-gomega-integration) - Biloba never polls unless you ask it to.  It's common to want to wait until an element exists before taking some action on the page.  To do that you'd need to poll `b.HaElement`... which, with Gomega, could look like this:

```go
Eventually(b.HasElement).WithArguments(selector).Should(BeTrue())
```

...but that's wordy and hideous and will have the deeply unsatisfying failure message of `"Expected false to be true"`.  Instead you should use `b.Exist()` which returns a matcher:

```go
Expect(selector).To(b.Exist()) // assert that the element is there right now
Eventually(selector).Should(b.Exist()) // assert that the element exists, eventually
```

if you want to assert the existing of `selector` on a different tab you would:

```go
Eventually(selector).Should(tab.Exist())
```

note that we use `tab`'s `Exist()` matcher here instead of the reusable root tab `b`.

Both `HasElement()` and `Exist()` succeed simply if the `selector` query returns an element.

---

You can count the number of elements that match a selector with:

```go
b.Count(selector)
```

or - as a matcher:

```go
Expect("a").To(b.HaveCount(7))
Eventually("img.thumbnail").Should(b.HaveCount(BeNumerically(">", 10)))
```

if no elements match the `selector`, `Count/HaveMatch` return `0`.  Obviously.

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

### Contents and Classes

You can get the `innerText` of an element with `InnerText()`:

```go
text := b.InnerText(selector) //returns string
```

If the element does not exist, `InnerText` will fail the test for you.  If you want to make an assertion on the text and, especially, if you want to pull until the text matches an assertion use `HaveInnerText()`:

```go
Eventually(selector).Should(b.HaveInnerText("Expected text goes here"))
```

you can pass `b.HaveInnerText` a string to require an exact match, or an appropriate Gomega Matcher:


```go
Eventually(selector).Should(b.HaveInnerText(ContainSubstring("text")))
Eventually(selector).Should(b.HaveInnerText(HavePrefix("Expected")))
//etc...
```

Both `HaveInnerText` and `InnerText` always operate on the **first** element matching `selector.

`HaveInnerText` requires an _exact_ match (modulo any Gomega matcher you provide).  This can be annoying when templating introduces incidental whitespace - leading/trailing spaces, newlines, or runs of spaces that you don't care about.  For those cases reach for `HaveText()`, which trims and collapses all internal whitespace runs down to single spaces _before_ matching:

```go
//if the element's innerText is "\n  Hello   there\n\n  Biloba!\n"
Eventually(selector).Should(b.HaveText("Hello there Biloba!")) //passes
Eventually(selector).Should(b.HaveText(ContainSubstring("there Biloba"))) //passes
```

Like `HaveInnerText`, `HaveText` accepts either a string (for an exact, post-normalization match) or a Gomega matcher, and operates on the **first** element matching `selector`.

You can fetch the content for a bunch of elements simultaneously with `InnerTextForEach()/EachHaveInnerText()`:

```go
texts := b.InnerTextForEach(selector) // returns []string
```

returns a slice of strings for all elements matching selector.  For example:

```go
list := b.InnerTextForEach("ol.movies li")
```

will return the individual inner texts for each list element under all `<ol>`s with class `movies`.  If no elements are found `list` will be an empty slice.

You can assert on InnerTextForEach with `b.EachHaveInnerText()` like so:

```go
Expect(selector).To(b.EachHaveInnerText("A", "B", "C")) //uses Gomega's HaveExactElements matcher to assert the texts match, in order
Expect(selector).To(b.EachHaveInnerText(ContainElement("B")) //passes the entire slice to the matcher
Expect("#non-existing").To(b.EachHaveInnerText()) // this will succeed - an empty slice is returned when there is no selector match and `b.EachHaveInnerText() will assert that the slice is empty
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

---

A handful of additional matchers cover common assertions on the **first** element matching `selector`:

`HaveAttribute()` asserts on an element's HTML _attribute_ (via `getAttribute`).  This is distinct from `HaveProperty` (below), which asserts on a javascript _property_ - the two frequently diverge (e.g. the `href` attribute is the raw `"/about"` whereas the `href` property is the resolved absolute URL).  Pass just a name to assert the attribute exists, or a name and an expected value (string or Gomega matcher):

```go
Eventually(selector).Should(b.HaveAttribute("href")) //the attribute is present
Eventually(selector).Should(b.HaveAttribute("href", "/about")) //exact value
Eventually(selector).Should(b.HaveAttribute("href", HaveSuffix("about"))) //matcher
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

You use `GetProperty/SetProperty/HaveProperty` to work with a **single** property on a **single** element (the first returned by `selector`).  You use `GetPropertyForEach/SetPropertyForEach/EachHaveProperty` to work with a **single** property for **all** elements matching `selector`.  You use `GetProperties` to fetch **multiple** properties for a **single** element and `GetPropertiesForEach` to fetch **multiple** properties for **all** elements matching `selector`.

All of these methods follow the following rules:

- If the method operates on a **single** element, it always fails if the element is not found
- If the method is an `Each` method that operates on **all** elements it returns an empty slice if no element is found.  Otherwise it returns a slice matching the length of the number of elements found.
- The `Get*` methods return `nil` if no property is found.  The `Each` variants will include `nil` in their returned slice for elements that don't have the property.
- All methods support `.` property delimiters.  For example you can access `data` attributes using `dataset.key`.  `Set*` methods will fail if the delimiter chain cannot be traversed (e.g. setting `foo.bar.baz` fails if either `foo` or `bar` re not defined on the element.  But `dataset.newKey` will succeed as `dataset` _is_ defined).  `Get*` do not fail, but simply return `nil` if the delimiter chain cannot be traversed.
- All properties are returned from JavaScript without type conversions: numbers will be `float64`, booleans will be `bool`, and strings will be `string`.  Arrays will be `[]any` and maps `[any]any`.  Anything `null`/`undefined` will be `nil`.  There are, however, two exceptions:
	- JavaScript properties that are [iterable](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Iteration_protocols) will be turned into `[]any` when returned (this allows `GetProperty(selector, "classList")` to return a slice).  
	- JavaScript properties of type `DOMStringMap` will be turned into `map[any]any` - this allows you get all `data` attributes via `GetProperty(selector, "dataset")`.

Let's show these in use to cover some additional nuances.  You can get any JavaScript property defined on an element via `GetProperty()` for example:

```go
property := b.GetProperty(selector, "href") //returns type any
```

this will query the DOM immediately and return the property value of the **first** element matching `selector`.  The value will have type `any` and the actual type will depend on what was stored in the property in JavaScript.  If no element matching `selector` is found the test will fail.  If an element is found, but doesn't have the requested property then - in keeping with JavaScript - `nil` is returned.

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

You can also set properties with `b.SetProperty()`.  When passed three arguments `b.SetProperty` operates immediately:

```go
b.SetProperty(selector, "href", "http://www.example.com/")
b.SetProperty(selector, "dataset.name", "Bob")
```

when passed two arguments, it returns a matcher that can be polled:

```go
Eventually(selector).Should(b.SetProperty("dataset.name", "George"))
```

`SetProperty` fails if selector doesn't match an element.  It will also fail if a delimited property (e.g. `foo.bar.baz`) can't be accessed.

To operate on every element returned by the `selector` use `GetPropertyForEach/EachHaveProperty/SetPropertyForEach`.  Here's how they work:

```go
b.GetPropertyForEach(".notice", "id")
```

will return a **slice** of type `[]any` that contains the `id` property of all elements matching the `.notice` selector.  If no elements are found, an empty slice is returned.  If any elements don't have the requested property, the value in the slice for that element will be `nil`.  You can make assertions on the returned value like so:

```go
Expect(b.GetPropertyForEach(".notice", "id")).To(HaveExactElements("A", BeNil(), "C"))
Expect(b.GetPropertyForEach(".notice", "dataset.name")).To(ContainElement("Bob"))
Expect(b.GetPropertyForEach(".does-not-exist", "foo")).To(BeEmpty())
```

(note that you must use `BeNil` instead of `nil` in Gomega's collection matchers)

Alternatively, you can use `EachHaveProperty` to make an assertion directly and/or to poll:

```go
//assert that every .notice has a dataset.name defined on it
Eventually(".notice").Should(EachHaveProperty("dataset.name"))

//require an exact match - note that you can specify nil to assert that an element does not have this property
Eventually(".notice").Should(EachHaveProperty("dataset.name", "Bob", "George", nil, "John"))

//use a matcher - this ensures that there are is at least a .notice with name Bob and one with name George
Eventually(".notice").Should(EachHaveProperty("dataset.name", ContainElements("Bob", "George")))

//if you don't care about order, use ConsistOf 
Eventually(".notice").Should(EachHaveProperty("dataset.name", ConsistOf(BeNil(), "John", "Bob", "George")))

// if you want all attribute values to be the same, use Gomega's `HaveEach`:
Eventually(".notice").Should(EachHaveProperty("disabled", HaveEach(BeFalse())))
```

You can use `SetPropertyForEach` to set the specified property to the specified value for **all** matched elements.  Since we're pointing at the set of _all_ elements matched by a selector it makes less sense to poll, so `SetPropertyForEach` does not provide a matcher variant.  You can use it like this:

```go
b.SetPropertyForEach(b.XPath("li").WithText("Seventeen"), "count", 17)
b.SetPropertyForEach(".notice", "dataset.name", "John")
```

Now all elements matching `<li>Seventeen</li>` will have a `count` property set to `17`; and all elements with class `notice` will have a `name` data attribute with value `John`.  If no elements match... nothing happens.  The only way `SetPropertyForEach` fails is if you provide a delimited property that it cannot traverse (e.g. `foo.bar.baz` - if either `foo` or `bar` do not already exist).

Often it can be more convenient, and efficient, to work with multiple properties at once.  You can do this with `GetProperties` and `GetPropertiesForEach`.  Unlike the other property-related methods in this section these return type `biloba.Properties` and `biloba.SliceOfProperties` to help with managing types (which can quickly get unwieldy when you're working with `[]map[string]any`).

You use `GetProperties` to get multiple properties for a `selector` at once:

```go
props := b.GetProperties(".notice", "classList", "tagName", "disabled", "offsetWidth", "dataset.name")
```

this will fail if no element matches `selector`.  The object returned, `props`, will have `type Properties map[string]any` - you can access defined properties with map notation: e.g. `props["classList"]`.  However this will always return type `any`.  You can, instead, use `Properties`' various getters to force a type conversion:

```go
props.GetString("tagName") //returns a string
props.GetInt("offsetWidth") //returns an integer
props.GetFloat64("offsetWidth") //returns a float64
props.GetBool("disabled") //returns a bool
props.GetStringSlice("classList") //returns []string - any `nil` entries in the original []any slice are converted to the empty string ""
```

all of these always return the zero or empty value if the requested property does not exist or came back as `nil` from JavaScript.  e.g. `props.GetFloat64("offsetHeight")` will return `0.0` in our example since we did not request `offsetHeight` in our call to `GetProperties`.  If you choose the wrong type, Biloba will panic - which Ginkgo will catch and fail the test.

Lastly, to fetch multiple properties from multiple elements use:

```go
propsForEach := b.GetPropertiesForEach(".notice", "classList", "tagName", "disabled", "offsetWidth", "dataset.name")
```

here `propsForEach` is `type SliceOfProperties []Properties` and will have zero length if no elements are found.  You can, of course, use index notation to access a particular property and then fetch a particular key: `propsForEach[0].GetString("tagName")` **or** you can generate a typed slice of a particular key for all elements:

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
p := b.GetPropertiesForEach(".notice", "id", "classList", "tagName", "data.name", "href")
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

### Form Elements

Biloba provides three methods to help you get and set the values of input elements. `b.GetValue` gets values, `b.SetValue` sets values, and `b.HaveValue` matches against values.  All three operate on the **first** element that matches their `selector`.

For most input elements you use them like this:

```go
val := b.GetValue("#my-text-input")
b.SetValue("#my-text-input", "your new value")
Eventually("#my-text-input").Should(b.HaveValue("some other value"))
Eventually("#my-text-input").Should(b.HaveValue(ContainSubstring("other")))
```

`GetValue` will fail the test if it can't find a DOM element matching the selector.  If it does, it will return that DOM element's value even if the DOM element is hidden or disabled.  Similarly, `HaveValue` will fail to match if it can't find an element - but will proceed if the element is hidden or disabled.

`SetValue`, on the other hand, requires that the element exist, be visible, and be enabled:

```go
b.SetValue("#my-hidden-numeric-input", 3)
```

will fail the test (assuming `#my-hidden-numeric-input` is not visible).  You can, however, use `b.SetValue` _as a matcher_:

```go
Eventually("#my-temporarily-hidden-numeric-input").Should(b.SetValue(3))
```

If `SetValue` has two arguments - it operates immediately and fails the test if it runs into issues.  If it only has one argument it returns a matcher that you can poll.

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

But `checkboxes`, `radio` buttons, and `<select multiple>` elements all behavior differently.  Biloba rationalizes all these differences for you through `GetValue`/`SetValue`/`HaveValue`

When Biloba sets a value it does the following:
- focus the element
- update its value (either by setting `el.value` or `el.checked` or `el.selected` etc.)
- blur the element
- dispatch an `input` event
- dispatch a `change` event

That should get _most_ web applications to realize that a form input has been set.  Some applications, though, are wired up to real keyboard events (search-as-you-type fields, rich-text editors, hotkeys).  `SetValue` does **not** fire `keydown`/`keypress`/`keyup` - it sets the value directly.  For those cases reach for [Keyboard Input](#keyboard-input) (`b.Type` and `b.SendKeys`), which dispatch genuine key events.

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

Biloba will find the **first** element matching `selector`, confirm that it exists, is visible, and is enabled - and then it will call `element.Click()`.  If any of these checks fail, `b.Click` will fail the test.

You can, alternatively, use `b.Click()` as a matcher:

```go
Eventually(selector).Should(b.Click())
```

this will poll the browser until `selector` points to an element that exists, is visible, and is enabled.  At which point the element will be clicked (just once) and the assertion will succeed.

The ability to use `Eventually` in this way is convenient as it allow you to write code like this:

```go
b.Navigate("http://example.com/homepage")
Eventually("#login").Should(b.Click())
```

you don't need a separate `Eventually` poll to wait for the page to load or the element to appear.

You can also click on every element matching `selector` using:

```go
b.ClickEach(selector)
```

unlike `Click`, `ClickEach` does not have a matcher variant.  It simply clicks on all the elements that match the selector that are also visible and enabled.  Elements that are not visible or enabled are silently skipped.

### Hovering, Focusing, and Scrolling {#interacting-with-elements}

Alongside `Click`, Biloba provides a few more first-class interactions, all following the same dual immediate/matcher convention:

```go
b.Focus("input.search")          // focuses the first matching element (must be visible and enabled)
b.Hover(".menu")                 // fires hover events at the first matching element (must be visible)
b.ScrollIntoView("#footer")      // scrolls the first matching element into view
```

Each also returns a matcher when called with no arguments, so you can poll:

```go
Eventually("input.search").Should(b.Focus())
Eventually(".menu").Should(b.Hover())
Eventually("#footer").Should(b.ScrollIntoView())
```

`Hover` is, like `Click`, a pragmatic simulation: it synchronously dispatches the pointer/mouse events associated with hovering (`pointerover`, `mouseover`, `pointerenter`, `mouseenter`, `mousemove`).  This triggers JavaScript hover handlers - for example a menu that opens on `mouseenter` - but it does **not** activate CSS `:hover` styling, which only responds to a real pointer.  If you need to exercise CSS `:hover`, drop down to chromedp's input domain.

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

### Keyboard Input

`b.SetValue` sets an input's value directly and dispatches `input`/`change` events.  That satisfies most applications, but some are wired up to **real keyboard events** - search-as-you-type fields, rich-text editors, and hotkey handlers all listen for `keydown`/`keypress`/`keyup`.  Biloba cannot synthesize those atomically in JavaScript (the browser forbids synthetic key events from actually typing into the page), so it drops down to `chromedp`'s input domain for you with `b.Type` and `b.SendKeys`.

#### Typing Text

`b.Type` focuses an element and then sends genuine keystrokes - one `keydown`/`keypress`/`keyup` sequence per character:

```go
b.Type("input.search", "gophers")
```

Biloba finds the **first** element matching `selector`, confirms that it exists, is visible, and is enabled, focuses it, and then types the text.  If any of those checks fail, `b.Type` fails the test.  Unlike `SetValue`, `Type` **appends** to whatever is already in the field (it types as a user would) and triggers any key-event listeners.

Like `Click` and `SetValue`, `Type` also works as a matcher so you can poll until the element is ready:

```go
Eventually("input.search").Should(b.Type("gophers"))
```

#### Sending Named Keys

To send named keys - `Enter`, `Tab`, `Escape`, the arrow keys, `Backspace`, etc. - use `b.SendKeys` together with the `biloba.Keys` namespace:

```go
b.Type("input.search", "gophers")
b.SendKeys("input.search", biloba.Keys.Enter) // submit the form
```

When the first argument is a selector, `SendKeys` focuses that element first (failing the test if it is missing, hidden, or disabled).  You can mix text and named keys in a single call:

```go
b.SendKeys("textarea", "Hello", biloba.Keys.Enter, "World")
```

If you omit the selector entirely the keys are sent to whichever element currently has focus.  This is handy for global hotkeys, or for following up on an element you've already focused:

```go
Eventually("#editor").Should(b.Click()) // focuses the editor
b.SendKeys(biloba.Keys.Escape)          // sent to the focused editor
```

The available keys are exposed on `biloba.Keys` (`Backspace`, `Tab`, `Enter`, `Escape`, `Delete`, `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`, `Home`, `End`, `PageUp`, `PageDown`).  Each is a `biloba.Key` - if you need a key that isn't listed you can drop down to `chromedp` via `b.Context` and the [chromedp/kb](https://pkg.go.dev/github.com/chromedp/chromedp/kb) package.

### Invoking JavaScript on and with selected elements

At the end of the day, Biloba can give you a pile of DOM methods and matchers but you'll still come across a usecase that isn't implemented.  For that, you can head straight to JavaScript and get the job done yourself.  The [Running Arbitrary Javascript](#running-arbitrary-javascript) chapter below discusses how to run JavaScript with Biloba in _general_.  But in this section we focus on how to use Biloba to run JavaScript against selected DOM elements (which - of course, you can do with arbitrary JavaScript, but the API outlined here does the work of selecting elements for you using the same `selector` infrastructure we've discussed throughout this chapter).

You can invoke a method defined on a DOM element (e.g. `focus()` or `scrollIntoView()`) with:

```go
b.InvokeOn(selector, methodName, <optional args>)
```

`InvokeOn` operates on the **first** matching element (failing if none are found) and returns whatever the called method returns.  You can also pass arguments in - some examples:

```go
b.InvokeOn("#submit", "click") //though you should really just use b.Click("#submit")
b.InvokeOn("input[type='text']", "focus") //finds the first matching element then calles el.focus()
b.InvokeOn("input[type='text']", "scrollIntoView") //finds the first matching element then calles el.scrollIntoView()
b.InvokeOn("h1.title", "append", " - Hello") //calls el.append(" - Hello")
r := b.InvokeOn(".notice", "getAttributeNames") // r has type any but is a slice of strings containing all attribute names
b.InvokeOn(".notice", "setAttribute", "data-age", "17") // calls el.setAttribute("data-age", "17")
Expect(b.InvokeOn(".notice", "getAttribute", "data-age")).To(Equal("17")) // will now pass
```

Similarly, you can use `InvokeOnEach` to invoke a method and arguments on **all** matching elements.  Nothing happens if no elements match and there is no way, currently, to specify different arguments for different matching elements.

The upshot is that `InvokeOn/InvokeOnEach` find elements then call `el[methodName](...args)`.  This works well if the element has a relevant method defined on it.

If you want to do something more complex with the element - or you want to call several methods atomically - you can use `InvokeWith/InvokeWithEach`.  These take a callable snippet of JavaScript and invoke it - passing in the element along with any optional arguments you've provided, and returning the result.  Here's an example:

```go
countCharacters := `(el) => len(el.innerText)`
Expect(b.InvokeWith(".notice", countCharacters)).To(Equal(12.0))
Expect(b.InvokeWithEach(".notice", countCharacters)).To(HaveExactElements(12.0, 4.0, 73.0))

appendLi := `(el, text) => {
	let li = document.createElement('li')
	li.innerText = text
	el.appendChild(li);
}`
b.InvokeWith("ul", appendLi, "Another Item") //runs on the first <ul>
b.InvokeWithEach("ul", appendLi, "Another Item For All") //runs on all <ul>s
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

#### Text selectors

A common need is to find "the element that says X" — for example, clicking the button labelled "Submit" or waiting for a confirmation message to appear.  Rather than writing `b.XPath().WithText("Submit")` every time, Biloba provides two top-level shortcuts that mirror the XPath DSL's `WithText`/`WithTextContains` vocabulary:

```go
// select any element whose full text is exactly "Submit"
b.WithText("Submit")

// select any element whose text contains the substring "Subm"
b.WithTextContains("Subm")
```

Both return an `XPath` value and compose freely with every Biloba action and matcher:

```go
b.Click(b.WithText("Submit"))
Eventually(b.WithText("Save changes")).Should(b.BeVisible())
Eventually(b.WithTextContains("Welcome")).Should(b.Exist())
```

`b.WithText(text)` is sugar for `b.XPath().WithText(text)` and `b.WithTextContains(text)` is sugar for `b.XPath().WithTextContains(text)`.  Both match any element type; refine with the full XPath DSL when you need a specific tag:

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

The key difference from `b.Run` is the `return`.  Because your script is the body of a function (not a bare expression) you must `return` the value you want out of it - a script that forgets to `return` will resolve to `undefined`.

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

### Capturing Screenshots {#capturing-screenshots}

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

**Attachment on failure.** Biloba can attach a DOM Outline for every open tab when a spec fails.  This gives you a readable, text-based view of the page state, which is especially useful in environments that cannot render images.  It is **off for an interactive human** (the screenshot is the more useful artifact) but **on automatically under CI or an AI agent**; force it either way with `BilobaConfigFailureOutlines()` / `BilobaConfigFailureOutlines(false)` (see [Failure artifacts](#failure-artifacts)).  When enabled, the entry appears under "DOM Outline for: '<title>'" in the Ginkgo report.

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
