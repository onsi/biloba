# Biloba

> "Automated browser testing is slow and flaky" - _every developer, ever_

Biloba builds on top of [chromedp](https://github.com/chromedp/chromedp) to bring stable, performant, automated browser testing to Ginkgo. It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

We'll unpack these throughout this document - which is intended as a supplement to the API-level [godocs](https://pkg.go.dev/github.com/onsi/biloba) to give you a mental model for Biloba.

### Support Policy

Biloba is currently under development.  Until a v1.0 release is made there are no guarantees about the stability of its public API.

## Getting Started

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

> "Why are 'spinning up Chrome' and 'connecting to Chrome'" separate commands?"

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

1. Query the document for a node matching the selector passed to `click`
2. Scroll the element into view
3. Compute the `(x,y)` centroid of the element in page coordinates
4. Tell the page to click on `(x,y)`

There are **several** benefits to this approach:  First and foremost, this is a a _realistic_ emulation of how human users will interact with the site under test.  They will scroll to the element to bring it to view, the will position the cursor over the element, and then they will click.  This can help catch all manner of accidental bugs: perhaps a transparent element is masking the element in question, absorbing the click - if so the test will fail.  Moreover, by scrolling and clicking Puppeteer/chromedp can more literally show you what's going on when running in non-headless mode: the page scrolls, the element is clicked.

But.  All this realism comes at a cost.  One is performance - there are several steps at play here; each a separate roundtrip to chrome that may be invoking javascript or calling the DevTools protocol.  Of course, any single click will be fast enough - but this overhead will start to add up as the test suite grows.

> Whatever.  Computers are plenty fast - so the performance concerns here are relatively minor.

True.  But it's the potential stability issues that raise deeper concerns.  Each step during the `click` event is an asynchronous call to Chrome and so the entire click event does not constitute a single atomic unit - all sorts of stuff can happen between those individual steps.  That node you found during your query?  Well, by the time you're scrolling it into view... it's gone, perhaps because [React](https://reactjs.org) or [Mithril](https://mithril.js.org) or what-have-you was performing an asynchronous redraw.  Sure, the _selector_ would still return a valid element.  But the concrete, specific, node identified by it the first time around is no longer present.  Fail.

Or, perhaps you've computed the `(x,y)` centroid and are about to click on the element when some image download/ajax request/what-have-you completes and causes the page to reflow.  The element moves.  The click misses, perhaps hitting some other dom node, leading to a fairly difficult to debug Heisenbug.

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
- Click on it via Javascripts `node.Click()`

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
		return storage.SetCookies([]*network.CookieParam{Name:"user", User:"Joe", Expires:&expr, Domain:"localhost").WithBrowserContextID(b.BrowserContextID()).Do(ctx)
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

## Tabs

Biloba encourages you to reuse the root tab (`b`) for most of your specs.  Of course some tests will require you to interact with multiple tabs.  There are, loosely speaking, three different "kinds" of tabs at play in Biloba:

- `b` the reusable root tab.
- new tabs that you explicitly create.  We'll simply call these "Tabs".
- tabs that are created by the browser as the result of a user interaction.  For example clicking on an anchor tag with `target="_blank"`.  We'll call these "Spawned Tabs".

In all three cases the tab is an instance of `*Biloba` and in general all three kinds of tabs are similar except for the following differences:

1. The reusable root tab is never closed.  It is simply reused between specs.  All other tabs are automatically closed by `b.Prepare()`.
2. Explicitly created Tabs are given their own `BrowserContextID`.  This is an important isolation mechanism in Chrome that allows different tabs to be in different isolated universes.  You can think of it as "incognito mode" where each new tab is in its own incognito mode.
3. Spawned Tabs inherit the `BrowserContextID` of the tab that spawned them.

### Creating and Closing Tabs Explicitly

You create Tabs using `tab := b.NewTab()`.  Here's a pseudocode example that shows us testing a multi-user chat application.  It includes an example of reusable helper function to handle logging in to the site, a well as extensive use of `Xpath` selectors to perform complex DOM queries:

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
		userXpath := b.Xpath("div").WithID("user-list").Descendant()
		// b should show both users
		Eventually(userXpath.WithText("Sally")).Should(b.HaveClass("online"))
		Eventually(userXpath.WithText("Jane")).Should(b.HaveClass("online"))
		// tab should show both users
		Eventually(userXpath.WithText("Sally")).Should(tab.HaveClass("online"))
		Eventually(userXpath.WithText("Jane")).Should(tab.HaveClass("online"))
	})

	It("shows Jane when Sally is typing", func() {
		lastEntryXpath := tab.Xpath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		Eventually(lastEntryXpath).Should(SatisfyAll(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))

		b.SetValue("#input", "")
		Eventually(lastEntryXpath).ShouldNot(SatisfyAny(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))
	})

	It("shows Jane new messages from Sally, and sally new messages from Jane", func() {
		lastEntryXpath := tab.Xpath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		b.Click("#send")
		Eventually(lastEntryXpath).Should(tab.HaveInnerText("Hey Jane, how are you?"))

		tab.SetValue("#input", "I'm splendid, Sally!")
		tab.Click("#send")
		Eventually(lastEntryXpath).Should(b.HaveInnerText("I'm splendid, Sally!"))
	})

	It("tracks when users aren't online", func() {
		userXpath := b.Xpath("div").WithID("user-list").Descendant()
		Eventually(userXpath.WithText("Jane")).Should(b.HaveClass("online"))

		tab.Close()
		Eventually(userXpath.WithText("Jane")).Should(b.HaveClass("offline"))
	})
})
```

Since each created tab is in its own `BrowserContextID` we don't have to worry about cookie pollution and can log Sally and Jane in on the separate tabs.  As you can see we direct matchers and actions towards a given tab by using the version of the matcher/action on that tab instance (e.g. `tab.HaveClass` vs `b.HaveClass`).  You can also see that non-root tabs can be closed via `tab.Close()`.

### Finding and Managing Spawned Tabs

If a browser action results in a _new_ tab being opened you'll need to get a Biloba instance that attaches to that tab in order to be able to use it.  There are three methods that support this:

- `tab.AllSpanedTabs()` returns a list of tabs (wrapped in Biloba instances) that were spawned by a user action performed on `tab`.
- `tab.FindSpawnedTab(filter)` returns a Biloba tab matching the passed-in filter (see below)
- `tab.HaveSpawnedTab(filter)` returns a matcher that asserts whether or not a spawned tab matches the passed-in filter (see below)

You can either query `AllSpawnedTabs` and search through them for the tab you're looking for, or use `FindSpanwedTab` with a filter function that has signature `func(*Biloba) bool`.  Biloba provides three filter functions out of the box:

- `TabWithDOMNode(selector)` matches if the spawned tab has a DOM node satisfying `selector`.
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
		lastEntryXpath := tab.Xpath("#conversation").Descendant().WithClass("entry").Last()
		Eventually(lastEntryXpath).Should(tab.HaveInnerText("Hey Jane, check this out: YouTube"))
		tab.Click(lastEntryXpath.Child("a"))
		Eventually(tab).Should(tab.HaveSpawnedTab(tab.TabWithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")))
		youtubeTab := tab.FindSpawnedTab(tab.TabWithURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ"))
		Eventually("body").Should(youtubeTab.HaveInnerText(ContainSubstring("Never Gonna Give You Up")))
	})
})
```

