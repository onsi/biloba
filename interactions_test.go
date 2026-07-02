package biloba_test

import (
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("First-class interactions", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/interactions.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("Focus", func() {
		It("focuses an element immediately", func() {
			b.Focus("#focusable")
			Expect("#focusable").To(b.BeFocused())
		})

		It("can be used as a matcher to poll until an element is focusable", func() {
			Eventually("#focusable").Should(b.Focus())
			Expect("#focusable").To(b.BeFocused())
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).Focus("#non-existing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().Focus("#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).Focus()
			ExpectFailures(ContainSubstring("Focus(...) returns a matcher"))
		})
	})

	Describe("Hover", func() {
		It("fires hover events so JavaScript hover handlers run", func() {
			Expect("#menu").NotTo(b.BeVisible())
			b.Hover("#hover-target")
			Eventually("#menu").Should(b.BeVisible())
		})

		It("can be used as a matcher", func() {
			Eventually("#hover-target").Should(b.Hover())
			Eventually("#menu").Should(b.BeVisible())
		})

		It("times out if the element never exists (poll-by-default)", func() {
			b.WithTimeout(time.Millisecond * 60).Hover("#non-existing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().Hover("#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("ScrollIntoView", func() {
		It("scrolls an off-screen element into view", func() {
			Expect("window.scrollY").To(b.EvaluateTo(0.0))
			b.ScrollIntoView("#footer")
			Eventually("window.scrollY").ShouldNot(b.EvaluateTo(0.0))
		})

		It("can be used as a matcher", func() {
			Eventually("#footer").Should(b.ScrollIntoView())
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).ScrollIntoView("#non-existing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().ScrollIntoView("#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).ScrollIntoView()
			ExpectFailures(ContainSubstring("ScrollIntoView(...) returns a matcher"))
		})
	})

	Describe("ClickWhen", func() {
		It("clicks exactly once while the guard matches, then waits for it to clear (no double-toggle)", func() {
			Expect("#toggle-async").To(b.HaveClass("collapsed"))
			b.ClickWhen("#toggle-async", "#toggle-async.collapsed")
			// on return the guard has cleared - the async class swap has happened...
			Expect("#toggle-async").NotTo(b.HaveClass("collapsed"))
			// ...and the click fired exactly once despite the 30ms settle window and polling
			Expect("window._asyncClicks").To(b.EvaluateTo(1.0))
			// give any errant re-click a chance to land, then re-confirm it stayed open and single-clicked
			Consistently("#toggle-async").ShouldNot(b.HaveClass("collapsed"))
			Expect("window._asyncClicks").To(b.EvaluateTo(1.0))
		})

		It("is a no-op when the guard never matches (already in the desired state)", func() {
			Expect("#toggle-open").NotTo(b.HaveClass("collapsed"))
			b.ClickWhen("#toggle-open", "#toggle-open.collapsed")
			Expect("window._openClicks").To(b.EvaluateTo(0.0))
		})

		It("clicks once under Immediate() when the guard clears synchronously", func() {
			Expect("#toggle-sync").To(b.HaveClass("collapsed"))
			b.Immediate().ClickWhen("#toggle-sync", "#toggle-sync.collapsed")
			Expect("#toggle-sync").NotTo(b.HaveClass("collapsed"))
		})

		It("times out if the guard never clears after the click", func() {
			// guard the click on a selector that stays present regardless of the click, so the guard
			// never clears and the poll must give up (having clicked exactly once).
			b.WithTimeout(time.Millisecond * 120).ClickWhen("#toggle-async", "#toggle-async")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("no longer match the guard selector"),
			))
			Expect("window._asyncClicks").To(b.EvaluateTo(1.0))
		})
	})

	Describe("SetUpload", func() {
		It("attaches a file to a file input and fires change", func() {
			path, err := filepath.Abs("./fixtures/upload-sample.txt")
			Expect(err).NotTo(HaveOccurred())

			b.SetUpload("#file", path)
			Eventually("#filenames").Should(b.HaveInnerText("upload-sample.txt"))
			Expect(b.GetProperty("#file", "files.length")).To(Equal(1.0))
		})

		It("attaches multiple files to a multi-file input", func() {
			a, _ := filepath.Abs("./fixtures/upload-sample.txt")
			c, _ := filepath.Abs("./fixtures/upload-other.txt")

			b.SetUpload("#files", a, c)
			Eventually("#multi-filenames").Should(b.HaveInnerText(ContainSubstring("upload-sample.txt")))
			Expect("#multi-filenames").To(b.HaveInnerText(ContainSubstring("upload-other.txt")))
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).SetUpload("#non-existing", "/tmp/whatever.txt")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("be uploadable to"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().SetUpload("#non-existing", "/tmp/whatever.txt")
			ExpectFailures(ContainSubstring("be uploadable to"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).SetUpload("/tmp/whatever.txt")
			ExpectFailures(ContainSubstring("SetUpload(...) returns a matcher"))
		})

		It("returns a matcher when under-applied, polling until the input is present", func() {
			path, err := filepath.Abs("./fixtures/upload-sample.txt")
			Expect(err).NotTo(HaveOccurred())

			Eventually("#file").Should(b.SetUpload(path))
			Eventually("#filenames").Should(b.HaveInnerText("upload-sample.txt"))
			Expect(b.GetProperty("#file", "files.length")).To(Equal(1.0))
		})

		It("attaches multiple files in the matcher form when given a []string", func() {
			a, _ := filepath.Abs("./fixtures/upload-sample.txt")
			c, _ := filepath.Abs("./fixtures/upload-other.txt")

			Eventually("#files").Should(b.SetUpload([]string{a, c}))
			Eventually("#multi-filenames").Should(b.HaveInnerText(ContainSubstring("upload-sample.txt")))
			Expect("#multi-filenames").To(b.HaveInnerText(ContainSubstring("upload-other.txt")))
		})
	})
})
