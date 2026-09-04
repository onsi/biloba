package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/biloba/engine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("context diagnostics", func() {
	It("captures full-page artifacts for every live page tab in the root context only", func(ctx SpecContext) {
		diagnosticsBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(), Arguments: []string{"--site-per-process"}, ArtifactDir: GinkgoT().TempDir(),
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(diagnosticsBrowser.Close)
		root, err := diagnosticsBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		Expect(sibling.Navigate(ctx, server.URL+"/destination")).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.style.minHeight='1700px'; window.diagnosticsWorker = new Worker(URL.createObjectURL(new Blob(['setInterval(()=>{}, 1000)'])))`)
		Expect(err).NotTo(HaveOccurred())

		frameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/destination"
		_, err = root.Evaluate(ctx, `(() => { const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(frameURL)+`; document.body.append(frame) })()`)
		Expect(err).NotTo(HaveOccurred())
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{
			Kind: engine.ExpectSuffix, Expected: "/destination",
		}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)

		other, err := diagnosticsBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)
		Expect(other.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeFailure, Name: "ordinary expectation / failed", Screenshots: true, Outlines: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Purpose).To(Equal(engine.DiagnosticsPurposeFailure))
		Expect(capture.Tabs).To(HaveLen(2))
		Expect(capture.Tabs).To(ContainElements(
			HaveField("TargetID", root.TargetID()),
			HaveField("TargetID", sibling.TargetID()),
		))
		for _, tab := range capture.Tabs {
			Expect(tab.TargetID).NotTo(Equal(other.TargetID()))
			Expect(tab.Errors).To(BeEmpty())
			Expect(tab.ScreenshotPath).To(BeAnExistingFile())
			Expect(tab.OutlinePath).To(BeAnExistingFile())
			Expect(tab.DOMOutline).To(ContainSubstring("<"))
			file, openErr := os.Open(tab.ScreenshotPath)
			Expect(openErr).NotTo(HaveOccurred())
			image, decodeErr := png.Decode(file)
			_ = file.Close()
			Expect(decodeErr).NotTo(HaveOccurred())
			if tab.TargetID == root.TargetID() {
				Expect(image.Bounds().Dy()).To(BeNumerically(">=", 1700), "the root artifact must be full-page")
			} else {
				Expect(image.Bounds().Dy()).To(BeNumerically(">=", 768))
			}
			Expect(filepath.Clean(tab.ScreenshotPath)).To(HavePrefix(filepath.Clean(capture.ArtifactDir) + string(os.PathSeparator)))
		}
	})

	It("honors disabled artifact modes while retaining per-tab metadata", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		tab, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeProgress, Name: "progress", Screenshots: false, Outlines: false,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Tabs).To(HaveLen(2))
		for _, result := range capture.Tabs {
			Expect(result.ScreenshotPath).To(BeEmpty())
			Expect(result.OutlinePath).To(BeEmpty())
			Expect(result.DOMOutline).To(BeEmpty())
			Expect(result.Errors).To(BeEmpty())
		}
		Expect(capture.Tabs).To(ContainElement(HaveField("TargetID", tab.TargetID())))
	})

	It("uses collision-safe paths and restores each temporary viewport", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())
		Expect(root.SetWindowSize(ctx, 713, 521)).To(Succeed())

		options := engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "../../same:name", Screenshots: true,
			Viewport: &engine.ViewportSize{Width: 360, Height: 240},
		}
		first, err := root.CaptureContextDiagnostics(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		second, err := root.CaptureContextDiagnostics(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Tabs).To(HaveLen(1))
		Expect(second.Tabs).To(HaveLen(1))
		Expect(first.Tabs[0].ScreenshotPath).NotTo(Equal(second.Tabs[0].ScreenshotPath))
		Expect(filepath.Base(first.Tabs[0].ScreenshotPath)).NotTo(ContainSubstring(".."))
		width, height, err := root.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect([]int{width, height}).To(Equal([]int{713, 521}))
	})

	It("bounds diagnostic screenshot resources before writing an artifact", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())
		Expect(root.SetWindowSize(ctx, 619, 477)).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "bounded", Screenshots: true, MaxBytes: 16,
			Viewport: &engine.ViewportSize{Width: 320, Height: 200},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Tabs).To(HaveLen(1))
		Expect(capture.Tabs[0].ScreenshotPath).To(BeEmpty())
		Expect(capture.Tabs[0].Errors).To(ContainElement(And(
			HaveField("Artifact", "screenshot"),
			HaveField("Message", ContainSubstring("exceeds the 16-byte limit")),
		)))
		width, height, sizeErr := root.WindowSize(ctx)
		Expect(sizeErr).NotTo(HaveOccurred())
		Expect([]int{width, height}).To(Equal([]int{619, 477}))
	})

	It("returns bounded screenshot bytes without requiring an artifact directory", func(ctx SpecContext) {
		worker, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath()})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(worker.Close)
		root, err := worker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "memory-only", Screenshots: true,
			IncludeScreenshotBytes: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.ArtifactDir).To(BeEmpty())
		Expect(capture.Tabs).To(HaveLen(1))
		Expect(capture.Tabs[0].ScreenshotPath).To(BeEmpty())
		Expect(capture.Tabs[0].Errors).To(BeEmpty())
		image, decodeErr := png.Decode(bytes.NewReader(capture.Tabs[0].Screenshot))
		Expect(decodeErr).NotTo(HaveOccurred())
		Expect(image.Bounds().Dx()).To(BeNumerically(">", 0))
	})

	It("returns the exact bytes written to the artifact path from one capture", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "bytes-and-path", Screenshots: true,
			IncludeScreenshotBytes: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Tabs).To(HaveLen(1))
		written, readErr := os.ReadFile(capture.Tabs[0].ScreenshotPath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(capture.Tabs[0].Screenshot).To(Equal(written))
	})

	It("does not retain screenshot bytes when the bounded capture is rejected", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "bounded-memory", Screenshots: true,
			IncludeScreenshotBytes: true, MaxBytes: 16,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Tabs).To(HaveLen(1))
		Expect(capture.Tabs[0].Screenshot).To(BeEmpty())
		Expect(capture.Tabs[0].ScreenshotPath).To(BeEmpty())
		Expect(capture.Tabs[0].Errors).To(ContainElement(And(
			HaveField("Artifact", "screenshot"),
			HaveField("Message", ContainSubstring("exceeds the 16-byte limit")),
		)))
	})

	It("reports artifact failures per tab and restores the temporary viewport", func(ctx SpecContext) {
		badArtifactRoot := filepath.Join(GinkgoT().TempDir(), "not-a-directory")
		Expect(os.WriteFile(badArtifactRoot, []byte("file"), 0o600)).To(Succeed())
		worker, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(), ArtifactDir: badArtifactRoot,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(worker.Close)
		root, err := worker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.SetWindowSize(ctx, 641, 479)).To(Succeed())

		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeFailure, Name: "write failure", Screenshots: true, Outlines: true,
			Viewport: &engine.ViewportSize{Width: 320, Height: 200},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(capture.Tabs).To(HaveLen(1))
		Expect(capture.Tabs[0].Errors).To(ContainElements(
			HaveField("Artifact", "outline-write"),
			HaveField("Artifact", "screenshot-write"),
		))
		for _, artifactErr := range capture.Tabs[0].Errors {
			Expect(artifactErr.Code).To(Equal(engine.CodeIO))
		}
		Expect(capture.Tabs[0].ScreenshotPath).To(BeEmpty())
		width, height, sizeErr := root.WindowSize(ctx)
		Expect(sizeErr).NotTo(HaveOccurred())
		Expect([]int{width, height}).To(Equal([]int{641, 479}))
	})

	It("restores a temporary viewport when capture is cancelled", func(ctx SpecContext) {
		resized := make(chan struct{}, 4)
		resizeServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/resized" {
				select {
				case resized <- struct{}{}:
				default:
				}
				return
			}
			fmt.Fprint(response, `<!doctype html><body><script>
			for (let i=0;i<20000;i++) document.body.append(document.createElement('div'))
			addEventListener('resize', () => fetch('/resized'))
			</script></body>`)
		}))
		DeferCleanup(resizeServer.Close)
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, resizeServer.URL)).To(Succeed())
		Expect(root.SetWindowSize(ctx, 677, 433)).To(Succeed())
		Eventually(resized).Within(time.Second).Should(Receive())
		for len(resized) > 0 {
			<-resized
		}
		captureCtx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-resized:
				cancel()
			case <-captureCtx.Done():
			}
		}()

		_, err = root.CaptureContextDiagnostics(captureCtx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeProgress, Name: "cancelled", Screenshots: true, Outlines: true,
			Viewport: &engine.ViewportSize{Width: 300, Height: 180},
		})
		Expect(err).To(HaveOccurred())
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeCanceled))
		width, height, sizeErr := root.WindowSize(ctx)
		Expect(sizeErr).NotTo(HaveOccurred())
		Expect([]int{width, height}).To(Equal([]int{677, 433}))
	})

	It("returns promptly with structured tab errors after a close or crash", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		closed, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(closed.Close()).To(Succeed())
		engine.MarkSessionCrashedForTest(root)

		started := time.Now()
		capture, err := root.CaptureContextDiagnostics(ctx, engine.DiagnosticsCaptureOptions{
			Purpose: engine.DiagnosticsPurposeOnDemand, Name: "crashed", Screenshots: true, Outlines: true,
			Viewport: &engine.ViewportSize{Width: 300, Height: 180},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(time.Since(started)).To(BeNumerically("<", 2*time.Second))
		Expect(capture.Tabs).To(HaveLen(1), "a closed sibling must not reappear")
		Expect(capture.Tabs[0].TargetID).To(Equal(root.TargetID()))
		Expect(capture.Tabs[0].Errors).To(ContainElement(And(
			HaveField("Artifact", "tab"),
			HaveField("Code", engine.CodePageCrashed),
		)))
		Expect(root.Prepare(ctx)).To(Succeed())
	})
})
