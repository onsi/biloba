package biloba

import (
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Tabs represents a slice of Biloba tabs

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
type Tabs []*Biloba

/*
Find returns the first Biloba tab matching the TabFilter

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (t Tabs) Find(f TabFilter) *Biloba {
	for _, tab := range t {
		if f(tab) {
			return tab
		}
	}
	return nil
}

/*
Find returns the all tabs matching the TabFilter

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (t Tabs) Filter(f TabFilter) Tabs {
	out := Tabs{}
	for _, tab := range t {
		if f(tab) {
			out = append(out, tab)
		}
	}
	return out
}

/*
HaveSpawnedTab() is a matcher that succeeds if the passed-in TabFilter returns true for any of the spawned tabs associated with this tab.  You can use it to wait for a tab to open:

	tab.Click("a[target='_blank']")
	Eventually(tab).Should(tab.HaveSpawnedTab(tab.TabWithTile("hello there")))

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) HaveSpawnedTab(f TabFilter) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.AllSpawnedTabs().Find(f) != nil, nil
	}).WithTemplate("Did not find tab satisfying requirements.")
}

/*
HaveTab() is a matcher that succeeds if the passed-in TabFilter returns true for any of the tabs associated with this tab's Chrome Connection.  This will include all tabs - not just tabs spawned by this Tab.  You generally only use this on the reusable root tab - but you are allowed to use it on any tab; the result will be the same.

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) HaveTab(f TabFilter) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.AllTabs().Find(f) != nil, nil
	}).WithTemplate("Did not find tab satisfying requirements.")
}

/*
AllSpawnedTabs() returns all tabs that were spawned by the current tab.  Spawned tabs are tabs that were created by the browser in response to some sort of user/javascript interaction.  They are not tabs that you create explicitly with NewTab()

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) AllSpawnedTabs() Tabs {
	return b.AllTabs().Filter(b.isSiblingTab)
}

type TabFilter func(b *Biloba) bool

func (b *Biloba) isSiblingTab(tab *Biloba) bool {
	return tab.browserContextID == b.browserContextID && tab != b
}

/*
TabWithDOMElement() is a TabFilter that selects tabs that have at least one element matching selector

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) TabWithDOMElement(selector any) TabFilter {
	return func(tab *Biloba) bool {
		return tab.HasElement(selector)
	}
}

/*
TabWithURL() is a TabFilter that selects tabs with URLs matching url.  If url is a string, an exact match is required.  If url is a matcher, the matcher is used to test the tab's [Biloba.Location]

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) TabWithURL(url any) TabFilter {
	m := matcherOrEqual(url)
	return func(tab *Biloba) bool {
		match, _ := m.Match(tab.Location())
		return match
	}
}

/*
TabWithTitle() is a TabFilter that selects tabs with window titles matching title.  If title is a string, an exact match is required.  If title is a matcher, the matcher is used to test the tab's [Biloba.Title]

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) TabWithTitle(title any) TabFilter {
	m := matcherOrEqual(title)
	return func(tab *Biloba) bool {
		match, _ := m.Match(tab.Title())
		return match
	}
}
