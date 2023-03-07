package biloba_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Tabs", func() {
	Describe("creating new tabs", func() {
		Context("explicitly", func() {
			It("allows the user to create a new tab that is its own universe", func() {
				b.Navigate(fixtureServer + "/nav-a.html")
				Eventually(b.Title).Should(Equal("Nav-A Testpage"))

				tab := b.NewTab().Navigate(fixtureServer + "/dom.html")
				Eventually(tab.Title).Should(Equal("DOM Testpage"))
				Ω(b.Title()).Should(Equal("Nav-A Testpage"))

				By("it correctly wires up logging and directs interactions to the correct tab")
				Eventually("#increment").Should(tab.Click())
				tab.Click("#increment")
				tab.Click("#increment")
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("1"))
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("2"))
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("3"))
			})

			It("the new tab has a different BrowserContextID than the original tab", func() {
				tab := b.NewTab()
				Ω(b.BrowserContextID()).ShouldNot(Equal(tab.BrowserContextID()))
			})
		})

		Context("when a user-interaction opens a new tab (href target=_blank)", func() {
			It("can find the tab and latch onto it", func() {
				b.Navigate(fixtureServer + "/nav-a.html")
				Eventually(b.XPath().WithTextContains("Go to DOM (new tab)")).Should(b.Click())

				Eventually(b).Should(b.HaveSpawnedTab(b.TabWithURL(ContainSubstring("dom.html"))))
				tab := b.FindSpawnedTab(b.TabWithURL(ContainSubstring("dom.html")))
				Eventually("#increment").Should(tab.Click())
				tab.Click("#increment")
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("1"))
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("2"))
			})

			It("should have the same BrowserContextID as the spawning tab", func() {
				b.Navigate(fixtureServer + "/nav-a.html")
				Eventually(b.XPath().WithTextContains("Go to DOM (new tab)")).Should(b.Click())
				time.Sleep(time.Millisecond * 200)
				Eventually(b).Should(b.HaveSpawnedTab(b.TabWithURL(ContainSubstring("dom.html"))))
				tab := b.FindSpawnedTab(b.TabWithURL(ContainSubstring("dom.html")))

				Ω(b.BrowserContextID()).Should(Equal(tab.BrowserContextID()))
			})
		})

		Context("when javacript opens a new tab", func() {
			It("can find the tab and latch onto it", func() {
				b.Navigate(fixtureServer + "/auto-open.html")
				Eventually(b).Should(b.HaveSpawnedTab(b.TabWithURL(ContainSubstring("dom.html"))))
				tab := b.FindSpawnedTab(b.TabWithURL(ContainSubstring("dom.html")))
				Eventually("#increment").Should(tab.Click())
				tab.Click("#increment")
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("1"))
				Eventually(gt.buffer).Should(gbytes.Say("increment to"))
				Eventually(gt.buffer).Should(gbytes.Say("2"))
			})
		})

		It("wires up events correctly for new tabs", func() {
			b.Run("console.log('this is the main tab')")
			Eventually(gt.buffer).Should(gbytes.Say("this is the main tab"))

			g2 := b.NewTab()
			g2.Run("console.log('this is the new tab')")
			Eventually(gt.buffer).Should(gbytes.Say("this is the new tab"))
		})
	})

	Describe("looking for tabs", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			b.NewTab().Navigate(fixtureServer + "/dom.html")
			b.NewTab().Navigate(fixtureServer + "/xpath.html")
			Eventually(b.AllTabs).Should(HaveLen(3))
		})

		It("can return all tabs", func() {
			Ω(b.AllTabs()).Should(ConsistOf(
				HaveField("Title()", "Nav-A Testpage"),
				HaveField("Title()", "DOM Testpage"),
				HaveField("Title()", "XPath Testpage"),
			))
		})

		It("any tab, in fact, can return all tabs", func() {
			tab := b.FindTab(b.TabWithTitle("DOM Testpage"))
			Ω(tab.AllTabs()).Should(ConsistOf(
				HaveField("Title()", "Nav-A Testpage"),
				HaveField("Title()", "DOM Testpage"),
				HaveField("Title()", "XPath Testpage"),
			))
		})

		It("returns nil when the tab can't be found", func() {
			Ω(b.FindSpawnedTab(b.TabWithDOMElement("#non-existing"))).Should(BeNil())
			Ω(b.FindSpawnedTab(b.TabWithTitle("non-existing"))).Should(BeNil())
			Ω(b.FindSpawnedTab(b.TabWithURL("non-existing.html"))).Should(BeNil())
			Ω(b.FindTab(b.TabWithDOMElement("#non-existing"))).Should(BeNil())
			Ω(b.FindTab(b.TabWithTitle("non-existing"))).Should(BeNil())
			Ω(b.FindTab(b.TabWithURL("non-existing.html"))).Should(BeNil())
		})

		It("does not include non-spawned tabs in spawned tabs", func() {
			Ω(b.AllSpawnedTabs()).Should(BeEmpty())
			b.Click("#to-b-new")
			Eventually(b).Should(b.HaveSpawnedTab(b.TabWithTitle("Nav-B Testpage")))
		})

		It("groups spawned tabs appropriately", func() {
			tab := b.NewTab()
			Ω(tab.AllSpawnedTabs()).Should(BeEmpty())

			tab.Navigate(fixtureServer + "/auto-open.html")
			Eventually(tab).Should(tab.HaveSpawnedTab(tab.TabWithTitle("DOM Testpage")))
			Ω(tab.AllSpawnedTabs()).Should(HaveLen(1))
			spawnedTab := tab.FindSpawnedTab(tab.TabWithDOMElement("#hello"))
			Ω(spawnedTab.Title()).Should(Equal("DOM Testpage"))

			By("this is currently a bit weird, but spawned tabs consider everything in the browser context to be their spawned tabs")
			Ω(spawnedTab.AllSpawnedTabs()).Should(HaveLen(1))
			Ω(spawnedTab).Should(spawnedTab.HaveSpawnedTab(spawnedTab.TabWithTitle("AutoOpen Testpage")))
			Ω(spawnedTab.FindSpawnedTab(spawnedTab.TabWithTitle("AutoOpen Testpage"))).Should(Equal(tab))

			By("the root tab doens't have any of these spawned tabs")
			Ω(b.AllSpawnedTabs()).Should(BeEmpty())
			Ω(b).ShouldNot(b.HaveSpawnedTab(b.TabWithTitle("DOM Testpage")))
			Ω(b.FindSpawnedTab(b.TabWithDOMElement("#hello"))).Should(BeNil())

			By("and when we open a tab from the root tab - it dosn't get attached to the other tabs")
			b.Click("#to-b-new")
			Eventually(b).Should(b.HaveSpawnedTab(b.TabWithTitle("Nav-B Testpage")))
			Ω(tab).ShouldNot(tab.HaveSpawnedTab(b.TabWithTitle("Nav-B Testpage")))
			Ω(tab.FindSpawnedTab(tab.TabWithTitle("Nav-B Testpage"))).Should(BeNil())
		})

		It("can find tabs by title", func() {
			tab := b.FindTab(b.TabWithTitle("Nav-A Testpage"))
			Eventually("#to-b").Should(tab.Exist())

			tab = b.FindTab(b.TabWithTitle(ContainSubstring("DOM")))
			Eventually("#increment").Should(tab.Exist())
		})

		It("can find tabs by URL", func() {
			tab := b.FindTab(b.TabWithURL(HaveSuffix("xpath.html")))
			Eventually("#aquarium").Should(tab.Exist())

			tab = b.FindTab(b.TabWithURL(fixtureServer + "/dom.html"))
			Eventually("#increment").Should(tab.Exist())
		})

		It("can find tabs by DOM element", func() {
			tab := b.FindTab(b.TabWithDOMElement("#increment"))
			Ω(tab.Title()).Should(Equal("DOM Testpage"))

			tab = b.FindTab(b.TabWithDOMElement(b.XPath().WithID("aquarium")))
			Ω(tab.Title()).Should(Equal("XPath Testpage"))
		})

		It("can match by title, URL, and DOM element", func() {
			Ω(b).Should(b.HaveTab(b.TabWithTitle("Nav-A Testpage")))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithTitle("Nav-B Testpage")))

			Ω(b).Should(b.HaveTab(b.TabWithURL(fixtureServer + "/dom.html")))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithURL(HaveSuffix(".htm"))))

			Ω(b).Should(b.HaveTab(b.TabWithDOMElement(b.XPath().WithID("increment"))))
			Ω(b).Should(b.HaveTab(b.TabWithDOMElement("#aquarium")))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithDOMElement("#non-existing")))
		})
	})

	Describe("closing tabs", func() {
		It("closes non-root tabs", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			tab := b.NewTab().Navigate(fixtureServer + "/dom.html")

			Ω(b).Should(b.HaveTab(b.TabWithTitle("DOM Testpage")))
			Ω(b.AllTabs()).Should(HaveLen(2))
			Ω(tab.Close()).Should(Succeed())

			Eventually(b.AllTabs).Should(HaveLen(1))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithTitle("DOM Testpage")))
		})

		It("fails when attempting to close the root tab", func() {
			Ω(b.Close()).Should(MatchError("invalid attempt to close the root tab"))
		})
	})

	Describe("a tab flow with stuff going on", Ordered, func() {
		It("can spawn new tabs, etc.", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b.Title).Should(Equal("Nav-A Testpage"))
			Ω(b.AllTabs()).Should(ConsistOf(b))

			b.Click("#to-b-new")
			Eventually(b).Should(b.HaveSpawnedTab(b.TabWithTitle("Nav-B Testpage")))
			g2 := b.FindTab(b.TabWithTitle("Nav-B Testpage"))
			Ω(g2).Should(Equal(b.FindSpawnedTab(b.TabWithURL(ContainSubstring("/nav-b.html")))))
			Ω(g2).Should(Equal(b.FindSpawnedTab(b.TabWithDOMElement("#to-a"))))

			g3 := b.NewTab().Navigate(fixtureServer + "/dom.html")
			Eventually(g3.Title).Should(Equal("DOM Testpage"))

			Ω(b.Title()).Should(Equal("Nav-A Testpage"))
			Ω(g2.Title()).Should(Equal("Nav-B Testpage"))
			Ω(g3.Title()).Should(Equal("DOM Testpage"))
			Ω(b.AllTabs()).Should(ConsistOf(b, g2, g3))

			Ω(g2.Close()).Should(Succeed())
			Ω(g3.Close()).Should(Succeed())
			Eventually(b.AllTabs).Should(ConsistOf(b))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithTitle("DOM Testpage")))
			Ω(b).ShouldNot(b.HaveTab(b.TabWithTitle("Nav-B Testpage")))
			Ω(b).Should(b.HaveTab(b.TabWithTitle("Nav-A Testpage")))

			Ω(b.Close()).Should(MatchError("invalid attempt to close the root tab"))
		})

		It("cleans up tabs between tests", func() {
			Ω(b.AllTabs()).Should(ConsistOf(b))
		})
	})
})
