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
- Handle downloads and dialog boxes
- Forward all `console.log`s to the `GinkgoWriter`
- Automatically emit [ImgCat screenshots to supported terminals](https://iterm2.com/documentation-images.html) when a test fails...
- ...or whenever a [Ginkgo Progress Report](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) is generated
- Run your specs in parallel with `ginkgo -p`


> "But you're obviously missing X, Y, and Z!"

Biloba is young, and just getting started.  Send in a PR!  Or, if you prefer, just use [`chromedp`](https://github.com/chromedp/chromedp) directly to accomplish what you need.  Or drop down all the way to [`cdproto`](https://pkg.go.dev/github.com/chromedp/cdproto) to use the [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) directly.   Every Biloba tab exposes its `chromedp` context via `b.Context` - so you can mix and match as needed.

> "You just said 'young' and 'getting started.'  Why should I use this thing when far more mature tools like [puppeteer](https://pptr.dev), [selenium](https://www.selenium.dev), and [capybara](https://github.com/teamcapybara/capybara) exist?"

Lol!  You probably shouldn't!  But... if you're building something out in Go, happen to know and like Ginkgo, and want to experiment with a shiny new toy that's aiming to deliver performant non-flakey automated browser tests... Give Biloba a try - and start opening issues and sending in PRs!

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
	
	Biloba registers a Ginkgo hook that runs after each spec.  If the spec has failed a screenshot of every tab associated with that spec is taken and is emitted to the terminal via iTerm2's [image protocol](https://iterm2.com/documentation-images.html) (have a different terminal image protocol?  Open an [issue](https://github.com/onsi/biloba/issues/new)!)

	In addition, Biloba registers a Ginkgo ProgressReporter that will emit screenshots whenever a [progress report](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) is requested.  This can happen when a spec times out, or when a spec decorated with `PollProgressAfter(X duration)` has taken longer than `X` to complete.  On MacOS you can get a progress report instantly by sending a `SIGINFO` signal with `^T`.  On Linux you can send a `SIGUSR2` signal.

	Both of these mechanisms make it just a bit easier to debug a failing test by getting visual feedback.

	(Note that you can disable both of these by passing in `BilobaConfigDisableFailureScreenshots()` and `BilobaConfigDisableProgressReportScreenshots()` to `ConnectToChrome()`)

3. All `console.log/info/warn/etc.` output gets immediately streamed to Ginkgo's GinkgoWriter.

	That means that output will be visible, but only if a test fails or you are running in verbose mode with `ginkgo -v`

4. `console.assert` failures count as test failures.

	This allows you to seamlessly decide whether some assertions are better handled in Javascript vs in Go.

5. Biloba never polls.  Instead, it can return Gomega matchers that _you_ poll with Eventually.

	This allows you to be explicit about when an interaction should succeed immediately vs when an interaction needs to poll while the browser gets into the right state.

	We'll dive into this more in the [Working with the DOM](#working-with-the-dom) chapter below, but as a quick example: `b.Click("#submit")` will immediately click the element with `id` `submit`.  This will only pass if the element exists, is visible, and interactible when `b.Click` is called.  But, perhaps the page is still loading.  Rather than have a separate polling readiness check you can simply write: `Eventually("#submit").Should(b.Click())`.

	When called with an argument, `b.Click` is invoked immediately and will fail the test if it fails.  When invoked without an argument, `b.Click` returns a Gomega matcher that can be polled.

These integrations will continue to evolve and deepen as Biloba matures.

### `chromedp`: Breaking the Fourth Wall

Biloba is only possible because of all the hard work that's gone into the [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) and the [chromedp](https://github.com/chromedp) [client](https://github.com/chromedp/chromedp) and [Go protocols](https://github.com/chromedp/cdproto).  Biloba wraps all this great stuff to give you a productive testing environment - but it doesn't hide any of it.  You can drop down to `chromedp` and `cdproto` and mix and match Biloba with `chromedp` trivially simply by passing in `b.Context ` to `chromedp`.  For example, Biloba doesn't yet have any cookie support.  You can use `chromedp` and `cdproto/network` to set and clear cookies, though:

```go
BeforeEach(func() {
	// set the login cookie
	chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		expr := cdp.TimeSinceEpoch(time.Now().Add(180 * 24 * time.Hour))
		return storage.SetCookies([]*network.CookieParam{{Name:"user", Value:"Joe", Expires:&expr, Domain:"localhost"}}).WithBrowserContextID(b.BrowserContextID()).Do(ctx)
	}))

	// clear all cookies after each test
	DeferCleanup(chromedp.Run, b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		return storage.ClearCookies().WithBrowserContextID(b.BrowserContextID()).Do(ctx)
	}))
})

It("shold be logged int", func() {
	b.Navigate("http://localhost:8080/home")
	Expect("#user-name").To(b.HaveInnerText("Joe"))
})
```

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

- `tab.AllSpanedTabs()` returns a list of tabs (wrapped in Biloba instances) that were spawned by a user action performed on `tab`.
- `tab.HaveSpawnedTab(filter)` returns a matcher that asserts whether or not a spawned tab matches the passed-in Tab filter (see below)

You can query `AllSpawnedTabs` and search through them for the tab you're looking for using `Find()` and `Filter()` - passing in a `TabFilter` (a function of type `func(*Biloba) bool`). Biloba provides three tab filters out of the box:

- `TabWithDOMNode(selector)` matches if the spawned tab has a DOM element satisfying `selector`.
- `TabWithURL(url)` matches if the spawned tab has a matching url
- `TabWithTitle(title)` matches if the spawned tab has a matching title

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
		Eventually(tab).Should(tab.HaveSpawnedTab(tab.TabWithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")))
		youtubeTab := tab.AllSpawnedTabs().Find(tab.TabWithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ"))
		Eventually("body").Should(youtubeTab.HaveInnerText(ContainSubstring("Never Gonna Give You Up")))
	})
})
```

As you can see, we poll `tab.HaveSpanwedTab` until the tab appears and then use `tab.AllSpawnedTabs().Find()` to get a reference to it. From here we can make assertions against the spawned tab and/or close it.

Note that `b.HaveSpawnedTab` will have failed.  That's because Biloba associates spawned tabs with the `BrowserContextID` of the tab that opened them.  And both `b` and `tab` (which is an explicitly created tab) have _different_ `BrowserContextID`s.

There are analogous `b.AllTabs()` and `b.HaveTab()` functions that let you search through _all_ tabs associated with this Biloba Chrome connection.  This won't include any tabs opened by other Ginkgo processes running in parallel - but any tabs that are associated with the current process (whether explicitly created Tabs or Spawned Tabs) will be returned by these methods.

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

Under the hood, `b.XPath()` returns an object of `type XPath string`.  You can use `fmt.Println(x)` to see the XPath query generated by the DSL.

And since the DSL always returns an `XPath` string you can do things like this (from our example above):

```go
//build out a partial XPath query that selects descendants of the div with id "user-list"
userXPath := b.XPath("div").WithID("user-list").Descendant()

//we can now select specific users by adding onto userXPath:
Eventually(userXPath.WithText("Sally")).Should(b.HaveClass("online"))
Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
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

Of course, asserting on length can be brittle and lead to unsatisfying failure messages.  Instead, you should use `b.HaveCompleteDownload` like so:

```go
// we should get a download with the specified filename
Eventually(b).Should(b.HaveCompleteDownload(b.DownloadWithFilename("hello.pdf")))

// we should get a download with the specified content
Eventually(b).Should(b.HaveCompleteDownload(b.DownloadWithContent([]byte{"hello world"})))
```

`b.HaveCompleteDownload()`  takes a download filter of type `func(d *Download) bool` and `b.DownloadWithFilename` and `b.DownloadWithContent` provide such filters.  Note that `b.DownloadWithFilename` is matching on the _suggested_ filename provided by the browser.  The actual filename on disk is opaque and not something you'll need to worry about.

Once the download has completed you can get a reference to it  by accessing `b.AllCompleteDownloads()` directly or using its `Find()` method:

```go
dl := b.AllCompleteDownloads[0]
dl := b.AllCompleteDownloads.Find(b.DownloadWithFilename("hello.pdf"))
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


## Window Size, Screenshots, and Configuration

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

### Capturing Screenshots

As discussed above, Biloba automatically emits screenshots when a spec fails or a progress report is requested.

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

### Configuration

Both `SpinUpChrome` and `ConnectToChrome` support a variety of configuration options.

`SpinUpChrome(GinkgoT(), ...)` will accept arbitrarily many [`chromedp.ExecAllocatorOption`s](https://pkg.go.dev/github.com/chromedp/chromedp#ExecAllocatorOption) after `GinkgoT()`.  You can use these to control [all manner of Chrome settings](https://github.com/chromedp/chromedp/blob/696afbda1c13788a234e9ebc0f4cd5e19e744f02/allocate.go#L56-L84).  To turn off `Headless` mode, for example you can run:

```go
SpinUpChrome(GinkgoT(), chromedp.Flag("headless", false))
```

and sit back and watch those windows appear and disappear as you run your specs.

`ConnectToChrome(GinkgoT(), ...)` supports a more limited set of options that are more specific to Biloba.  Here's a quick summary:

- `BilobaConfigEnableDebugLogging()` will send all Chrome DevTools protocol traffic to the `GinkgoWriter`.  This can be useful when debugging specs and/or implementing your own more advanced `chromedp` behavior.  Fair warning, though: these logs are verbose!
- `BilobaConfigWithChromeConnection(cc ChromeConnection)` allows you to specify your own Chrome connection settings (typically a `WebSocketURL`)
- `BilobaConfigDisableFailureScreenshots()` disables Biloba's screenshots on failure
- `BilobaConfigDisableProgressReportScreenshots()` disables Biloba's screenshots when progress reports are requested


{% endraw  %}
