package biloba_test

import (
	"path/filepath"

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

		It("fails if the element does not exist", func() {
			b.Focus("#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
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

		It("fails if the element does not exist", func() {
			b.Hover("#non-existing")
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

		It("fails if the element does not exist", func() {
			b.SetUpload("#non-existing", "/tmp/whatever.txt")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
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
