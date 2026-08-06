---
name: explore-unfamiliar-page
description: Orient to a page or app you haven't seen, then draft a starter Biloba spec. Use when writing browser tests against an unfamiliar URL or fixture — it drives the page once to dump its DOM outline, accessibility tree, and a screenshot so you can SEE it, then proposes a spec with sensible readiness anchors and interactions. Covers the orient-then-author loop and cleanup. Also invokable as /biloba:explore-unfamiliar-page <url-or-fixture>.
---

# Orienting to an unfamiliar page, then drafting a spec

Drive the page once to see it (DOM outline + a11y tree + screenshot), then write the spec against what you actually saw. Assumes Biloba is wired in (`biloba:setup`); the draft follows `biloba:write-tests`.

## 1. Drive the page once

Write a **throwaway** spec (`zz_scratch_test.go`) — you delete it in step 3.

```go
package <suite>_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("scratch", func() {
	It("dumps the page", func() {
		b.Navigate("<TARGET_URL>")
		// SPAs: gate on first meaningful content first —
		// Eventually("<a stable selector you expect>").Should(b.Exist())

		fmt.Println("=== DOM OUTLINE ===")
		fmt.Println(b.Outline())
		fmt.Println("=== A11Y OUTLINE ===")
		fmt.Println(b.A11yOutline())
		fmt.Println("SCREENSHOT:", b.CaptureScreenshotToFile("./tmp/scratch.png"))
	})
})
```

```
ginkgo --no-color --focus="scratch"
```

Then **`Read` the printed PNG path** so you actually see the rendered page (what's visible, what's below the fold), and cross-reference the two outlines:

- **`b.Outline()`** — raw DOM; your primary map for an app you own. Hunt for stable, intentional hooks (`#id`, `[data-testid]`) and target them with **CSS** — the default, fastest pathway. Avoid styling classes (`.btn-primary`) that get renamed in redesigns.
- **`b.A11yOutline()`** — role + accessible name. Use it when there's no good hook, or when you *want* to assert the user-perceivable thing so the spec doubles as an a11y guard: `b.ByRole("button").WithName("Save")`, `b.ByText(...)`, `b.ByLabel("Email")`, `b.ByTestID(...)`. XPath is the rare fallback for axis/ordinal structure (`biloba:xpath`).

You get the same outline for free on a failure under CI or an AI agent (`biloba:debug-failures`) — once you're iterating in step 2, read it from the failure report instead of re-running the scratch spec.

## 2. Author the real spec

```go
var _ = Describe("<feature>", func() {
	BeforeEach(func() {
		b.Navigate("<TARGET_URL>")
		Eventually("<readiness anchor>").Should(b.Exist())
	})

	It("<does the obvious thing>", func() {
		b.SetValue(b.ByLabel("Search"), "biloba")          // polls until settable, sets once
		b.Click(b.ByRole("button").WithName("Search"))     // polls until clickable, clicks once
		Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

A good draft:

- **Readiness anchor** that's stable and meaningful — a heading or key container present once the page is interactive.
- **CSS on stable hooks** by default; a **semantic locator** when the visible label is the natural identifier or you want the a11y guard. No `nth-child`/styling-class paths.
- **Assert observable outcomes** — visible text, counts, URL/title, network effects. Not implementation details.
- **On an unfamiliar page, distrust every green negative assertion.** A scope you guessed at matches *nothing* when it doesn't resolve, so `ShouldNot(b.Exist())` passes whether or not you got the selector right. Anchor the scope first — which also tells you immediately that you guessed the hook wrong:
  ```go
  Eventually("#list").Should(b.Exist())
  Eventually(b.ByText("Draft").Within("#list")).ShouldNot(b.Exist())
  ```
  (`biloba:flaky-specs` §7.)
- **Need a value the page generates (an id, a slug)?** Capture it off the assertion that proves it's there, don't assert then re-read: `Eventually(sel).Should(b.HaveAttribute("data-id", Not(BeEmpty())).Capture(&id))`.
- **Leave `// TODO` markers wherever you're guessing.**
- Stub a backend you don't want to depend on (`b.StubRequest(...)`) so the spec is fast and hermetic.
- If the flow is occlusion-, hover-, or drag-sensitive, the fast track may pass where a real user would be blocked. Add a focused smoke test on the realistic track (`b.Realistic()`, `Label("realistic")`) → `biloba:realistic-mode`.

## 3. Clean up

```
rm zz_scratch_test.go
rm -rf ./tmp/scratch.png
ginkgo -r -p
```

Report the new spec to the user and call out every `// TODO` and guess you left, so they know what to verify.
