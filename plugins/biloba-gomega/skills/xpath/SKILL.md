---
name: xpath
description: Build XPath selectors with Biloba's b.XPath() mini-DSL — tag/id/class/text/attribute predicates, boolean logic with b.XPredicate(), tree navigation (Child/Descendant/Parent/Ancestor/siblings), WithChildMatching + b.RelativeXPath, indexing (Nth/First/Last), and the XPath().WithText text predicates. Use when constructing or debugging an XPath selector for a Biloba action or matcher — the rare power tool after CSS and semantic locators. Covers the common pitfalls (XPredicate, RelativeXPath, ancestor-or-self, no shadow/iframe crossing).
---

# The Biloba XPath DSL

XPath is the **rare power tool**, reached for after CSS and semantic locators (`write-tests`). Use it for axis/relationship/ordinal queries those can't express — an ancestor, a following-sibling, "the `ul` that has a child `li` saying X" — or exact `text()`-node matching. It is native and fast but verbose, and it does **not** pierce shadow roots or iframes (CSS `>>>` and locators do).

`b.XPath()` returns `type XPath string` — chainable, printable (`fmt.Println(b.XPath("div").WithClass("c"))`), and accepted as the `selector` by any Biloba action or matcher. Docs: <https://onsi.github.io/biloba/#the-xpath-dsl>.

## Starting a query

```go
b.XPath()                 // //*        — any element
b.XPath("div")            // //div      — by tag
b.XPath("//div[@id='x']") // verbatim   — anything starting with / or ./
```

## Predicates (refine the current node)

```go
b.XPath().WithID("submit")                  // @id = 'submit'
b.XPath().WithClass("red").WithClass("lg")  // both classes
b.XPath("button").WithText("Next")          // exact full text
b.XPath("li").WithTextStartsWith("Chapter")
b.XPath("q").WithTextContains("dream")
b.XPath("button").HasAttr("disabled")       // attribute present
b.XPath("input").WithAttr("type", "text")   // attribute equals
b.XPath("input").WithAttrStartsWith("name", "astro")
b.XPath().WithAttrContains("name", "bueller")
```

## Boolean logic — operands are `b.XPredicate()`, not `b.XPath()`

The #1 gotcha: `And`/`Or`/`Not` take **predicates**.

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
b.XPath("div").WithClass("comments").Child()        // any direct child
b.XPath("div").WithClass("comments").Child("p")     // direct <p> children
b.XPath("div").WithID("top").Descendant("li")       // any-depth <li>
b.XPath("div").WithClass("comments").Parent()
b.XPath("div").WithID("bottom").Ancestor("section").WithClass("outer")
b.XPath("li").WithClass("red").FollowingSibling("li").WithClass("blue")
b.XPath("li").WithClass("red").PrecedingSibling()
```

**`Ancestor` is `ancestor-or-self`** — a matching element counts as its own ancestor. Use `AncestorNotSelf(tag)` (plain `ancestor::`) when you need a strict ancestor. `DescendantNotSelf(tag)` is the matching `descendant::` form.

Every step can be refined further with the predicate methods: `.Child("p").WithClass("highlight").WithText("User")`.

## Selecting by a child — needs `b.RelativeXPath()`

`WithChildMatching` takes a **relative** (`./`) predicate:

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

## Reuse partial queries

The DSL returns a string, so build a base once and extend it:

```go
users := b.XPath("div").WithID("user-list").Descendant()
Eventually(users.WithText("Sally")).Should(b.HaveClass("online"))
Eventually(users.WithText("Jane")).Should(b.HaveClass("online"))
```

## Don't use XPath for "the element that says X"

Prefer the locator engine (`write-tests`): `b.ByText("Submit")` / `b.ByTextContains("Welcome")` match *visible* text; `b.ByRole("button").WithName("Save")` and `b.ByLabel("Email")` cover role and label. Locators compose (`.ContainingText`/`.Containing`/`.And`/`.Or`/`.Within`/`.Nth`, all accepting any selector) and **pierce open shadow roots** automatically, which XPath cannot. Use the DSL's `WithText` only to scope an exact `text()` match to a tag: `b.XPath("button").WithText("Submit")`.

## Limits

XPath crosses neither shadow DOM nor iframe boundaries — `>>>` is CSS-only. For those, use a CSS selector with `>>>`, or a semantic locator.
