package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage", func() {
	BeforeEach(func() {
		//web storage requires a navigated origin - about:blank has an opaque origin
		b.Navigate(fixtureServer + "/storage.html")
		Eventually("#title").Should(b.Exist())
		//localStorage persists across navigations within the same origin, so start clean
		b.LocalStorage().Clear()
		b.SessionStorage().Clear()
	})

	for _, which := range []string{"localStorage", "sessionStorage"} {
		which := which
		Describe(which, func() {
			storage := func() interface {
				Set(string, any)
				Get(string, ...any) any
				GetAll() map[string]any
				Remove(string)
				Clear()
				Length() int
			} {
				if which == "localStorage" {
					return b.LocalStorage()
				}
				return b.SessionStorage()
			}

			It("round-trips a string value", func() {
				storage().Set("user", "Joe")
				Ω(storage().Get("user")).Should(Equal("Joe"))

				var user string
				storage().Get("user", &user)
				Ω(user).Should(Equal("Joe"))
			})

			It("round-trips typed values", func() {
				storage().Set("count", 3)
				var count int
				storage().Get("count", &count)
				Ω(count).Should(Equal(3))

				storage().Set("flag", true)
				Ω(storage().Get("flag")).Should(Equal(true))

				storage().Set("user", map[string]any{"name": "Joe", "age": 42})
				var user struct {
					Name string
					Age  int
				}
				storage().Get("user", &user)
				Ω(user.Name).Should(Equal("Joe"))
				Ω(user.Age).Should(Equal(42))
			})

			It("returns nil for missing keys", func() {
				Ω(storage().Get("nope")).Should(BeNil())
			})

			It("supports Length, GetAll, Remove, and Clear", func() {
				storage().Set("a", 1)
				storage().Set("b", "two")
				Ω(storage().Length()).Should(Equal(2))
				Ω(storage().GetAll()).Should(SatisfyAll(
					HaveKeyWithValue("a", 1.0),
					HaveKeyWithValue("b", "two"),
				))

				storage().Remove("a")
				Ω(storage().Length()).Should(Equal(1))
				Ω(storage().Get("a")).Should(BeNil())

				storage().Clear()
				Ω(storage().Length()).Should(Equal(0))
			})

			It("reads raw (non-JSON) values written by the page", func() {
				b.Run("window." + which + `.setItem("raw", "plain-string-not-json")`)
				Ω(storage().Get("raw")).Should(Equal("plain-string-not-json"))
			})

			It("makes the value visible to the page", func() {
				storage().Set("seeded", "from-go")
				b.Run("window.refresh()")
				selector := "#local"
				if which == "sessionStorage" {
					selector = "#session"
				}
				//the page stored it as a JSON-encoded string, so it reads back quoted
				Ω(b.GetProperty(selector, "innerText")).Should(Equal(`"from-go"`))
			})
		})
	}

	Describe("isolation across tabs", func() {
		It("does not leak storage between isolated tabs", func() {
			b.LocalStorage().Set("user", "Joe")

			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/storage.html")
			Eventually("#title").Should(tab.Exist())
			Ω(tab.LocalStorage().Get("user")).Should(BeNil())
		})
	})
})
