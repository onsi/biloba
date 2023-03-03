package biloba

import (
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

func (b *Biloba) HaveSpawnedTab(f func(*Biloba) bool) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.FindSpawnedTab(f) != nil, nil
	}).WithTemplate("Did not find tab satisfying requirements.")
}

func (b *Biloba) FindSpawnedTab(f func(*Biloba) bool) *Biloba {
	for _, tab := range b.AllSpawnedTabs() {
		if f(tab) {
			return tab
		}
	}
	return nil
}

func (b *Biloba) AllSpawnedTabs() []*Biloba {
	tabs := []*Biloba{}
	for _, tab := range b.AllTabs() {
		if tab.browserContextID == b.browserContextID && tab != b {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

func (b *Biloba) HaveTab(f func(*Biloba) bool) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.FindTab(f) != nil, nil
	}).WithTemplate("Did not find tab satisfying requirements.")
}

func (b *Biloba) FindTab(f func(*Biloba) bool) *Biloba {
	for _, tab := range b.AllTabs() {
		if f(tab) {
			return tab
		}
	}
	return nil
}

func (b *Biloba) TabWithDOMNode(selector any) func(*Biloba) bool {
	return func(tab *Biloba) bool {
		return tab.HasElement(selector)
	}
}

func (b *Biloba) TabWithURL(url any) func(*Biloba) bool {
	m := matcherOrEqual(url)
	return func(tab *Biloba) bool {
		match, _ := m.Match(tab.Location())
		return match
	}
}

func (b *Biloba) TabWithTitle(title any) func(*Biloba) bool {
	m := matcherOrEqual(title)
	return func(tab *Biloba) bool {
		match, _ := m.Match(tab.Title())
		return match
	}
}
