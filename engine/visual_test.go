package engine_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("runner-neutral screenshots", func() {
	It("captures page and element pixels and bounds the returned binary", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		pageShot, err := session.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(bytes.HasPrefix(pageShot.PNG, []byte("\x89PNG\r\n\x1a\n"))).To(BeTrue())
		Expect(pageShot.Width).To(BeNumerically(">", 100))
		Expect(pageShot.Height).To(BeNumerically(">", 100))

		elementShot, err := session.CaptureElementScreenshot(ctx, engine.TestID("drag-source"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(elementShot.Width).To(BeNumerically("~", 40, 1))
		Expect(elementShot.Height).To(BeNumerically("~", 40, 1))
		Expect(pixelAt(elementShot.PNG, 10, 10)).To(Equal(color.NRGBA{R: 255, A: 255}))

		_, err = session.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{MaxBytes: 16})
		Expect(err).To(MatchError(ContainSubstring("exceeds the 16-byte limit")))
	})

	It("masks observable pixels and restores color-scheme emulation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:40px;height:40px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box><i style="display:block;width:10px;height:10px;background:red"></i></div>`)).To(Succeed())
		before, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())

		shot, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{
			ColorScheme: "dark",
			Masks:       []engine.Selector{engine.CSS("i")},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(pixelAt(shot.PNG, 5, 5)).To(Equal(color.NRGBA{R: 128, G: 128, B: 128, A: 255}))
		Expect(pixelAt(shot.PNG, 20, 20)).To(Equal(color.NRGBA{A: 255}))
		after, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})

	It("freezes rendering by default and permits animated capture only when requested", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:20px;height:20px;background:red;animation:shade 100000s linear infinite;animation-delay:-50000s}@keyframes shade{from{background:red}to{background:blue}}</style><div class=box></div>`)).To(Succeed())

		frozen, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		animated, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{Animated: true})
		Expect(err).NotTo(HaveOccurred())

		Expect(pixelAt(frozen.PNG, 10, 10)).To(Equal(color.NRGBA{R: 255, A: 255}))
		Expect(pixelAt(animated.PNG, 10, 10)).NotTo(Equal(pixelAt(frozen.PNG, 10, 10)))
	})

	It("warns for a raw fully clipped element capture but refuses it as a visual baseline", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<div style='width:50px;height:50px;overflow:hidden'><div class=box style='margin-top:100px;width:20px;height:20px;background:red'></div></div>")).To(Succeed())

		shot, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(shot.PNG).NotTo(BeEmpty())
		Expect(shot.Warning).To(ContainSubstring("not painted because it is clipped"))

		_, err = session.CompareScreenshot(ctx, "clipped", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: GinkgoT().TempDir(), ArtifactDir: GinkgoT().TempDir(), Update: true,
			SettleAttempts: 2, SettleStreak: 2, SettleInterval: time.Millisecond,
		})
		Expect(err).To(MatchError(ContainSubstring("refusing to write")))
	})

	It("reports when viewport expansion removes the element being captured", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.SetWindowSize(ctx, 600, 400)).To(Succeed())
		html := `<body style="margin:0;height:1500px"><div id="doomed" style="position:absolute;top:1200px;width:150px;height:80px;background:red"></div><script>let q=matchMedia('(max-width:100px)');let remove=()=>document.getElementById('doomed')?.remove();q.addEventListener('change',remove)</script>`
		Expect(session.Navigate(ctx, "data:text/html,"+url.PathEscape(html))).To(Succeed())

		shot, err := session.CaptureElementScreenshot(ctx, engine.CSS("#doomed"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(shot.PNG).NotTo(BeEmpty())
		Expect(shot.Warning).To(ContainSubstring("present before this capture and gone after it"))
	})
})

var _ = Describe("runner-neutral visual comparison", func() {
	It("validates baseline names without allowing escape from the baseline directory", func() {
		path, err := engine.ScreenshotBaselinePath("checkout/receipt", "dark")
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(Equal(filepath.Join("checkout", "receipt-dark.png")))

		for _, name := range []string{"", "../receipt", "checkout/../receipt", "/tmp/receipt"} {
			_, err := engine.ScreenshotBaselinePath(name, "")
			Expect(err).To(HaveOccurred(), name)
		}
	})

	It("fails on a missing baseline while preserving the captured actual", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0;background:rgb(2,4,8)}</style>missing")).To(Succeed())
		root := GinkgoT().TempDir()

		result, err := session.CompareScreenshot(ctx, "pages/missing", engine.PageScreenshotTarget(), engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"),
			ArtifactDir: filepath.Join(root, "artifacts"),
		})

		Expect(engine.IsFatal(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("no screenshot baseline")))
		Expect(result.Schemes).To(HaveLen(1))
		Expect(result.Schemes[0].ActualPath).To(BeAnExistingFile())
		Expect(result.Schemes[0].DiffPath).To(BeEmpty())
		Expect(filepath.Base(result.Schemes[0].ActualPath)).To(Equal("pages_missing.actual.png"))
	})

	It("settles before update mode writes, then compares within pixel and channel tolerances", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px;background:red}</style><div class=box></div><script>setTimeout(()=>document.querySelector('.box').style.background='rgb(10,20,30)',5)</script>")).To(Succeed())
		root := GinkgoT().TempDir()
		opts := engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"), ArtifactDir: filepath.Join(root, "artifacts"),
			Update: true, SettleAttempts: 4, SettleStreak: 2, SettleInterval: 10 * time.Millisecond,
		}
		updated, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Match).To(BeTrue())
		Expect(updated.Updated).To(BeTrue())
		Expect(updated.Schemes[0].BaselinePath).To(BeAnExistingFile())
		baseline, err := os.ReadFile(updated.Schemes[0].BaselinePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(pixelAt(baseline, 5, 5)).To(Equal(color.NRGBA{R: 10, G: 20, B: 30, A: 255}))

		_, err = session.Evaluate(ctx, `document.querySelector('.box').style.background='rgb(14,20,30)'`)
		Expect(err).NotTo(HaveOccurred())
		opts.Update = false
		opts.Tolerance = engine.ScreenshotTolerance{ChannelDelta: 4}
		matched, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched.Match).To(BeTrue())

		opts.Tolerance = engine.ScreenshotTolerance{ChannelDelta: 3, PixelFraction: 0.01}
		different, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(different.Match).To(BeFalse())
		Expect(different.Schemes[0].Diff.DifferingPixels).To(Equal(100))
		Expect(different.Schemes[0].ActualPath).To(BeAnExistingFile())
		Expect(different.Schemes[0].DiffPath).To(BeAnExistingFile())
		Expect(different.Schemes[0].Diagnosis).To(And(
			ContainSubstring("100 of 100 pixels differ"),
			ContainSubstring("actual:"),
			ContainSubstring("diff:"),
		))
	})

	It("captures every color scheme into a distinct baseline and restores the original scheme", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:10px;height:10px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box></div>`)).To(Succeed())
		before, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		root := GinkgoT().TempDir()

		result, err := session.CompareScreenshot(ctx, "theme", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"), ArtifactDir: filepath.Join(root, "artifacts"), Update: true,
			ColorSchemes: []string{"light", "dark"}, SettleAttempts: 3, SettleStreak: 2, SettleInterval: time.Millisecond,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Schemes).To(HaveLen(2))
		light, err := os.ReadFile(filepath.Join(root, "baselines", "theme-light.png"))
		Expect(err).NotTo(HaveOccurred())
		dark, err := os.ReadFile(filepath.Join(root, "baselines", "theme-dark.png"))
		Expect(err).NotTo(HaveOccurred())
		Expect(bytes.Equal(light, dark)).To(BeFalse())
		after, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})

	It("restores the exact media state that preceded a color capture", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>.box{width:10px;height:10px}</style><div class=box></div>")).To(Succeed())
		Expect(session.SetMedia(ctx, engine.Media{Type: "screen", ColorScheme: "dark", ReducedMotion: "reduce"})).To(Succeed())

		_, err = session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{
			Animated: true, ColorScheme: "light",
		})
		Expect(err).NotTo(HaveOccurred())
		state, err := session.Evaluate(ctx, `[
			matchMedia("screen").matches,
			matchMedia("(prefers-color-scheme: dark)").matches,
			matchMedia("(prefers-reduced-motion: reduce)").matches
		]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal([]any{true, true, true}))
	})

	It("rejects invalid update tolerance before settling or overwriting a baseline", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>.box{width:10px;height:10px;background:red}</style><div class=box></div>")).To(Succeed())
		baselineDir := GinkgoT().TempDir()
		baselinePath := filepath.Join(baselineDir, "invalid-tolerance.png")
		original := solidPNG(1, 1, color.NRGBA{R: 1, A: 255})
		Expect(os.WriteFile(baselinePath, original, 0o644)).To(Succeed())
		start := time.Now()

		_, err = session.CompareScreenshot(ctx, "invalid-tolerance", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir:    baselineDir,
			Update:         true,
			Tolerance:      engine.ScreenshotTolerance{PixelFraction: math.NaN()},
			SettleAttempts: 3,
			SettleStreak:   3,
			SettleInterval: time.Second,
		})

		Expect(engine.IsFatal(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("pixel tolerance")))
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
		contents, readErr := os.ReadFile(baselinePath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(contents).To(Equal(original))
	})

	It("uses a 16 MiB inclusive default screenshot boundary", func() {
		Expect(engine.DefaultMaxScreenshotBytes).To(Equal(16 << 20))
		pngData := solidPNG(1, 1, color.NRGBA{A: 255})
		exact := append(pngData, make([]byte, engine.DefaultMaxScreenshotBytes-len(pngData))...)
		Expect(engine.WriteScreenshotPNG(filepath.Join(GinkgoT().TempDir(), "exact.png"), exact, 0)).To(Succeed())
		tooLarge := append(exact, 0)
		Expect(engine.WriteScreenshotPNG(filepath.Join(GinkgoT().TempDir(), "too-large.png"), tooLarge, 0)).To(MatchError(ContainSubstring("exceeds the 16777216-byte limit")))
	})

	It("warns when a color-scheme matrix captures the same rendering twice", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px;background:red}</style><div class=box></div>")).To(Succeed())

		result, err := session.CompareScreenshot(ctx, "pinned-theme", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: GinkgoT().TempDir(), ArtifactDir: GinkgoT().TempDir(), Update: true,
			ColorSchemes: []string{"light", "dark"}, SettleAttempts: 2, SettleStreak: 2, SettleInterval: time.Millisecond,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Warnings).To(ContainElement(ContainSubstring("byte-identical")))
	})

	It("reports when update mode writes the last frame of a screenshot that never settled", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px}</style><div class=box></div>")).To(Succeed())
		_, err = session.Exists(ctx, engine.CSS(".box"))
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `window.captureCount=0;_biloba.freezeRendering=()=>{document.querySelector('.box').style.background=['red','blue','green'][window.captureCount++%3];return {success:true,result:true}}`)
		Expect(err).NotTo(HaveOccurred())

		result, err := session.CompareScreenshot(ctx, "moving", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: GinkgoT().TempDir(), ArtifactDir: GinkgoT().TempDir(), Update: true,
			SettleAttempts: 4, SettleStreak: 3, SettleInterval: time.Millisecond,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Match).To(BeTrue())
		Expect(result.Warnings).To(ContainElement(ContainSubstring("never settled")))
		Expect(result.Schemes[0].BaselinePath).To(BeAnExistingFile())
	})

	It("uses unequal growing settle gaps that do not lock onto a periodic renderer", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px;background:black}</style><div class=box></div>")).To(Succeed())
		_, err = session.Exists(ctx, engine.CSS(".box"))
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `window.captureTimes=[];_biloba.freezeRendering=()=>{window.captureTimes.push(performance.now());return {success:true,result:true}}`)
		Expect(err).NotTo(HaveOccurred())

		_, err = session.CompareScreenshot(ctx, "cadence", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: GinkgoT().TempDir(), ArtifactDir: GinkgoT().TempDir(), Update: true,
			SettleAttempts: 3, SettleStreak: 3, SettleInterval: 100 * time.Millisecond,
		})
		Expect(err).NotTo(HaveOccurred())
		value, err := session.Evaluate(ctx, `window.captureTimes`)
		Expect(err).NotTo(HaveOccurred())
		times := value.([]any)
		Expect(times).To(HaveLen(3))
		firstGap := times[1].(float64) - times[0].(float64)
		secondGap := times[2].(float64) - times[1].(float64)
		Expect(secondGap-firstGap).To(BeNumerically("<", 80), "canonical gaps grow by about 40ms, not another full base interval")
	})

	It("restores color and animation state when update settling is canceled", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px;background:red;animation:pulse 1s infinite}@keyframes pulse{to{background:blue}}</style><div class=box></div>")).To(Succeed())
		before, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		captureCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = session.CompareScreenshot(captureCtx, "cancelled", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: GinkgoT().TempDir(), ArtifactDir: GinkgoT().TempDir(), Update: true,
			ColorSchemes: []string{"dark"}, SettleAttempts: 3, SettleStreak: 3, SettleInterval: time.Second,
		})
		Expect(err).To(HaveOccurred())
		after, err := session.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal([]any{before, float64(0)}))
	})

	It("retries failed color and freeze cleanup during prepare without crossing sessions", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Close)
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		page := "data:text/html,<style>.box{width:20px;height:20px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box></div>"
		Expect(first.Navigate(ctx, page)).To(Succeed())
		Expect(second.Navigate(ctx, page)).To(Succeed())

		colorClears, freezeClears := 0, 0
		restore := engine.SetVisualOperationHooksForTest(first, engine.VisualOperationHooksForTest{
			EmulateColorScheme: func(hookCtx context.Context, scheme string) error {
				if scheme == "" {
					colorClears++
					if colorClears == 1 {
						return errors.New("injected color cleanup failure")
					}
				}
				return engine.EmulateColorSchemeContext(hookCtx, scheme)
			},
			RunHandler: func(hookCtx context.Context, name, arg string) (engine.HandlerResponse, error) {
				if name == "unfreezeRendering" {
					freezeClears++
					if freezeClears == 1 {
						return engine.HandlerResponse{}, errors.New("injected freeze cleanup failure")
					}
				}
				return engine.RunHandlerContext(hookCtx, name, arg)
			},
		})
		DeferCleanup(restore)

		_, err = first.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{ColorScheme: "dark"})
		Expect(err).To(MatchError(And(
			ContainSubstring("injected color cleanup failure"),
			ContainSubstring("injected freeze cleanup failure"),
		)))
		firstState, err := first.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstState).To(Equal([]any{true, float64(1)}))
		secondState, err := second.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondState).To(Equal([]any{false, float64(0)}))

		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(colorClears).To(Equal(2))
		Expect(freezeClears).To(Equal(2))
		Expect(first.Navigate(ctx, page)).To(Succeed())
		cleared, err := first.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(cleared).To(Equal([]any{false, float64(0)}))
	})

	It("remembers color emulation when cancellation races setup and cleanup fails", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		page := "data:text/html,<style>.box{width:20px;height:20px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box></div>"
		Expect(session.Navigate(ctx, page)).To(Succeed())

		captureCtx, cancel := context.WithCancel(ctx)
		colorClears := 0
		restore := engine.SetVisualOperationHooksForTest(session, engine.VisualOperationHooksForTest{
			EmulateColorScheme: func(hookCtx context.Context, scheme string) error {
				if scheme == "dark" {
					if applyErr := engine.EmulateColorSchemeContext(hookCtx, scheme); applyErr != nil {
						return applyErr
					}
					cancel()
					return context.Canceled
				}
				colorClears++
				if colorClears == 1 {
					return errors.New("injected canceled cleanup failure")
				}
				return engine.EmulateColorSchemeContext(hookCtx, scheme)
			},
		})
		DeferCleanup(restore)

		_, err = session.CaptureElementScreenshot(captureCtx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{ColorScheme: "dark", Animated: true})
		Expect(err).To(HaveOccurred())
		active, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue())
		Expect(session.Navigate(ctx, page)).To(Succeed())
		active, err = session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue(), "navigation must not hide a leaked target-level override")
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(colorClears).To(Equal(2))
		Expect(session.Navigate(ctx, page)).To(Succeed())
		active, err = session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeFalse())
	})

	It("clears a possibly-landed freeze through crash recovery navigation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		page := "data:text/html,<style>.box{width:20px;height:20px;background:red;animation:pulse 1s infinite}@keyframes pulse{to{background:blue}}</style><div class=box></div>"
		Expect(session.Navigate(ctx, page)).To(Succeed())

		colorClears, freezeClears := 0, 0
		restore := engine.SetVisualOperationHooksForTest(session, engine.VisualOperationHooksForTest{
			EmulateColorScheme: func(hookCtx context.Context, scheme string) error {
				if scheme == "" {
					colorClears++
					if colorClears <= 2 {
						return errors.New("injected crashed color cleanup failure")
					}
				}
				return engine.EmulateColorSchemeContext(hookCtx, scheme)
			},
			RunHandler: func(hookCtx context.Context, name, arg string) (engine.HandlerResponse, error) {
				if name == "freezeRendering" {
					response, runErr := engine.RunHandlerContext(hookCtx, name, arg)
					if runErr != nil {
						return response, runErr
					}
					return response, errors.New("injected freeze setup acknowledgment failure")
				}
				if name == "unfreezeRendering" {
					freezeClears++
					if freezeClears == 1 {
						return engine.HandlerResponse{}, errors.New("injected freeze recovery failure")
					}
				}
				return engine.RunHandlerContext(hookCtx, name, arg)
			},
		})
		DeferCleanup(restore)

		_, err = session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{ColorScheme: "dark"})
		Expect(err).To(MatchError(And(
			ContainSubstring("setup acknowledgment failure"),
			ContainSubstring("freeze recovery failure"),
			ContainSubstring("crashed color cleanup failure"),
		)))
		frozen, err := session.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(frozen).To(Equal([]any{true, float64(1)}))

		engine.MarkSessionCrashedForTest(session)
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(colorClears).To(Equal(3), "prepare retries target emulation cleanup after recovery when the crashed target rejects it")
		Expect(freezeClears).To(Equal(1), "successful recovery navigation makes an old-DOM retry unnecessary")
		Expect(session.Navigate(ctx, page)).To(Succeed())
		frozen, err = session.Evaluate(ctx, `[matchMedia('(prefers-color-scheme: dark)').matches, document.querySelectorAll('#_biloba-freeze').length]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(frozen).To(Equal([]any{false, float64(0)}))
	})

	It("retries a leaked freeze before a same-document navigation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		page := "data:text/html,<div class=box style='width:20px;height:20px;background:red'></div>"
		Expect(session.Navigate(ctx, page)).To(Succeed())

		freezeClears := 0
		restore := engine.SetVisualOperationHooksForTest(session, engine.VisualOperationHooksForTest{
			RunHandler: func(hookCtx context.Context, name, arg string) (engine.HandlerResponse, error) {
				if name == "freezeRendering" {
					response, runErr := engine.RunHandlerContext(hookCtx, name, arg)
					if runErr != nil {
						return response, runErr
					}
					return response, errors.New("injected navigation setup failure")
				}
				if name == "unfreezeRendering" {
					freezeClears++
					if freezeClears == 1 {
						return engine.HandlerResponse{}, errors.New("injected navigation cleanup failure")
					}
				}
				return engine.RunHandlerContext(hookCtx, name, arg)
			},
		})
		DeferCleanup(restore)

		_, err = session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{})
		Expect(err).To(HaveOccurred())
		Expect(session.Navigate(ctx, page+"#next")).To(Succeed())
		Expect(freezeClears).To(Equal(2))
		frozen, err := session.Evaluate(ctx, `document.querySelectorAll('#_biloba-freeze').length`)
		Expect(err).NotTo(HaveOccurred())
		Expect(frozen).To(Equal(float64(0)))
	})
})

