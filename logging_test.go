package biloba_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Console messages and page errors", func() {
	// consoleSummary is the page's own console calls; Chrome's messages (a non-empty Source) have specs
	// of their own.
	consoleSummary := func() []string {
		out := []string{}
		for _, message := range b.AllConsoleMessages() {
			if message.Source == "" {
				out = append(out, message.Type+": "+message.Text)
			}
		}
		return out
	}

	pageErrorMessages := func() []string {
		out := []string{}
		for _, pageError := range b.AllPageErrors() {
			out = append(out, pageError.Message)
		}
		return out
	}

	Describe("on a page that logs and throws while it loads", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/page-errors.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("records the page's console messages at every level", func() {
			Eventually(consoleSummary).Should(Equal([]string{"log: hello 3", "info: some info", "warning: careful", "error: bad thing"}))
			message := b.AllConsoleMessages()[0]
			Ω(message.Timestamp).Should(BeTemporally("~", time.Now(), time.Minute))
			Ω(message.Stack).ShouldNot(BeEmpty())
			Ω(message.Stack[0].URL).Should(HaveSuffix("/page-errors.html"))

			b.Run(`console.debug("a", 1, true, null, [1, 2], {x: 1})`)
			Eventually(consoleSummary).Should(ContainElement(`debug: a 1 true <nil> Array(2) Object`))
			Ω(gt.buffer).Should(gbytes.Say(`"hello" - 3`), "the console still streams to the GinkgoWriter")
		})

		It("records the messages Chrome generates itself, naming their source", func() {
			b.Run(`fetch("/status/404").then(r => r.text())`)
			Eventually(b.AllConsoleMessages).Should(ContainElement(SatisfyAll(
				HaveField("Source", "network"),
				HaveField("Type", "error"),
				HaveField("Text", ContainSubstring("404")),
			)))
			Ω(b.AllConsoleMessages()[0].Source).Should(BeEmpty(), "the page's own console calls have no source")
		})

		It("prints Chrome's warnings and errors to the GinkgoWriter, but leaves them out of the failure replay", func() {
			Eventually(consoleSummary).Should(ContainElement("error: bad thing"))
			b.Run(`fetch("/status/404").then(r => r.text())`)
			Eventually(gt.buffer).Should(gbytes.Say(`\[\d\d:\d\d\] .*\[network\] Failed to load resource: .*404.* \(http://.*/status/404\)`))
			Ω(b.SafeAllTabConsoleErrorsForTest()).Should(ContainElement(`"bad thing"`))
			Ω(b.SafeAllTabConsoleErrorsForTest()).ShouldNot(ContainElement(ContainSubstring("Failed to load resource")))
		})

		It("records uncaught exceptions and unhandled promise rejections", func() {
			Eventually(pageErrorMessages).Should(ConsistOf("Uncaught TypeError: load boom", "Uncaught (in promise) Error: rejected boom"))
			var thrown biloba.PageError
			for _, pageError := range b.AllPageErrors() {
				if strings.Contains(pageError.Message, "load boom") {
					thrown = pageError
				}
			}
			Ω(thrown.URL).Should(HaveSuffix("/page-errors.html"))
			Ω([]int{thrown.Line, thrown.Column}).Should(Equal([]int{23, 18}), "zero-based: the error is constructed on line 24, column 19 of the fixture")
			Ω(thrown.Timestamp).Should(BeTemporally("~", time.Now(), time.Minute))
			Ω(thrown.Stack).Should(HaveLen(2))
			Ω(thrown.Stack[0]).Should(HaveField("FunctionName", "explode"))
			Ω(thrown.Stack[0].Line).Should(Equal(thrown.Line))

			b.Run(`setTimeout(() => { throw "a string" })`)
			Eventually(pageErrorMessages).Should(ContainElement("Uncaught a string"))
		})

		It("streams page errors to the GinkgoWriter and replays them with the console errors when a spec fails, with one-based locations", func() {
			Eventually(gt.buffer).Should(gbytes.Say(`Uncaught TypeError: load boom \(http://.*/page-errors.html:24:19\)`))
			Ω(gt.buffer).Should(gbytes.Say(`explode .*http://.*/page-errors.html:24:19`), "the stack trace too")
			Ω(gt.buffer).Should(gbytes.Say(`http://.*/page-errors.html:27:13`))
			Eventually(b.SafeAllTabConsoleErrorsForTest).Should(ContainElements(
				`"bad thing"`,
				MatchRegexp(`^Uncaught TypeError: load boom \(http://.*/page-errors.html:24:19\)$`),
				HavePrefix("Uncaught (in promise) Error: rejected boom"),
			))
		})

		It("is reset by Prepare", func() {
			Eventually(b.AllPageErrors).Should(HaveLen(2))
			Eventually(b.AllConsoleMessages).ShouldNot(BeEmpty())
			b.Prepare()
			Ω(b.AllConsoleMessages()).Should(BeEmpty())
			Ω(b.AllPageErrors()).Should(BeEmpty())
		})

		It("keeps the most recent 1000 of each", func() {
			b.Run(`for (let i = 0; i < 1005; i++) { console.debug(i); setTimeout(() => { throw new Error("boom " + i) }) }`)
			Eventually(func() string { m := b.AllConsoleMessages(); return m[len(m)-1].Text }).Should(Equal("1004"))
			Ω(b.AllConsoleMessages()).Should(HaveLen(1000))
			Ω(b.AllConsoleMessages()[0].Text).Should(Equal("5"), "the fixture's messages and the first five are dropped")
			Eventually(func() string { e := b.AllPageErrors(); return e[len(e)-1].Message }).Should(Equal("Uncaught Error: boom 1004"))
			Ω(b.AllPageErrors()).Should(HaveLen(1000))
		})

		It("rejects every poll-config knob", func() {
			b.WithTimeout(time.Second).AllConsoleMessages()
			ExpectFailures(ContainSubstring("AllConsoleMessages does not support WithTimeout"))
			b.WithPolling(time.Second).AllPageErrors()
			ExpectFailures(ContainSubstring("AllPageErrors does not support WithPolling"))
			b.WithContext(context.Background()).AllPageErrors()
			ExpectFailures(ContainSubstring("AllPageErrors does not support WithContext"))
			b.Immediate().AllConsoleMessages()
			ExpectFailures(ContainSubstring("AllConsoleMessages does not support Immediate"))
			b.Immediate().AllRequests()
			ExpectFailures(ContainSubstring("AllRequests does not support Immediate"))
		})

	})

	It("builds an error report from the three snapshots", func() {
		b.AbortRequest(ContainSubstring("/api/aborted"))
		b.Navigate(fixtureServer + "/page-errors.html")
		Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/aborted")).Failed())
		Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/redirected")).WithStatus(200))
		Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/missing.json")).WithStatus(404))
		Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/status/500")).WithStatus(500))
		Eventually(b.AllPageErrors).Should(HaveLen(2))

		report := []string{}
		for _, pageError := range b.AllPageErrors() {
			report = append(report, "page error: "+pageError.Message)
		}
		for _, req := range b.AllRequests() {
			if req.Failure != "" {
				report = append(report, fmt.Sprintf("failed: %s %s", req.URL, req.Failure))
			} else if req.Status >= 400 {
				report = append(report, fmt.Sprintf("HTTP %d: %s", req.Status, req.URL))
			}
		}
		for _, message := range b.AllConsoleMessages() {
			// Chrome's own "Failed to load resource" messages would repeat the request outcomes above
			if message.Source == "" && (message.Type == "error" || message.Type == "warning") {
				report = append(report, message.Type+": "+message.Text)
			}
		}
		Ω(report).Should(ConsistOf(
			"page error: Uncaught TypeError: load boom",
			"page error: Uncaught (in promise) Error: rejected boom",
			MatchRegexp(`^failed: http://.*/api/aborted net::ERR_BLOCKED_BY_CLIENT`),
			MatchRegexp(`^HTTP 404: http://.*/missing.json$`),
			MatchRegexp(`^HTTP 500: http://.*/status/500$`),
			"warning: careful",
			"error: bad thing",
		))
	})
})

