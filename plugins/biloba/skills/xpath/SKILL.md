---
name: xpath
description: Build XPath selectors with Biloba's b.XPath() mini-DSL — tag/id/class/text/attribute predicates, boolean logic with b.XPredicate(), tree navigation (Child/Descendant/Parent/Ancestor/siblings), WithChildMatching + b.RelativeXPath, indexing (Nth/First/Last), and the XPath().WithText text predicates. Use when constructing or debugging an XPath selector for a Biloba action or matcher — the rare power tool after CSS and semantic locators. Covers the common pitfalls (XPredicate, RelativeXPath, no shadow/iframe crossing).
---

# The Biloba XPath DSL

XPath is the **rare power tool**, reached for *after* CSS and semantic locators (see `biloba:write-tests`): use it for axis/relationship/ordinal queries those can't express (an *ancestor*, a *following-sibling*, "the `ul` that has a child `li` saying X") or exact `text()`-node matching. It's native and fast but verbose, and — unlike CSS (`>>>`) and locators — it does **not** pierce shadow roots or iframes. Biloba's DSL builds the gnarly query string for you; any Biloba action/matcher takes the resulting `XPath` as its `selector`. Docs: <https://onsi.github.io/biloba/#the-xpath-dsl>.

`b.XPath()` returns a value of `type XPath string` — chainable, and printable for inspection: `fmt.Println(b.XPath("div").WithClass("c"))`.

## Starting a query

```go
b.XPath()                 // //*        — any element
b.XPath("div")            // //div      — by tag
b.XPath("//div[@id='x']") // verbatim   — anything starting with / or ./
```

## Predicates (refine the current node)

```go
b.XPath().WithID("submit")               // @id = 'submit'
b.XPath().WithClass("red").WithClass("lg") // both classes
b.XPath("button").WithText("Next")          // exact full text
b.XPath("li").WithTextStartsWith("Chapter")
b.XPath("q").WithTextContains("dream")
b.XPath("button").HasAttr("disabled")       // attribute present
b.XPath("input").WithAttr("type", "text")   // attribute equals
b.XPath("input").WithAttrStartsWith("name", "astro")
b.XPath().WithAttrContains("name", "bueller")
```

## Boolean logic — needs `b.XPredicate()`

The operands of `And`/`Or`/`Not` are **predicates**, built with `b.XPredicate()` (not `b.XPath()`). This is the #1 gotcha:

```go
// a button labelled "Add Comment" that is not disabled
b.XPath("button").WithText("Add Comment").Not(b.XPredicate().HasAttr("disabled"))

// a red Error div or an orange Warning div, but not a fire-drill
b.XPath("div").Or(
	b.XPredicate().And(b.XPredicate().WithClass("red"), b.XPredicate().WithText("Error")),
	b.XPredicate().And(b.XPredicate().WithClass("orange"), b.XPredicate().WithText("Warning")),
).Not(b.XPredicate().HasAttr("fire-drill"))
```

## Navigating the tree

```go
b.XPath("div").WithClass("comments").Child()       // any direct child
b.XPath("div").WithClass("comments").Child("p")    // direct <p> children
b.XPath("div").WithID("top").Descendant("li")      // any-depth <li>
b.XPath("div").WithClass("comments").Parent()
b.XPath("div").WithID("bottom").Ancestor("section").WithClass("outer")
b.XPath("li").WithClass("red").FollowingSibling("li").WithClass("blue")
b.XPath("li").WithClass("red").PrecedingSibling()
```

Each step can be further refined with the predicate methods above (`.Child("p").WithClass("highlight").WithText("User")`).

## Selecting by a child — needs `b.RelativeXPath()`

`WithChildMatching` takes a **relative** (`./`) predicate, built with `b.RelativeXPath()`:

```go
// the <ul> that has a child <li> with text "igloo"
b.XPath("ul").WithChildMatching(b.RelativeXPath("li").WithText("igloo"))
```

## Indexing

```go
b.XPath("ul").Nth(2)                          // the 2nd <ul> (1-based)
b.XPath("ul").Nth(2).Descendant("li").Last()  // its last <li>
someList.First()
```

## Prefer a locator for "the element that says X"

Don't reach for XPath's `text()` predicates as a first move — prefer the locator engine (see `biloba:write-tests`): `b.ByText("Submit")` / `b.ByTextContains("Welcome")` match *visible* text; `b.ByRole("button").WithName("Save")` and `b.ByLabel("Email")` cover role/label. Locators **compose** — `.ContainingText`/`.Containing`/`.And`/`.Or`/`.Within`/`.Nth` (all accepting any selector) — and **pierce open shadow roots** automatically, which XPath cannot. Use the XPath DSL's `WithText` only to scope an exact `text()` match to a specific tag: `b.XPath("button").WithText("Submit")`.

## Reuse partial queries

Because the DSL returns an `XPath` string, build a base once and extend it:

```go
users := b.XPath("div").WithID("user-list").Descendant()
Eventually(users.WithText("Sally")).Should(b.HaveClass("online"))
Eventually(users.WithText("Jane")).Should(b.HaveClass("online"))
```

## Limits

XPath selectors **do not cross** shadow DOM or iframe boundaries — the `>>>` piercing combinator is CSS-only (see `biloba:write-tests`). For those, use a CSS selector with `>>>`, or reach for a semantic locator (`b.ByRole`/`b.ByText`/`b.ByLabel`), which pierces open shadow roots automatically.
