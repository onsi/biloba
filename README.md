![Ginkgo](https://onsi.github.io/biloba/images/biloba.png)

[![test](https://github.com/onsi/biloba/workflows/test/badge.svg?branch=master)](https://github.com/onsi/biloba/actions?query=workflow%3Atest+branch%3Amaster) | [Biloba Docs](https://onsi.github.io/biloba/)

---

# Biloba

> "Automated browser testing is slow and flaky" - _every developer, ever_

Biloba builds on top of [chromedp](https://github.com/chromedp/chromedp) to bring stable, performant, automated browser testing to Ginkgo. It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

It's blazing fast and designed to work _really_ well with AI toolchains like Claude Code.  

Take a look at the [documentation](https://onsi.github.io/biloba) to learn more and get started!  Biloba tests can be written in Go using Ginkgo, [and in typescript using vitest](#vitest-support).

Here's a quick taste of what Biloba specs look like in Ginkgo:

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

	It("renders a message bubble exactly as designed", func() {
		tabSally.SetValue("#input", "Hey Jane")
		tabSally.Click(b.ByRole("button").WithName("Send"))

		// compare against a committed baseline — masking the volatile timestamp,
		// in both themes.  A failure says what moved and where, in words.
		Eventually(b.ByRole("listitem").Within("#conversation").Last()).Should(
			tabSally.HaveScreenshot("message-bubble",
				tabSally.Mask(".timestamp"),
				tabSally.InColorSchemes("light", "dark")))
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

Browsers are asynchronous, so Biloba's interactions and value-getters **poll by default**.  A fully-applied call like `tab.Click("#go")` or `tab.SetValue("#input", "hi")` retries — finding-and-acting atomically in the browser — until it succeeds or times out.

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

For the handful of specs that need genuine input fidelity — real CSS `:hover`, occlusion-aware clicks, scroll-into-view, real keystrokes/drags/wheel/touch — `b.Realistic()` returns a view of the *same tab* whose interactions route through real Chrome DevTools Protocol input.  Same API, just a more faithful (and slightly slower) interaction engine.  See the [documentation](https://onsi.github.io/biloba) (and the `biloba-go:realistic-mode` Claude Code skill).

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

The same instinct shapes `b.HaveScreenshot`, Biloba's visual-regression matcher: when a comparison against a committed baseline fails, Biloba writes the usual `.actual.png`/`.diff.png` pair *and* tells you in words what changed — how many pixels, where the changed boxes are, and whether the shape of the change reads as one region, a uniform shift, or something scattered across every text run.  Those three are different bugs, and you can tell them apart without opening an image.

### Using Biloba with Claude Code

Biloba ships separate [Claude Code](https://claude.com/claude-code) plugins for its Go/Gomega and TypeScript/Vitest clients, with this repo doubling as the marketplace. Install the client you use:

```
/plugin marketplace add onsi/biloba
/plugin install biloba-go@biloba
/plugin install biloba-vitest@biloba
```

(or use `claude plugin marketplace add onsi/biloba` followed by `claude plugin install biloba-go@biloba` or `claude plugin install biloba-vitest@biloba`.)

The former `biloba@biloba` plugin remains as a deprecated compatibility alias for the Go/Gomega skills during the transition window. Existing `/biloba:*` invocations continue to work, but migrate to `biloba-go@biloba`; install both new client plugins only in repositories that genuinely exercise both clients.

### Vitest Support

Biloba's TypeScript client lets a `vitest` suite drive Chrome through Biloba. **It's a prototype**:
install the `biloba` package from npm, and expect its API to keep shifting before 1.0.
[**Biloba for Vitest**](https://onsi.github.io/biloba/vitest.html) is the documentation: setup and
the shared-browser topology, launch modes, locators, actions and assertions, network control,
screenshots and visual assertions, and structured failures.

`npm install -D vitest biloba` pulls in the daemon binary too — no Go toolchain required. It installs
`biloba` plus one small per-platform package (macOS/Linux, x64/arm64; Windows isn't supported yet).
`npx biloba install-chrome` then fetches the `chrome-headless-shell` build the daemon drives, once per
Chrome version.

Each `vitest` worker process spawns a small Go daemon (`bilobad`) and talks to it over framed JSON on stdin/stdout.  Every daemon attaches to one shared Chrome — the same "one browser, one isolated tab per parallel process" model that makes the Go suites fast.  Polling happens on the daemon, next to Chrome, so an assertion with a 1s timeout and a 5ms interval is *one* request rather than two hundred.

Here's the chat app from the top of this README, in TypeScript.  Actions and assertions poll by default, exactly as they do in Go:

```ts
import {beforeEach, describe, it} from "vitest";
import {contains, Keys, not, type Session} from "biloba";

async function login(tab: Session, user: string, password: string) {
  await tab.navigate("/login");
  await tab.getByLabel("Username").setValue(user);              // locator: a form control by its label
  await tab.getByLabel("Password").setValue(password);
  await tab.getByRole("button", {name: "Log in"}).click();      // locator: role + accessible name
  await tab.locator(".chat-page").expectExists();
}

describe("a simple chat app", () => {
  // session is a root Session opened in a beforeAll (not shown).  We open an isolated tab per
  // user off of it, and generate reusable locators off each tab.
  let tabSally: Session, tabJane: Session;
  beforeEach(async () => {
    tabSally = await session.newTab();
    await login(tabSally, "sally", "yllas");
    tabJane = await session.newTab();
    await login(tabJane, "jane", "enaj");
  });

  it("shows all logged in users as present", async () => {
    // both tabs should show both users online, by the names a user actually reads
    for (const tab of [tabSally, tabJane]) {
      await tab.getByText("Sally").within("#user-list").expectClass("online");
      await tab.getByText("Jane").within("#user-list").expectClass("online");
    }
  });

  it("shows Jane that Sally is typing", async () => {
    const lastEntry = tabJane.getByRole("listitem").within("#conversation").last();
    await tabSally.locator("#input").setValue("Hey Jane, how are you?");
    await lastEntry.expectText("Sally is typing...");
    await lastEntry.expectClass("typing");

    await tabSally.locator("#input").setValue("");
    await lastEntry.expectNotText("Sally is typing...");
    await lastEntry.expectClass(not(contains("typing")));
  });

  it("delivers messages between Sally and Jane", async () => {
    await tabSally.locator("#input").type("Hey Jane, how are you?"); // real keystrokes...
    await tabSally.locator("#input").type(Keys.Enter);               // ...sent by pressing Enter
    await tabJane.getByRole("listitem").within("#conversation").last()
      .expectText("Hey Jane, how are you?");

    await tabJane.locator("#input").type("I'm splendid, Sally!");
    await tabJane.getByRole("button", {name: "Send"}).click();
    await tabSally.getByRole("listitem").within("#conversation").last()
      .expectText("I'm splendid, Sally!");
  });

  it("lets Sally share a document that Jane can download", async () => {
    await tabSally.getByLabel("Attach a file").setUploadFiles(["./fixtures/report.pdf"]);
    await tabSally.getByRole("button", {name: "Send"}).click();

    const doc = tabJane.getByRole("link", {name: "report.pdf"});
    await doc.expectVisible();                              // Jane sees the shared document...
    await doc.click();                                      // ...and downloads it
    await tabJane.expectDownload({filename: "report.pdf"});
  });

  it("reveals message actions on hover", async () => {
    await tabSally.locator("#input").setValue("Hey Jane");
    await tabSally.getByRole("button", {name: "Send"}).click();

    const last = tabSally.getByRole("listitem").within("#conversation").last();
    await last.realistic().hover(); // genuine CSS :hover — one of the few things the fast track can't do
    await tabSally.getByRole("button", {name: "React"}).within(last).expectVisible();
  });

  it("renders a message bubble exactly as designed", async () => {
    await tabSally.locator("#input").setValue("Hey Jane");
    await tabSally.getByRole("button", {name: "Send"}).click();

    // compare against a committed baseline — masking the volatile timestamp,
    // in both themes.  A failure says what moved and where, in words.
    await tabSally.getByRole("listitem").within("#conversation").last()
      .expectScreenshot("message-bubble", {
        mask: [tabSally.locator(".timestamp")],
        colorSchemes: ["light", "dark"],
      });
  });

  it("shows an error when a message fails to send", async () => {
    await tabSally.abortRequest(contains("/messages")); // make the send fail, hermetically
    await tabSally.locator("#input").setValue("Hey Jane");
    await tabSally.getByRole("button", {name: "Send"}).click();
    await tabSally.getByRole("alert").expectText("Message failed to send");
  });

  it("loads conversation history", async () => {
    // stub the history response
    await tabSally.stubRequest(contains("/history"), {
      body: new TextEncoder().encode('[{"from":"Jane","text":"Welcome back!"}]'),
    });
    await tabSally.navigate("/chat");
    await tabSally.getByRole("listitem").within("#conversation").expectText("Welcome back!");
  });

  it("tracks when users aren't online", async () => {
    const jane = tabSally.getByText("Jane").within("#user-list");
    await jane.expectClass("online");

    await tabJane.close();
    await jane.expectClass("offline");
  });
});
```

#### Running them

Install `biloba`, which pulls in the daemon for you — no Go toolchain needed on macOS or Linux (x64 or arm64; Windows isn't supported yet):

```bash
npm install -D vitest biloba
npx biloba install-chrome   # once per Chrome version
```

Start one Chrome for the whole run in vitest's global setup and hand its connection to the workers.  Register that setup — and a process pool, so each test file really is its own worker with its own daemon — in your vitest config:

```ts
// vitest.config.ts
import {defineConfig} from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    globalSetup: ["./test/global-setup.ts"],
    pool: "forks",
    fileParallelism: true,
  },
});
```

Then run the suite the way you run any other vitest suite:

```bash
npx vitest run       # the whole suite, files in parallel across worker processes
npx vitest           # watch mode
npx vitest run chat  # just the files whose path matches "chat"
```

Every worker shares the one Chrome that global setup started, so adding workers costs a daemon and a tab rather than a browser.  See the [setup section of the Vitest docs](https://onsi.github.io/biloba/vitest.html#getting-set-up) for the `global-setup.ts` and per-file `connect`/`openSession` boilerplate.

---

Ginkgo Tree Graphics Designed By 可行 From <a href="https://lovepik.com/image-401791345/ginkgo-branches-in-autumn.html">LovePik.com</a>