As you can see, we poll `tab.HaveSpanwedTab` until the tab appears and then use `tab.FindSpawnedTab` to get a reference to it. From here we can make assertions against the spawned tab and/or close it.

Note that `b.HaveSpawnedTab` will have failed.  That's because Biloba associates spawned tabs with the `BrowserContextID` of the tab that opened them.  And both `b` and `tab` (which is an explicitly created tab) have _different_ `BrowserContextID`s.

There are analogous `b.AllTabs()`, `b.HaveTab()` and `b.FindTab()` functions that let you search through _all_ tabs associated with this Biloba Chrome connection.  This won't include any tabs opened by other Ginkgo processes running in parallel - but any tabs that are associated with the current process (whether explicitly created Tabs or Spawned Tabs) will be returned by these methods.

## Working with the DOM
#### Mental Model: how Biloba works with the dom

### XPath Queries

## Dialogs, Downloads, and Windows

#### Going the Extra Mile for Stability

Biloba tries to cover its bases

## Running Javascript

Biloba will happily allow you to run arbitrary JavaScript on the page for any tab.  This can often be a convenient shortcut to make a more complex assertion in JavaScript, or to make assertions on the state of your web application that may be overly difficult/complex to make simply by interrogating the DOM alone.

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

One last note: all this Javascript runs on the global window object.  That means you can do stuff like this:

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

You said it, not me.  While this, technically, works the developer experience is fairly `meh`.