# Staged release notes for the graft branch

These describe features that exist only on this branch, so they must NOT ship in a release cut
from master.  Fold them into `CHANGELOG-TMP.md` when this branch merges, and delete this file.

Why this file exists rather than the usual `CHANGELOG-TMP.md`: that file is gitignored, so its
contents follow your working DIRECTORY and not your branch.  Notes staged there while working on
this branch are still sitting there when you check out master, and `shipit` would fold them into a
release that does not contain the features they describe.  (`shipit` also does not clear
`CHANGELOG-TMP.md` after a release, so anything left in it is folded again next time.)  Keeping
this branch's notes in a tracked file on this branch is what makes them travel with the code.

## Features

- `b.Artifacts()` returns the files Biloba wrote during the current spec - failure screenshots, the `.actual.png`/`.diff.png` of a failed `b.HaveScreenshot`, baselines written under `BILOBA_UPDATE_SCREENSHOTS`, and any `b.CaptureScreenshotToFile` - as `[]biloba.Artifact` (`Kind`, absolute `Path`, `Label`).  Biloba already announces each of these in the test output; this is the same information as data, for a reporter that wants to upload the files rather than print their paths.  The list is cleared by `b.Prepare()`.  Because the failure screenshots are written by a cleanup, read it from a `ReportAfterEach` rather than an `AfterEach`.

- `b.VisualComparisons()` returns what each `b.HaveScreenshot` comparison in the current spec measured, as `[]biloba.VisualComparison` - the failure message's prose diagnosis as data.  Each carries the baseline name and label, whether it matched, the baseline/actual/diff paths, both image sizes, the tolerances it ran under, `TotalPixels`/`DifferingPixels`/`Fraction`/`MaxChannelDelta`, and the shape verdicts (`Regions`, `RegionCount`, `Shifted`/`Shift`, `Scattered`).  A comparison that found no baseline at all is recorded too, flagged with `MissingBaseline` - a distinct verdict from a mismatch, since the response is to generate baselines rather than to investigate a regression.  For a reporter that wants to record a visual regression rather than print the sentences, or a spec that wants to assert on *how* an image changed.  The measurements are definitions; the shape verdicts are tuned heuristics whose thresholds move between releases.  Only the attempt that decided an assertion is recorded - one entry per scheme `b.InColorSchemes` actually measured - passing ones included, and the list is cleared by `b.Prepare()`.

- `b.ViewportOnly()` returns a lightweight view of the tab whose *page* captures contain only what is currently on screen, instead of the whole document: `b.ViewportOnly().CaptureScreenshotToFile("fold.png")`, `Expect(b.ViewportOnly()).To(b.HaveScreenshot("fold"))`.  Like `b.Realistic()` it is a shallow clone-with-a-flag, and the capture is taken at the current scroll position.  Biloba's default - the whole document - is right for a failure screenshot and is not what the user can actually see.  Combining it with an *element* capture is a hard error rather than a silent no-op, since an element capture is already clipped to its box.

- `biloba.InterceptedResponse` now carries the request `URL` alongside `Status`, `Headers`, and `Body`.  A `b.ModifyResponse(...).Using(...)` transform registered with a matcher can serve several URLs, and a `b.HoldResponse(...)` registered with a matcher can be holding several at once - `Await()` had no way to say which one it handed you.

- `SpinUpChrome` takes a new `biloba.SkipConfigFile()` option that suppresses the `./.biloba-config-<process>` handshake file.  That file exists so `ConnectToChrome` can find the browser when you don't plumb the `ChromeConnection` yourself; if you do plumb it, nothing reads the file and it's just a stray dotfile in your working directory.

- A network handler whose URL matcher returns an *error* no longer fails silently.  All three dispatch sites dropped the error on the floor, so a matcher that could not decide - `BeNumerically(">", 3)` handed a URL string, say - was indistinguishable from one that honestly did not match: the request went to the real network and the spec failed somewhere else entirely.  Biloba still treats it as a non-match (it cannot intercept a request it cannot decide about) but now prints the registration site and the error to the test output the first time each handler errors, and attaches the same note to a failing spec.

- A response-stage URL matcher is now consulted exactly once per request.  Deciding whether to intercept the response at all walked the handler list, and dispatching the response walked it again - two evaluations of the same matcher, which is not merely wasteful: nothing says a URL matcher must be pure, and a stateful one ("match only the first request to this URL") got a different answer to each walk.  The request stage now resolves the handler once and the response stage cashes that in; the provenance bookkeeping behind `Count()` and the shadowed-handler note still happens at the response stage, so a request that never comes back still counts for nothing.

- `b.Mask`'s documentation now states the consequence of masking the capture rather than the baseline: *adding* a mask to an assertion that already has a baseline makes it fail until that baseline is regenerated under `BILOBA_UPDATE_SCREENSHOTS`, because the stored image still has the unmasked region in it.  Behaviour is unchanged - writing the mask into the baseline is the right design - but the old wording ("both sides are always masked identically", immediately followed by "a selector that matches nothing is a no-op") invited the reading that masks are safe to add.

- `b.Type` now accepts a `Locator` as its selector.  `b.Type(b.ByLabel("Email"), "jane@example.com")` used to fall through to the matcher form and fail with `Type received an invalid key of type biloba.Locator`; every other selector-taking method already took one.

- A failure raised in the browser now names a `Locator` the way `Locator.String()` does rather than dumping its wire encoding.  `b.Immediate().Click(b.ByLabel("Nope"))` used to report `could not find DOM element matching selector: {"by":"label","value":"Nope","valueMode":"exact"}`; it now reports `...selector: label="Nope"`.  This covers every browser-side message that annotates itself with the selector - missing elements, guard failures (`DOM element is not enabled: ...`), and the realistic interaction path.  CSS and XPath selectors are unchanged.

- A failed assertion against a `Locator` now prints the Locator's human-readable rendering (`role=button name="Nope"`) instead of a dump of its unexported fields.  Gomega consults `GomegaStringer`, not `Stringer`, and `Locator` only had the latter.
