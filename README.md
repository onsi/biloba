![Ginkgo](https://onsi.github.io/biloba/images/biloba.png)

[![test](https://github.com/onsi/biloba/workflows/test/badge.svg?branch=master)](https://github.com/onsi/biloba/actions?query=workflow%3Atest+branch%3Amaster) | [Biloba Docs](https://onsi.github.io/biloba/)

---

# Biloba

> "Automated browser testing is slow and flaky" - _every developer, ever_

Biloba builds on top of [chromedp](https://github.com/chromedp/chromedp) to bring stable, performant, automated browser testing to Ginkgo. It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

Take a look at the [documentation](https://onsi.github.io/biloba) to learn more and get started!

Here's a quick taste of what Biloba specs look like:

```go
func login(tab *Biloba, user string, password string) {
	GinkgoHelper()
	tab.Navigate("/login")
	Eventually("#user").Should(tab.SetValue("sally"))
	tab.SetValue("#password", "yllas")
	tab.Click("#log-in")
	Eventually(".chat-page").Should(tab.Exist())
}

Describe("a simple chat app", func() {
	var tab *Biloba
	BeforeEach(func() {
		login(b, "sally", "yllas")
		tab = b.NewTab()
		login(tab, "jane", "enaj")
	})

	It("shows all logged in users as present", func() {
		userXPath := b.XPath("div").WithID("user-list").Descendant()
		// b should show both users
		Eventually(userXPath.WithText("Sally")).Should(b.HaveClass("online"))
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
		// tab should show both users
		Eventually(userXPath.WithText("Sally")).Should(tab.HaveClass("online"))
		Eventually(userXPath.WithText("Jane")).Should(tab.HaveClass("online"))
	})

	It("shows Jane when Sally is typing", func() {
		lastEntryXPath := tab.XPath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		Eventually(lastEntryXPath).Should(SatisfyAll(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))

		b.SetValue("#input", "")
		Eventually(lastEntryXPath).ShouldNot(SatisfyAny(
			tab.HaveInnerText("Jane is typing..."),
			tab.HaveClass("typing"),
		))
	})

	It("shows Jane new messages from Sally, and sally new messages from Jane", func() {
		lastEntryXPath := tab.XPath("#conversation").Descendant().WithClass("entry").Last()
		b.SetValue("#input", "Hey Jane, how are you?")
		b.Click("#send")
		Eventually(lastEntryXPath).Should(tab.HaveInnerText("Hey Jane, how are you?"))

		tab.SetValue("#input", "I'm splendid, Sally!")
		tab.Click("#send")
		Eventually(lastEntryXPath).Should(b.HaveInnerText("I'm splendid, Sally!"))
	})

	It("tracks when users aren't online", func() {
		userXPath := b.XPath("div").WithID("user-list").Descendant()
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))

		tab.Close()
		Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("offline"))
	})
})
```

Run these in series with `ginkgo`.  And in parallel with `ginkgo -p` for fast, stable, browser tests.

Biloba is quite feature complete and in active development.  However, a 1.0 release milestone has not been reached yet, so the public API contract may shift as the project evolves.

---

Ginkgo Tree Graphics Designed By 可行 From <a href="https://lovepik.com/image-401791345/ginkgo-branches-in-autumn.html">LovePik.com</a>