var _ = Describe("Logging", func() {
	It("redirects logs to the GinkgoWriter", func() {
		b.Run("console.log('hello log', 3, [1,2,3])")
		Ω(gt.buffer).Should(gbytes.Say("\"hello log\" - 3 - \\[1, 2, 3\\]"))
		b.Run("console.log(null)")
		Ω(gt.buffer).Should(gbytes.Say("null"))
		b.Run("console.log('hello log', 3, 'this is very very long', ['a', 3, 'b'], {dog: 'woof', cat: 'meow'}, 100, ['a', 'b', 'c', 'd', 'e'], 'still very very very long', 100, true, 'come on!')")
		Ω(gt.buffer).Should(gbytes.Say("hello log"))
		Ω(gt.buffer).Should(gbytes.Say("3\n"))
		Ω(gt.buffer).Should(gbytes.Say("\"this is very very long\"\n"))
		Ω(gt.buffer).Should(gbytes.Say("\\[a, 3, b\\]\n"))
		Ω(gt.buffer).Should(gbytes.Say("\\{dog: woof, cat: meow\\}\n"))
		b.Run("console.debug('hello debug')")
		Ω(gt.buffer).Should(gbytes.Say("hello debug"))
		b.Run("console.info('hello info')")
		Ω(gt.buffer).Should(gbytes.Say("hello info"))
		b.Run("console.error('hello error')")
		Ω(gt.buffer).Should(gbytes.Say("hello error"))
		b.Run("console.warn('hello warning')")
		Ω(gt.buffer).Should(gbytes.Say("hello warning"))
		b.Run("console.assert(false, 'hello assert')")
		Ω(gt.buffer).Should(gbytes.Say("hello assert"))

		ExpectFailures("Detected console.assert failure:\n\"hello assert\"\n")
	})

	It("captures console.error messages (not console.log) so they can be replayed at the top of the failure block", func() {
		b.Run("console.log('just a log')")
		b.Run("console.error('boom', 42)")
		Eventually(b.SafeAllTabConsoleErrorsForTest).Should(ConsistOf(SatisfyAll(
			ContainSubstring("boom"),
			ContainSubstring("42"),
		)))
	})
})
