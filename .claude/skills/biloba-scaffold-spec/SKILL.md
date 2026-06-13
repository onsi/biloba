---
name: biloba-scaffold-spec
description: Scaffold a starter Biloba spec for an unfamiliar URL or fixture. Use when you need to write browser tests against a page you haven't seen — it drives the page once to dump its DOM outline, accessibility outline, and a screenshot, then proposes a starter spec with sensible readiness anchors and interactions. Covers the orient-then-author loop and cleanup.
---

# Scaffolding a Biloba spec for a URL

The hardest part of *authoring* (vs. maintaining) a browser test is orienting to a DOM you've never seen. This skill turns "here's a URL" into "here's a draft spec" by first letting Biloba *show you the page* (text outline + a11y tree + screenshot), then writing a spec against what you actually saw.

Read the `biloba-testing` skill first for the suite harness (the shared `b`, the `fixtureServer`, `Prepare()`, `ExpectFailures`). This skill assumes you're working inside a repo that already has Biloba wired into its Ginkgo suite.

## Step 1 — drive the page once to see it

Write a **throwaway** spec (e.g. `zz_scaffold_test.go`) that navigates to the target and dumps everything you need to understand the page. Keep it disposable — you'll delete it in step 3.

```go
package <suite>_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("scaffold scratch", func() {
	It("dumps the page", func() {
		b.Navigate("<TARGET_URL>") // a fixtureServer URL or an external URL
		// give SPAs a beat to render their first meaningful content:
		// Eventually("<a stable selector you expect>").Should(b.Exist())

		fmt.Println("=== DOM OUTLINE ===")
		fmt.Println(b.Outline())
		fmt.Println("=== A11Y OUTLINE ===")
		fmt.Println(b.A11yOutline())
		path := b.CaptureScreenshotToFile("./tmp/scaffold.png")
		fmt.Println("SCREENSHOT:", path)
	})
})
```

Run it on its own and capture the output:

```
ginkgo --no-color --focus="scaffold scratch"
```

Then **`Read` the screenshot file** (the path is printed as `SCREENSHOT: ...`) so you *see* the rendered page — layout, what's visible, what's above/below the fold — and cross-reference it with the two text outlines:

- **`b.Outline()`** is the raw DOM: use it to find the actual selectors (ids, classes, tags, `data-*`) you'll target.
- **`b.A11yOutline()`** is the role/name view: use it to pick stable, human-meaningful anchors (a heading's text, a button's accessible name) and to drive `b.Text("…")` / XPath-by-text selectors.

> **You also get this for free on failure.** Biloba detects when it's running under an AI agent (or CI) and, on a failing spec, automatically attaches a DOM outline of every tab and writes screenshots to disk (`./biloba-screenshots`, or `BILOBA_SCREENSHOTS_DIR`). So once you're iterating in Step 2, a failing readiness anchor or assertion already hands you the outline — `Read` it (and the screenshot file) straight from the failure report instead of re-running the scratch spec to re-orient.

## Step 2 — author the real spec

Now write the actual spec against what you observed. A good starter follows the standard Biloba shape (see `biloba-testing`):

```go
var _ = Describe("<feature>", func() {
	BeforeEach(func() {
		b.Navigate("<TARGET_URL>")
		Eventually("<readiness anchor>").Should(b.Exist()) // gate on the page being ready
	})

	It("<does the obvious thing>", func() {
		// drive it with the selectors you found, e.g.:
		// b.SetValue("#search", "biloba")
		// b.Click(b.Text("Search"))
		// Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

Guidance for a *good* scaffold:

- **Pick a readiness anchor** that's stable and meaningful — prefer a heading or a key container that's present once the page is interactive. `Eventually(anchor).Should(b.Exist())` (or `.Should(b.BeVisible())`) in a `BeforeEach`.
- **Prefer text/role selectors for actions** the user would describe by label: `b.Click(b.Text("Submit"))` over a brittle `nth-child` CSS path. Fall back to ids/`data-*` from the DOM outline when there's no good visible label.
- **Assert on observable outcomes**, not implementation: visible text (`b.HaveInnerText`/`b.HaveText`), counts (`b.HaveCount`), URL/title (`b.HaveURL`/`b.HaveTitle`), or network effects (`b.HaveMadeRequest`).
- **Leave `// TODO` markers** where you're guessing — a scaffold is a starting point for the human, not a finished suite.
- If the page calls a backend you don't want to depend on, suggest stubbing it (`b.StubRequest(...)`) so the spec is fast and hermetic.

## Step 3 — clean up

Delete the throwaway scratch spec and the screenshot, then run the real spec to confirm it's green:

```
rm zz_scaffold_test.go
rm -rf ./tmp/scaffold.png
ginkgo -r -p
```

Report the new spec to the user and call out every `// TODO`/guess you left, so they know exactly what to verify.
