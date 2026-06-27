package biloba

import (
	"fmt"
	"strings"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

/*
Tabs represents a slice of Biloba tabs

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
type Tabs []*Biloba

/*
Find returns the first Biloba tab matching the passed-in TabQuery (see [Biloba.TabMatching]), or nil if none match:

	tab := b.AllTabs().Find(b.TabMatching().WithTitle("Dashboard"))

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (t Tabs) Find(query *TabQuery) *Biloba {
	for _, tab := range t {
		if query.matches(tab) {
			return tab
		}
	}
	return nil
}

/*
Filter returns all tabs matching the passed-in TabQuery (see [Biloba.TabMatching])

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (t Tabs) Filter(query *TabQuery) Tabs {
	out := Tabs{}
	for _, tab := range t {
		if query.matches(tab) {
			out = append(out, tab)
		}
	}
	return out
}

/*
TabQuery is a chainable query over tabs.  A single value plays two roles:

  - a Gomega matcher you assert against a tab - read it as [Biloba.HaveSpawnedTab] (searches the tabs this tab spawned) or [Biloba.HaveTab] (searches every tab on the connection), and
  - a predicate you pass to [Tabs.Find] / [Tabs.Filter] - read it as [Biloba.TabMatching] (does this one tab match?).

A tab has no single primary key, so all of its dimensions are refinements: chain WithTitle, WithURL, and WithDOMElement.  Every refinement applies to the same tab.

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
type TabQuery struct {
	b             *Biloba
	scopeSpawned  bool
	titleMatcher  types.GomegaMatcher
	urlMatcher    types.GomegaMatcher
	hasDOMElement bool
	domSelector   any
	observed      Tabs
}

/*
TabMatching() returns a [TabQuery].  Refine it with WithTitle/WithURL/WithDOMElement.  Use this spelling when the query reads as a predicate - i.e. when handing it to [Tabs.Find] / [Tabs.Filter]:

	tab := b.AllSpawnedTabs().Find(b.TabMatching().WithURL(ContainSubstring("dom.html")))

When you're asserting against a tab, the [Biloba.HaveSpawnedTab] / [Biloba.HaveTab] aliases read more naturally.  They return the same query (HaveSpawnedTab additionally narrows the searched population to spawned tabs).

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) TabMatching() *TabQuery {
	return &TabQuery{b: b}
}

/*
HaveSpawnedTab() returns a [TabQuery] whose matcher searches the tabs spawned by this tab (tabs the browser opened in response to user/JavaScript interaction).  Refine it and poll to wait for a tab to open:

	b.Click("a[target='_blank']")
	Eventually(b).Should(b.HaveSpawnedTab().WithTitle("hello there"))

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) HaveSpawnedTab() *TabQuery {
	return &TabQuery{b: b, scopeSpawned: true}
}

/*
HaveTab() returns a [TabQuery] whose matcher searches every tab associated with this tab's Chrome connection - not just tabs this tab spawned.  You generally use this on the reusable root tab, but it is allowed on any tab; the result is the same.

	Eventually(b).Should(b.HaveTab().WithTitle("Dashboard"))

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) HaveTab() *TabQuery {
	return &TabQuery{b: b}
}

/*
WithTitle() refines the [TabQuery] to also require the tab's window title to match.  title may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (q *TabQuery) WithTitle(title any) *TabQuery {
	out := *q
	out.titleMatcher = matcherOrEqual(title)
	return &out
}

/*
WithURL() refines the [TabQuery] to also require the tab's URL ([Biloba.Location]) to match.  url may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (q *TabQuery) WithURL(url any) *TabQuery {
	out := *q
	out.urlMatcher = matcherOrEqual(url)
	return &out
}

/*
WithDOMElement() refines the [TabQuery] to also require the tab to have at least one element matching selector.

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (q *TabQuery) WithDOMElement(selector any) *TabQuery {
	out := *q
	out.hasDOMElement = true
	out.domSelector = selector
	return &out
}

// matches is the predicate role: does this single tab satisfy every constraint?
func (q *TabQuery) matches(tab *Biloba) bool {
	if q.titleMatcher != nil {
		if match, _ := q.titleMatcher.Match(tab.Title()); !match {
			return false
		}
	}
	if q.urlMatcher != nil {
		if match, _ := q.urlMatcher.Match(tab.Location()); !match {
			return false
		}
	}
	if q.hasDOMElement && !tab.HasElement(q.domSelector) {
		return false
	}
	return true
}

// Match is the Gomega matcher role: does the searched population have any tab that matches?
func (q *TabQuery) Match(actual any) (bool, error) {
	if _, ok := actual.(*Biloba); !ok {
		return false, fmt.Errorf("HaveTab/HaveSpawnedTab must be passed a Biloba tab.  Got:\n%s", format.Object(actual, 1))
	}
	if q.scopeSpawned {
		q.observed = q.b.AllSpawnedTabs()
	} else {
		q.observed = q.b.AllTabs()
	}
	return q.observed.Find(q) != nil, nil
}

func (q *TabQuery) scopeNoun() string {
	if q.scopeSpawned {
		return "spawned tab"
	}
	return "tab"
}

func (q *TabQuery) description() string {
	clauses := []string{}
	if q.titleMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("Title matching %s", q.titleMatcher.FailureMessage("")))
	}
	if q.urlMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("URL matching %s", q.urlMatcher.FailureMessage("")))
	}
	if q.hasDOMElement {
		clauses = append(clauses, fmt.Sprintf("a DOM element matching %v", q.domSelector))
	}
	if len(clauses) == 0 {
		return "have a " + q.scopeNoun()
	}
	return normalizeWhitespace("have a " + q.scopeNoun() + " with " + strings.Join(clauses, "\nand "))
}

func (q *TabQuery) presentTabs() string {
	if len(q.observed) == 0 {
		return "There were no tabs to search."
	}
	out := &strings.Builder{}
	out.WriteString("The tabs that were searched were:")
	for _, tab := range q.observed {
		fmt.Fprintf(out, "\n%s (%s)", tab.Title(), tab.Location())
	}
	return out.String()
}

func (q *TabQuery) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected to %s.\n%s", q.description(), q.presentTabs())
}

func (q *TabQuery) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected not to %s, but there was one.", q.description())
}

/*
AllSpawnedTabs() returns all tabs that were spawned by the current tab.  Spawned tabs are tabs that were created by the browser in response to some sort of user/javascript interaction.  They are not tabs that you create explicitly with NewTab()

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) AllSpawnedTabs() Tabs {
	b.guardConfig("AllSpawnedTabs")
	out := Tabs{}
	for _, tab := range b.AllTabs() {
		if b.isSiblingTab(tab) {
			out = append(out, tab)
		}
	}
	return out
}

func (b *Biloba) isSiblingTab(tab *Biloba) bool {
	return tab.browserContextID == b.browserContextID && tab != b
}
