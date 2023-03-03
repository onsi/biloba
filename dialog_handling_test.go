package biloba_test

import (
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("DialogHandling", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dialogs.html")
		Eventually("#status").Should(b.HaveInnerText("Green Alert!"))
	})

	Context("when no handler is registered and a dialog appears", func() {
		It("handles the dialog and tells the user", func() {
			b.Click(b.XPath("button").WithText("Red Alert"))
			Eventually(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: alert - Red Alert!"))
			Ω("#status").Should(b.HaveInnerText("Red Alert!"))
		})

		It("autohandles most dialogs with 'false'", func() {
			b.Click(b.XPath("button").WithText("Enter Warp?"))
			Eventually(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: confirm - Enter Warp?"))
			Ω("#status").Should(b.HaveInnerText("Green Alert!"))
		})

		It("authoandles beforeunload with 'true'", func() {
			b.Click(b.XPath("a").WithText("Evacuate"))
			Eventually(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: beforeunload"))
			Eventually(b.Location).Should(HaveSuffix("/dom.html"))
		})
	})

	Describe("Registering handlers", func() {
		BeforeEach(func() {
			gt.buffer.Clear()
		})
		Context("handling alerts", func() {
			It("can handle alerts", func() {
				b.HandleAlertDialogs().MatchingMessage(ContainSubstring("Red"))
				b.Click(b.XPath("button").WithText("Red Alert"))
				Eventually("#status").Should(b.HaveInnerText("Red Alert!"))
				Ω(gt.buffer.Contents()).Should(BeEmpty())

				b.Click(b.XPath("button").WithText("Yellow Alert"))
				Eventually("#status").Should(b.HaveInnerText("Yellow Alert!"))
				Ω(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: alert - Yellow Alert!"))
			})

			It("can handle all allerts silently", func() {
				b.HandleAlertDialogs()
				b.Click(b.XPath("button").WithText("Red Alert"))
				Eventually("#status").Should(b.HaveInnerText("Red Alert!"))
				Ω(gt.buffer.Contents()).Should(BeEmpty())
				b.Click(b.XPath("button").WithText("Yellow Alert"))
				Eventually("#status").Should(b.HaveInnerText("Yellow Alert!"))
				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})
		})

		Context("handling confirms", func() {
			It("autohandles as false", func() {
				b.HandleConfirmDialogs().MatchingMessage("nope").WithResponse(true)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Eventually(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: confirm - Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert!"))
			})

			It("can be configured to handle true", func() {
				b.HandleConfirmDialogs().MatchingMessage("Enter Warp?").WithResponse(true)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 0"))
				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})

			It("can be reconfigured", func() {
				b.HandleConfirmDialogs().MatchingMessage("Enter Warp?").WithResponse(true)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 0"))

				//later handler wins
				handler := b.HandleConfirmDialogs().MatchingMessage("Enter Warp?").WithResponse(false)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert!"))

				//we can remove the later handler and the earlier handler wins
				b.RemoveDialogHandler(handler)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 0"))

				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})
		})

		Context("handling prompts", func() {
			BeforeEach(func() {
				b.HandleConfirmDialogs().WithResponse(true)
				b.Click(b.XPath("button").WithText("Enter Warp?"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 0"))
			})

			It("autohandles as false", func() {
				b.Click(b.XPath("button").WithText("Set Speed"))
				Eventually(gt.buffer).Should(gbytes.Say("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: prompt - Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 0"))
			})

			It("can return accept the default value", func() {
				b.HandlePromptDialogs().MatchingMessage("Set Speed").WithResponse(true)
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 3"))

				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})

			It("can return a value", func() {
				b.HandlePromptDialogs().MatchingMessage("Set Speed").WithText("7")
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 7"))

				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})

			It("can return empty string as a value", func() {
				b.HandlePromptDialogs().MatchingMessage("Set Speed").WithText("")
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed:"))

				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})

			It("can be reconfigured", func() {
				b.HandlePromptDialogs().MatchingMessage("Set Speed").WithText("7")
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 7"))

				//later handler wins
				handler := b.HandlePromptDialogs().MatchingMessage("Set Speed").WithText("10")
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 10"))

				//we can remove the later handler and the earlier handler wins
				b.RemoveDialogHandler(handler)
				b.Click(b.XPath("button").WithText("Set Speed"))
				Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 7"))

			})
		})

		Context("handling onbeforeunload", func() {
			It("can be configured to handle false", func() {
				handler := b.HandleBeforeunloadDialogs().WithResponse(false)
				b.Click(b.XPath("a").WithText("Evacuate"))
				Consistently(b.Location).Should(HaveSuffix("/dialogs.html"))

				//so we can run navigate away after this test ends!
				b.RemoveDialogHandler(handler)
				Ω(gt.buffer.Contents()).Should(BeEmpty())
			})
		})

		It("works with multiple tabs", func() {
			tab := b.NewTab().Navigate(fixtureServer + "/dialogs.html")
			Eventually("#status").Should(tab.HaveInnerText("Green Alert!"))

			b.HandleConfirmDialogs().WithResponse(true)
			tab.HandleConfirmDialogs().WithResponse(true)

			b.Click(b.XPath("button").WithText("Enter Warp?"))
			tab.Click(b.XPath("button").WithText("Enter Warp?"))

			b.HandlePromptDialogs().WithText("10")
			tab.HandlePromptDialogs().WithText("11")

			b.Click(b.XPath("button").WithText("Set Speed"))
			tab.Click(b.XPath("button").WithText("Set Speed"))

			Ω("#status").Should(b.HaveInnerText("Green Alert! - Warp Speed: 10"))
			Ω("#status").Should(tab.HaveInnerText("Green Alert! - Warp Speed: 11"))
		})
	})

	Describe("looking for and making assertions on dialogs", func() {
		BeforeEach(func() {
			b.Click(b.XPath("button").WithText("Red Alert")) //unhandled
			b.HandleAlertDialogs()
			b.Click(b.XPath("button").WithText("Yellow Alert")) //handled
			b.Click(b.XPath("button").WithText("Enter Warp?"))  //unhandled
			b.HandleConfirmDialogs().WithResponse(true)
			b.Click(b.XPath("button").WithText("Enter Warp?")) //handled
			b.HandlePromptDialogs().WithText("10")
			b.Click(b.XPath("button").WithText("Set Speed")) //handled
			b.HandlePromptDialogs().WithResponse(true)
			b.Click(b.XPath("button").WithText("Set Speed")) //handled with default prompt
			b.HandlePromptDialogs().WithResponse(false)
			b.Click(b.XPath("button").WithText("Set Speed")) //handled with false response

			handler := b.HandleBeforeunloadDialogs().WithResponse(false)
			b.Click(b.XPath("a").WithText("Evacuate")) //handled
			b.RemoveDialogHandler(handler)
		})

		It("can return a list of all dialogs", func() {
			Ω(len(b.Dialogs())).Should(Or(Equal(8), Equal(9))) //there is a beforeunload that gets triggered navigating to this page.  but it won't be there if we happen to be the first spec!
		})

		It("returns nil if there are no most recent dialogs", func() {
			Ω(biloba.Dialogs{}.MostRecent()).Should(BeNil())
		})

		It("can return the most recent dialog", func() {
			Ω(b.Dialogs().MostRecent()).Should(HaveField("Type", biloba.DialogTypeBeforeunload))
			Ω(b.Dialogs().MostRecent()).Should(HaveField("Autohandled", false))
		})

		It("can filter by type", func() {
			alerts := b.Dialogs().OfType(biloba.DialogTypeAlert)
			Ω(alerts).Should(HaveLen(2))
			Ω(alerts[0]).Should(HaveField("Message", "Red Alert!"))
			Ω(alerts[0]).Should(HaveField("Autohandled", true))
			Ω(alerts[1]).Should(HaveField("Message", "Yellow Alert!"))
			Ω(alerts[1]).Should(HaveField("Autohandled", false))
		})

		It("can filter by message", func() {
			alerts := b.Dialogs().MatchingMessage(ContainSubstring("Alert"))
			Ω(alerts).Should(HaveLen(2))
			Ω(alerts[0]).Should(HaveField("Message", "Red Alert!"))
			Ω(alerts[0]).Should(HaveField("Autohandled", true))
			Ω(alerts[1]).Should(HaveField("Message", "Yellow Alert!"))
			Ω(alerts[1]).Should(HaveField("Autohandled", false))
		})

		It("records how confirm dialogs were handled", func() {
			warps := b.Dialogs().OfType(biloba.DialogTypeConfirm)
			Ω(warps).Should(HaveLen(2))
			Ω(warps[0].Autohandled).Should(BeTrue())
			Ω(warps[0].HandleResponse).Should(BeFalse())
			Ω(warps[1].Autohandled).Should(BeFalse())
			Ω(warps[1].HandleResponse).Should(BeTrue())
		})

		It("record how prompt dialogs were handled", func() {
			warps := b.Dialogs().OfType(biloba.DialogTypePrompt)
			Ω(warps).Should(HaveLen(3))
			Ω(warps[0].HandleResponse).Should(BeTrue())
			Ω(warps[1].HandleResponse).Should(BeTrue())
			Ω(warps[2].HandleResponse).Should(BeFalse())
			Ω(warps[0].HandleText).Should(Equal("10"))
			Ω(warps[1].HandleText).Should(Equal("3"))
			Ω(warps[2].HandleText).Should(Equal(""))
		})
	})
})
