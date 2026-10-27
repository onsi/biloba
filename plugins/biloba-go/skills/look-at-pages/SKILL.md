---
name: look-at-pages
description: Look at your own UI while you iterate on it, with Go/Ginkgo Biloba — a throwaway spec that screenshots a page across colour schemes (light/dark via b.SetMedia) and viewport sizes (phone via b.SetWindowSize + b.Mobile()/b.TouchScreen()), plus the text views an agent can read without an image (b.Outline, b.A11yOutline, GetBoundingBox, GetComputedStyle, AllConsoleMessages/AllPageErrors), the states worth capturing (focus, open menus, dialogs, CSS :hover via b.Realistic(), error states via StubResponse, long content), and turning what you saw into assertions (Encloses and the other layout matchers, HaveScreenshot with InColorSchemes). Use when asked to look at, review, or screenshot a page or design (light/dark, phone/desktop) rather than to test it. For drafting a first spec against a page you haven't seen, see explore-unfamiliar-page.
---

# Looking at your own pages

For seeing what a page looks like while you work on it, not for testing it. The harness is the throwaway spec from `explore-unfamiliar-page`, so you get the suite's Chrome, app server and seeding for free. That skill is for drafting a spec against a page you haven't seen; this one is for reviewing one you're building. Sibling skills are named here without a prefix; invoke one with the same plugin prefix you loaded this skill under.

## 1. The scratch spec: schemes × sizes

```go
package <suite>_test

import (
	"fmt"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("scratch", func() {
	It("looks at the page", func() {
		sizes := []struct {
			name          string
			width, height int
			opts          []biloba.DeviceOption
		}{
			{"phone", 390, 844, []biloba.DeviceOption{b.Mobile(), b.TouchScreen()}},
			{"desktop", 1280, 800, nil},
		}
		for _, scheme := range []string{"light", "dark"} {
			for _, size := range sizes {
				b.SetMedia(biloba.Media{ColorScheme: scheme})
				b.SetWindowSize(size.width, size.height, size.opts...)
				b.Navigate("<TARGET_URL>")
				Eventually("<readiness anchor>").Should(b.Exist())
				fmt.Println(b.CaptureScreenshotToFile(fmt.Sprintf("./tmp/look/%s-%s.png", scheme, size.name)))
			}
		}
	})
})
```

```
ginkgo --no-color --focus="scratch"
```

**`Read` each printed PNG.** Gate on a readiness anchor before every capture, or you are looking at a half-rendered page. Put `tmp/` in `.gitignore`.

## 2. Text views

When an image isn't enough, or you need numbers, print these next to the screenshot:

- `b.Outline()` — the DOM, pruned; `b.A11yOutline()` — roles and accessible names (does the button have a name?).
- `b.GetBoundingBox(sel)` — `Top/Left/Width/Height/Bottom/Right`. Checks "does it fit at 390px?" without eyeballing.
- `b.GetComputedStyle(sel, "background-color")` — the colour the dark scheme actually resolved to.
- `b.AllConsoleMessages()` / `b.AllPageErrors()` — a page that looks fine but logged an error.

## 3. States worth capturing

A page at rest is the easy case. Drive it into each state before capturing:

- **Focus:** `b.Focus(sel)`, to see the focus ring.
- **Open menus and popovers:** `b.Click(trigger)`, then wait for the menu.
- **CSS `:hover`:** `b.Realistic().Hover(sel)`. The fast `b.Hover` fires JS events only, so `:hover` styles never show → `realistic-mode`.
- **Dialogs:** the page's own modal. A native `confirm`/`alert` doesn't show in a screenshot.
- **Errors:** `b.StubRequest(url, biloba.StubResponse{Status: 500})` before `Navigate`, to see the error state.
- **Long content:** seed or `b.Run` in a long name or many rows, to see wrapping and overflow.

## 4. Keep it looking right

Once it looks right, turn what you checked by eye into specs in the real suite:

- Layout invariants → `Eventually(".card").Should(b.Encloses(".date-input"))`, `BeAbove`/`BeLeftOf`/…. Unlike a `scrollWidth <= innerWidth` check, `Encloses` doesn't go vacuous under `overflow-x: clip`.
- Colours → `Eventually("body").Should(b.HaveComputedStyle("background-color", b.MatchColor("#1b1513")))` after `SetMedia`.
- Appearance as a whole → `b.HaveScreenshot` with `b.InColorSchemes("light", "dark")` against a committed baseline → `visual-assertions`.

## 5. Clean up

```
rm zz_scratch_test.go
rm -rf ./tmp/look
```

Leave nothing behind in the suite: a scratch spec that stays becomes a slow spec that asserts nothing.
