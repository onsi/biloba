---
name: explore-unfamiliar-page
description: Orient to a page or app you haven't seen, then draft a starter Biloba spec. Use when writing browser tests against an unfamiliar URL or fixture — it drives the page once to dump its DOM outline, accessibility tree, and a screenshot so you can SEE it, then proposes a spec with sensible readiness anchors and interactions. Covers the orient-then-author loop and cleanup. Also invokable as /biloba:explore-unfamiliar-page <url-or-fixture>.
---

# Orienting to an unfamiliar page, then drafting a spec

The hardest part of *authoring* (vs. maintaining) a browser test is getting your bearings on a DOM you've never seen. This skill turns "here's a URL" into "here's a draft spec" by first letting Biloba **show you the page** (DOM outline + a11y tree + screenshot), then writing a spec against what you actually saw.

Assumes Biloba is already wired into the suite — if not, do `biloba:setup` first. For the authoring patterns this draft follows, see `biloba:write-tests`.

## Step 1 — drive the page once to see it

Write a **throwaway** spec (e.g. `zz_scratch_test.go`) that navigates to the target and dumps everything you need. Keep it disposable — you delete it in step 3.

```go
package <suite>_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("scratch", func() {
	It("dumps the page", func() {
		b.Navigate("<TARGET_URL>") // a local/fixture URL or an external one
		// give SPAs a beat to render first meaningful content:
		// Eventually("<a stable selector you expect>").Should(b.Exist())

		fmt.Println("=== DOM OUTLINE ===")
		fmt.Println(b.Outline())
		fmt.Println("=== A11Y OUTLINE ===")
		fmt.Println(b.A11yOutline())
		fmt.Println("SCREENSHOT:", b.CaptureScreenshotToFile("./tmp/scratch.png"))
	})
})
```

Run it on its own:

```
ginkgo --no-color --focus="scratch"
```

Then **`Read` the screenshot file** (the path printed as `SCREENSHOT: ...`) so you *see* the rendered page — what's visible, what's above/below the fold — and cross-reference the two text outlines:

- **`b.Outline()`** is the raw DOM, and your **primary** map for an app you own: hunt for stable, intentional hooks — an `#id` or a `[data-testid]` — and target them with **CSS** (the default, fastest pathway). Avoid styling classes (`.btn-primary`) that get renamed in redesigns.
- **`b.A11yOutline()`** is the role/name view: when there's no good hook, or when you *want* to assert the user-perceivable thing (so the spec doubles as an a11y guard), drive a **semantic locator** off it — `b.ByRole("button").WithName("Save")`, `b.ByText("…")`, `b.ByLabel("Email")`, `b.ByTestID("…")`. These read well and survive markup churn. XPath is the rare fallback for axis/ordinal structure neither expresses.

> **You get this for free on failure too.** Under an AI agent or CI, Biloba auto-attaches a DOM outline of every tab and writes screenshots to disk on a failing spec (see `biloba:debug-failures`). So once you're iterating in Step 2, a failing readiness anchor already hands you the outline — `Read` it from the failure report instead of re-running the scratch spec.

## Step 2 — author the real spec

Write the actual spec against what you observed, following the standard shape (`biloba:write-tests`):

```go
var _ = Describe("<feature>", func() {
	BeforeEach(func() {
		b.Navigate("<TARGET_URL>")
		Eventually("<readiness anchor>").Should(b.Exist()) // gate on the page being ready
	})

	It("<does the obvious thing>", func() {
		// b.SetValue(b.ByLabel("Search"), "biloba")
		// Eventually(b.ByRole("button").WithName("Search")).Should(b.Click())
		// Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

A *good* draft:

- **Readiness anchor** that's stable and meaningful — a heading or key container present once the page is interactive.
- **CSS targeting stable hooks** (`b.Click("#submit")`, `b.Click("[data-testid=submit]")`) as the default; reach for a **semantic locator** (`b.Click(b.ByRole("button").WithName("Submit"))`) to assert a11y or when the visible label is the natural identifier. Avoid brittle `nth-child`/styling-class paths; XPath only for axis/ordinal structure.
- **Assert observable outcomes**: visible text, counts, URL/title, network effects — not implementation details.
- **Leave `// TODO` markers** wherever you're guessing — a draft is a starting point for the human.
- If the page hits a backend you don't want to depend on, stub it (`b.StubRequest(...)`) so the spec is fast and hermetic.
- If a flow is **occlusion- or hover-sensitive** (a dropdown that opens on `:hover`, a click that must route around an overlay, a drag), the fast track may pass when a real user would be blocked. Add a focused smoke test on the **realistic track** (`b.Realistic()`, `Label("realistic")`) — see `biloba:realistic-mode`.

## Step 3 — clean up

Delete the scratch spec and screenshot, then run the real spec to confirm it's green:

```
rm zz_scratch_test.go
rm -rf ./tmp/scratch.png
ginkgo -r -p
```

Report the new spec to the user and call out every `// TODO`/guess you left, so they know exactly what to verify.
