package biloba_test

import (
	"path/filepath"
	"strings"
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
			b.WithTimeout(time.Millisecond*60).SetUpload("#non-existing", "/tmp/whatever.txt")
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

var _ = Describe("Occluded-click diagnosis", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/diagnostics.html")
		Eventually("#submit").Should(b.Exist())
		b.ResetPollDiagnosticsForTest()
	})

	It("still clicks through an overlay - Click is occlusion-blind by design", func() {
		Expect("#submit").NotTo(b.BeClickable())
		b.Click("#submit")
		Eventually("#submit-result").Should(b.HaveInnerText("clicked"))
	})

	It("records the occluder so the failure artifacts can name it", func() {
		b.Click("#submit")
		Expect(b.OccludedClicksNoteForTest()).To(SatisfyAll(
			ContainSubstring(`⚠ Click on "#submit" was dispatched while <div#overlay.modal-scrim> was the topmost`),
			ContainSubstring("element at its centre — the click may have been swallowed."),
			ContainSubstring(`Consider Eventually("#submit").Should(b.BeClickable()) or b.Realistic().Click("#submit").`),
		))
	})

	It("keeps only the most recent handful of occluded clicks", func() {
		for range 8 {
			b.Click("#submit")
		}
		Expect(strings.Count(b.OccludedClicksNoteForTest(), "⚠ Click on")).To(Equal(5))
	})

	It("does not cry wolf when the element's own child is the hit-test target", func() {
		b.Click("#labeled-btn")
		Eventually("#labeled-result").Should(b.HaveInnerText("clicked"))
		Expect(b.OccludedClicksNoteForTest()).To(BeEmpty())
	})

	It("says nothing about an ordinary, uncovered click", func() {
		b.Click("#label-span")
		Eventually("#labeled-result").Should(b.HaveInnerText("clicked"))
		Expect(b.OccludedClicksNoteForTest()).To(BeEmpty())
	})

	It("does not record anything for a click that never happened", func() {
		b.WithTimeout(time.Millisecond * 60).Click("#non-existing")
		ExpectFailures(ContainSubstring("Timed out after"))
		Expect(b.OccludedClicksNoteForTest()).To(BeEmpty())
	})
})