var _ = Describe("screenshot diff", func() {
	It("returns structured dimensions, regions, and an observable PNG diff", func() {
		baseline := solidPNG(8, 8, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		actualImage := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				actualImage.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		actualImage.SetNRGBA(6, 2, color.NRGBA{R: 255, A: 255})
		actual := encodePNG(actualImage)

		diff, diffPNG, err := engine.CompareScreenshotPNGs(baseline, actual, engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.Match).To(BeFalse())
		Expect(diff.DifferingPixels).To(Equal(1))
		Expect(diff.MaxChannelDelta).To(Equal(255))
		Expect(diff.Regions).To(ContainElement(engine.ScreenshotRegion{Rect: image.Rect(6, 2, 7, 3), Count: 1}))
		Expect(bytes.HasPrefix(diffPNG, []byte("\x89PNG\r\n\x1a\n"))).To(BeTrue())
		Expect(pixelAt(diffPNG, 6, 2)).To(Equal(color.NRGBA{R: 255, B: 255, A: 255}))
		Expect(pixelAt(diffPNG, 0, 0)).To(Equal(color.NRGBA{R: 255, G: 255, B: 255, A: 255}))
	})

	It("clusters nearby diagonal changes and bounds the reported region list", func() {
		baseline := solidPNG(160, 160, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		actual, err := png.Decode(bytes.NewReader(baseline))
		Expect(err).NotTo(HaveOccurred())
		changed := image.NewNRGBA(actual.Bounds())
		for y := 0; y < 160; y++ {
			for x := 0; x < 160; x++ {
				changed.Set(x, y, actual.At(x, y))
			}
		}
		changed.SetNRGBA(8, 8, color.NRGBA{A: 255})
		changed.SetNRGBA(9, 9, color.NRGBA{A: 255})
		for i := 0; i < 8; i++ {
			changed.SetNRGBA(20+i*16, 120, color.NRGBA{A: 255})
		}

		diff, _, err := engine.CompareScreenshotPNGs(baseline, encodePNG(changed), engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.RegionCount).To(Equal(9))
		Expect(diff.Regions).To(HaveLen(5))
		Expect(diff.Regions).To(ContainElement(engine.ScreenshotRegion{Rect: image.Rect(8, 8, 10, 10), Count: 2}))
	})

	It("classifies whole-image shifts and scattered font-like changes structurally", func() {
		base := patternedImage(200, 200)
		shifted := image.NewNRGBA(base.Bounds())
		for y := 0; y < 200; y++ {
			sourceY := y - 1
			if sourceY < 0 {
				sourceY = 0
			}
			for x := 0; x < 200; x++ {
				shifted.SetNRGBA(x, y, base.NRGBAAt(x, sourceY))
			}
		}
		diff, _, err := engine.CompareScreenshotPNGs(encodePNG(base), encodePNG(shifted), engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.Shifted).To(BeTrue())
		Expect(diff.Shift).To(Equal(image.Pt(0, 1)))

		scattered := image.NewNRGBA(image.Rect(0, 0, 400, 400))
		for y := 0; y < 400; y++ {
			for x := 0; x < 400; x++ {
				scattered.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		for row := 0; row < 5; row++ {
			for column := 0; column < 5; column++ {
				for y := 10 + row*90; y < 16+row*90; y++ {
					for x := 10 + column*90; x < 16+column*90; x++ {
						scattered.SetNRGBA(x, y, color.NRGBA{A: 255})
					}
				}
			}
		}
		diff, _, err = engine.CompareScreenshotPNGs(solidPNG(400, 400, color.NRGBA{R: 255, G: 255, B: 255, A: 255}), encodePNG(scattered), engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.Scattered).To(BeTrue())
		Expect(diff.RegionCount).To(Equal(25))
	})

	It("classifies low-amplitude rasterization and a provably untouched side", func() {
		base := image.NewNRGBA(image.Rect(0, 0, 100, 100))
		actual := image.NewNRGBA(base.Bounds())
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				pixel := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
				base.SetNRGBA(x, y, pixel)
				actual.SetNRGBA(x, y, pixel)
			}
		}
		for y := 4; y < 14; y++ {
			for x := 20; x < 50; x++ {
				actual.SetNRGBA(x, y, color.NRGBA{R: 105, G: 100, B: 100, A: 255})
			}
		}

		diff, _, err := engine.CompareScreenshotPNGs(encodePNG(base), encodePNG(actual), engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.RasterizationLikely).To(BeTrue())
		Expect(diff.Unchanged).To(Equal("everything below y=14"))
		diagnosis := diff.Diagnose("text", engine.ScreenshotPaths{})
		Expect(diagnosis).To(ContainSubstring("rasterisation or compositing difference"))
		Expect(diagnosis).To(ContainSubstring("unchanged: everything below y=14"))
	})

	It("rejects malformed and oversized PNG artifacts", func() {
		path := filepath.Join(GinkgoT().TempDir(), "shot.png")
		Expect(engine.WriteScreenshotPNG(path, []byte("not-png"), 1024)).To(MatchError(ContainSubstring("decode PNG")))
		Expect(engine.WriteScreenshotPNG(path, solidPNG(2, 2, color.NRGBA{A: 255}), 4)).To(MatchError(ContainSubstring("exceeds the 4-byte limit")))
		_, err := os.Stat(path)
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())

		huge := pngWithDeclaredDimensions(100_000, 100_000)
		Expect(engine.WriteScreenshotPNG(path, huge, len(huge)+1)).To(MatchError(ContainSubstring("pixel limit")))
	})

	It("reports dimension changes in words without manufacturing a diff image", func() {
		diff, diffPNG, err := engine.CompareScreenshotPNGs(
			solidPNG(80, 60, color.NRGBA{A: 255}),
			solidPNG(80, 64, color.NRGBA{A: 255}),
			engine.ScreenshotTolerance{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.DimensionMismatch).To(BeTrue())
		Expect(diffPNG).To(BeNil())
		Expect(diff.Diagnose("rail", engine.ScreenshotPaths{})).To(ContainSubstring("baseline is 80x60, actual is 80x64 (4px taller)"))
	})

	It("applies channel tolerance to alpha and rejects non-finite pixel tolerance", func() {
		base := solidPNG(2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 250})
		actual := solidPNG(2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 245})
		diff, _, err := engine.CompareScreenshotPNGs(base, actual, engine.ScreenshotTolerance{ChannelDelta: 5})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.Match).To(BeTrue())
		Expect(diff.MaxChannelDelta).To(Equal(5))

		_, _, err = engine.CompareScreenshotPNGs(base, actual, engine.ScreenshotTolerance{PixelFraction: math.NaN()})
		Expect(err).To(MatchError(ContainSubstring("pixel tolerance")))
	})
})

func solidPNG(width, height int, fill color.NRGBA) []byte {
	GinkgoHelper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	GinkgoHelper()
	var out bytes.Buffer
	Expect(png.Encode(&out, img)).To(Succeed())
	return out.Bytes()
}

func pixelAt(data []byte, x, y int) color.NRGBA {
	GinkgoHelper()
	img, err := png.Decode(bytes.NewReader(data))
	Expect(err).NotTo(HaveOccurred())
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func patternedImage(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := uint8((7*x + 13*y) % 251)
			img.SetNRGBA(x, y, color.NRGBA{R: value, G: value, B: value, A: 255})
		}
	}
	return img
}

func pngWithDeclaredDimensions(width, height uint32) []byte {
	data := append([]byte(nil), solidPNG(1, 1, color.NRGBA{A: 255})...)
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}
