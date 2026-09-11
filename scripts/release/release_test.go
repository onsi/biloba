package main

import (
	"os"
	"strings"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

func TestRelease(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Release Tool Suite")
}

var _ = ginkgo.Describe("the release tool", func() {
	Describe, It, Expect := ginkgo.Describe, ginkgo.It, gomega.Expect
	Equal, HaveOccurred, MatchError := gomega.Equal, gomega.HaveOccurred, gomega.MatchError

	const bilobaGo = "package biloba\n\n// comment mentioning BILOBA_VERSION\nconst BILOBA_VERSION = \"0.15.4\"\n\nfunc x() {}\n"

	// The released history below the Unreleased section - Prepare must never touch it.  It
	// includes a stray `## Features` heading inside a release, like 0.15.1's real one.
	const history = "## 0.15.4\n\n### Fixes\n\n- a fix\n\n## 0.15.1\n\n## Features\n\n- a feature\n\n### Fixes\n\n"

	Describe("versions", func() {
		It("reads BILOBA_VERSION", func() {
			Expect(CurrentVersion(bilobaGo)).To(Equal("0.15.4"))
		})

		It("refuses a biloba.go without exactly one BILOBA_VERSION line", func() {
			_, err := CurrentVersion("package biloba\n")
			Expect(err).To(HaveOccurred())
			_, err = CurrentVersion(bilobaGo + bilobaGo)
			Expect(err).To(HaveOccurred())
		})

		It("bumps patch and minor", func() {
			Expect(NextVersion("0.15.4", "patch")).To(Equal("0.15.5"))
			Expect(NextVersion("0.15.4", "minor")).To(Equal("0.16.0"))
			Expect(NextVersion("1.9.9", "minor")).To(Equal("1.10.0"))
		})

		It("refuses anything else", func() {
			_, err := NextVersion("0.15.4", "major")
			Expect(err).To(HaveOccurred())
			_, err = NextVersion("0.15.4-rc.1", "patch")
			Expect(err).To(HaveOccurred())
		})

		It("rewrites only the BILOBA_VERSION line", func() {
			rewritten, err := SetVersion(bilobaGo, "0.16.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(rewritten).To(Equal(strings.Replace(bilobaGo, `"0.15.4"`, `"0.16.0"`, 1)))
			Expect(CurrentVersion(rewritten)).To(Equal("0.16.0"))
		})
	})

	Describe("Prepare", func() {
		It("releases Unreleased as the new version under a fresh Unreleased, dropping empty subsections", func() {
			changelog := "## Unreleased\n\n### Features\n\n### Fixes\n\n- fixed a thing\n  across two lines\n\n" + history
			Expect(Prepare(changelog, "0.15.5")).To(Equal(
				"## Unreleased\n\n### Features\n\n### Fixes\n\n" +
					"## 0.15.5\n\n### Fixes\n\n- fixed a thing\n  across two lines\n\n" +
					history))
		})

		It("keeps every non-empty subsection and entries outside any subsection", func() {
			changelog := "## Unreleased\n\n- loose entry\n\n### Features\n\n- a feature\n\n### Fixes\n\n- a fix\n\n" + history
			Expect(Prepare(changelog, "0.16.0")).To(Equal(
				"## Unreleased\n\n### Features\n\n### Fixes\n\n" +
					"## 0.16.0\n\n- loose entry\n\n### Features\n\n- a feature\n\n### Fixes\n\n- a fix\n\n" +
					history))
		})

		It("treats a #### heading as content, not as an empty subsection", func() {
			changelog := "## Unreleased\n\n### Features\n\n#### Vitest\n\n### Fixes\n\n" + history
			Expect(Prepare(changelog, "0.16.0")).To(Equal(
				"## Unreleased\n\n### Features\n\n### Fixes\n\n" +
					"## 0.16.0\n\n### Features\n\n#### Vitest\n\n" +
					history))
		})

		It("leaves text above Unreleased byte-for-byte alone", func() {
			changelog := "# Changelog\n\nSome preamble.\n\n## Unreleased\n\n- entry\n\n" + history
			prepared, err := Prepare(changelog, "0.15.5")
			Expect(err).NotTo(HaveOccurred())
			Expect(prepared).To(Equal("# Changelog\n\nSome preamble.\n\n## Unreleased\n\n### Features\n\n### Fixes\n\n## 0.15.5\n\n- entry\n\n" + history))
		})

		It("keeps a blank line before the next release when the last entry had none", func() {
			changelog := "## Unreleased\n\n### Fixes\n\n- entry\n### Features\n" + history
			Expect(Prepare(changelog, "0.15.5")).To(Equal(
				"## Unreleased\n\n### Features\n\n### Fixes\n\n## 0.15.5\n\n### Fixes\n\n- entry\n\n" + history))
		})

		It("works when Unreleased is the only section", func() {
			Expect(Prepare("## Unreleased\n\n- entry\n", "0.1.0")).To(Equal("## Unreleased\n\n### Features\n\n### Fixes\n\n## 0.1.0\n\n- entry\n"))
		})

		It("stops when Unreleased holds only blank lines and ### headings", func() {
			for _, unreleased := range []string{
				"## Unreleased\n\n### Features\n\n### Fixes\n\n",
				"## Unreleased\n\n",
				"## Unreleased\n",
				"## Unreleased\n### Features\n   \n### Fixes\n",
			} {
				_, err := Prepare(unreleased+history, "0.15.5")
				Expect(err).To(MatchError(ErrEmptyUnreleased), unreleased)
			}
		})

		It("leaves the real CHANGELOG.md's released history byte-for-byte alone", func() {
			onDisk, err := os.ReadFile("../../CHANGELOG.md")
			Expect(err).NotTo(HaveOccurred())
			_, _, history, err := splitSection(string(onDisk), "Unreleased")
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(gomega.HavePrefix("## "))

			changelog := "## Unreleased\n\n### Features\n\n- a feature\n\n### Fixes\n\n" + history
			prepared, err := Prepare(changelog, "9.9.9")
			Expect(err).NotTo(HaveOccurred())
			Expect(prepared).To(Equal("## Unreleased\n\n### Features\n\n### Fixes\n\n## 9.9.9\n\n### Features\n\n- a feature\n\n" + history))
		})

		It("stops when there is no Unreleased section", func() {
			_, err := Prepare(history, "0.15.5")
			Expect(err).To(MatchError(gomega.ContainSubstring("no ## Unreleased section")))
		})

		It("stops when the version is already released", func() {
			_, err := Prepare("## Unreleased\n\n- entry\n\n"+history, "0.15.4")
			Expect(err).To(MatchError(gomega.ContainSubstring("already has a ## 0.15.4 section")))
		})
	})

	Describe("Notes", func() {
		It("returns the section body without its surrounding blank lines", func() {
			prepared, err := Prepare("## Unreleased\n\n### Fixes\n\n- a fix\n\n### Features\n\n"+history, "0.15.5")
			Expect(err).NotTo(HaveOccurred())
			Expect(Notes(prepared, "0.15.5")).To(Equal("### Fixes\n\n- a fix\n"))
			Expect(Notes(prepared, "0.15.4")).To(Equal("### Fixes\n\n- a fix\n"))
		})

		It("fails for a missing or empty section", func() {
			_, err := Notes(history, "0.9.9")
			Expect(err).To(HaveOccurred())
			_, err = Notes("## Unreleased\n\n## 0.1.0\n\n## 0.0.9\n\n- x\n", "0.1.0")
			Expect(err).To(HaveOccurred())
		})
	})
})
