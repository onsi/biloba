package biloba_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// b.Artifacts() is the structured twin of the paths Biloba already prints: the same files, as data,
// for a reporter that wants to upload them rather than print them.  The specs below pin the two
// properties a consumer depends on and cannot check for itself - that EVERY write site records
// (including the visual-regression ones, which happen mid-spec from inside a matcher), and that the
// list survives the failure cleanup so a ReportAfterEach can still read it.
var _ = Describe("Artifacts", func() {
	pathsOfKind := func(artifacts []biloba.Artifact, kind biloba.ArtifactKind) []string {
		paths := []string{}
		for _, artifact := range artifacts {
			if artifact.Kind == kind {
				paths = append(paths, artifact.Path)
			}
		}
		return paths
	}

	Describe("the list's lifecycle", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually("body").Should(b.Exist())
		})

		It("starts empty - Prepare() clears the previous spec's list", func() {
			Ω(b.Artifacts()).Should(BeEmpty())
		})

		It("records a screenshot written by CaptureScreenshotToFile", func() {
			path := filepath.Join(GinkgoT().TempDir(), "shot.png")
			written := b.CaptureScreenshotToFile(path)

			Ω(b.Artifacts()).Should(HaveExactElements(biloba.Artifact{
				Kind: biloba.ScreenshotArtifact,
				Path: written,
			}))
			Ω(written).Should(Equal(path))
		})

		It("accumulates in write order", func() {
			dir := GinkgoT().TempDir()
			b.CaptureScreenshotToFile(filepath.Join(dir, "first.png"))
			b.CaptureScreenshotToFile(filepath.Join(dir, "second.png"))

			Ω(pathsOfKind(b.Artifacts(), biloba.ScreenshotArtifact)).Should(HaveExactElements(
				filepath.Join(dir, "first.png"),
				filepath.Join(dir, "second.png"),
			))
		})

		It("is a snapshot: it rejects every poll-config knob", func() {
			b.WithTimeout(time.Second).Artifacts()
			ExpectFailures(ContainSubstring("Artifacts does not support WithTimeout"))

			b.WithPolling(time.Second).Artifacts()
			ExpectFailures(ContainSubstring("Artifacts does not support WithPolling"))

			b.WithContext(context.Background()).Artifacts()
			ExpectFailures(ContainSubstring("Artifacts does not support WithContext"))

			b.Immediate().Artifacts()
			ExpectFailures(ContainSubstring("Artifacts does not support Immediate"))
		})

		It("is cleared by Prepare, and only by Prepare", func() {
			b.CaptureScreenshotToFile(filepath.Join(GinkgoT().TempDir(), "shot.png"))
			Ω(b.Artifacts()).Should(HaveLen(1))

			// the read window a reporter uses is *after* the on-failure cleanup runs, so that
			// cleanup must leave the list alone
			b.RunFailureArtifactCleanupForTest()
			Ω(b.Artifacts()).Should(HaveLen(1))

			b.Prepare()
			Ω(b.Artifacts()).Should(BeEmpty())
		})

		It("hands back a copy, so a caller cannot corrupt the list", func() {
			b.CaptureScreenshotToFile(filepath.Join(GinkgoT().TempDir(), "shot.png"))
			artifacts := b.Artifacts()
			artifacts[0].Path = "clobbered"
			Ω(b.Artifacts()[0].Path).ShouldNot(Equal("clobbered"))
		})
	})

	Describe("the failure screenshots", func() {
		It("records each tab's screenshot, labelled with the tab title", func() {
			dir := GinkgoT().TempDir()
			bWithDir := biloba.ConnectToChrome(gt, biloba.BilobaConfigScreenshotsToDir(dir))
			bWithDir.Navigate(fixtureServer + "/screenshots.html")
			Eventually("body").Should(bWithDir.Exist())

			// a capture under heavy parallel load can exceed the internal per-tab bound; retry the
			// way screenshots_test.go does, then assert on what it recorded
			var shots []biloba.TabScreenshotForTest
			Eventually(func() string {
				shots = bWithDir.SafeAllTabScreenshotsForTest(0, 0)
				if len(shots) == 0 {
					return "no screenshots returned"
				}
				return shots[0].Failure
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(BeEmpty())

			Ω(bWithDir.Artifacts()).Should(ContainElement(biloba.Artifact{
				Kind:  biloba.ScreenshotArtifact,
				Path:  shots[0].FilePath,
				Label: shots[0].Title,
			}))
			// and it did not leak onto the shared root tab
			Ω(b.Artifacts()).Should(BeEmpty())
		})
	})

	// The visual artifacts are the half a consumer cannot recover from the filesystem: unlike the
	// failure screenshots, their filenames carry no spec component to scope a directory scan by.
	Describe("the visual-regression artifacts", func() {
		var baselinesDir, artifactsDir string
		var g Gomega

		BeforeEach(func() {
			root := GinkgoT().TempDir()
			baselinesDir, artifactsDir = filepath.Join(root, "baselines"), filepath.Join(root, "artifacts")
			DeferCleanup(b.SetVisualDirsForTest(baselinesDir, artifactsDir))
			g = NewGomega(func(message string, callerSkip ...int) {})

			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
		})

		It("records the baseline update mode writes", func() {
			defer b.SetUpdateScreenshotsForTest(true)()
			Eventually("#box").Should(b.HaveScreenshot("box"))

			Ω(b.Artifacts()).Should(HaveExactElements(biloba.Artifact{
				Kind:  biloba.VisualBaselineArtifact,
				Path:  filepath.Join(baselinesDir, "box.png"),
				Label: "box",
			}))
		})

		It("records the actual a missing baseline writes", func() {
			g.Eventually("#box").WithTimeout(time.Second).Should(b.HaveScreenshot("never-generated"))

			Ω(pathsOfKind(b.Artifacts(), biloba.VisualActualArtifact)).Should(ContainElement(
				filepath.Join(artifactsDir, "never-generated.actual.png"),
			))
		})

		It("records the actual and the diff a real mismatch writes", func() {
			func() {
				defer b.SetUpdateScreenshotsForTest(true)()
				Eventually("#box").Should(b.HaveScreenshot("box"))
			}()
			b.Run("document.getElementById('box').classList.add('spot')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))

			artifacts := b.Artifacts()
			Ω(pathsOfKind(artifacts, biloba.VisualActualArtifact)).Should(ContainElement(
				filepath.Join(artifactsDir, "box.actual.png"),
			))
			Ω(pathsOfKind(artifacts, biloba.VisualDiffArtifact)).Should(ContainElement(
				filepath.Join(artifactsDir, "box.diff.png"),
			))
			// the baseline this spec generated is in there too - the list is everything Biloba wrote
			Ω(pathsOfKind(artifacts, biloba.VisualBaselineArtifact)).Should(ContainElement(
				filepath.Join(baselinesDir, "box.png"),
			))
		})
	})
})
