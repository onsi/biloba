package engine_test

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame visual regressions", func() {
	It("captures an offset same-process cross-origin element from the child document", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><title>visual-child</title><style>html,body{margin:0;height:900px}.subject{position:absolute;left:20px;top:520px;width:80px;height:60px;background:rgb(0,255,0)}.mask{width:20px;height:20px;background:white}</style><div class="subject"><div class="mask"></div></div>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.head.innerHTML = '<style>html,body{margin:0;min-height:1500px;background:rgb(255,0,0)}</style>'; document.body.innerHTML = '<iframe id="child" style="position:absolute;left:350px;top:950px;width:300px;height:200px;border:0;transform:scale(.75);transform-origin:top left"></iframe>'; document.querySelector('#child').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"}, HasElement: selectorPtr(engine.CSS(".subject"))}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		rootOrigin, err := url.Parse(server.URL)
		Expect(err).NotTo(HaveOccurred())
		childOrigin, err := url.Parse(child.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(childOrigin.Hostname()).To(Equal(rootOrigin.Hostname()))
		Expect(childOrigin.Host).NotTo(Equal(rootOrigin.Host))
		Expect(frame.TargetID()).To(Equal(root.TargetID()), "fixture must exercise a cross-origin frame sharing its renderer target")
		_, err = frame.Evaluate(ctx, `window.scrollTo(0, 450)`)
		Expect(err).NotTo(HaveOccurred())

		shot, err := frame.CaptureElementScreenshot(ctx, engine.CSS(".subject"), engine.ScreenshotCaptureOptions{
			Animated: true,
			Masks:    []engine.Selector{engine.CSS(".mask")},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(shot.Width).To(Equal(60))
		Expect(shot.Height).To(Equal(45))
		Expect(pixelAt(shot.PNG, 8, 8)).To(Equal(color.NRGBA{R: 128, G: 128, B: 128, A: 255}))
		Expect(pixelAt(shot.PNG, 30, 22)).To(Equal(color.NRGBA{G: 255, A: 255}))
	})

	It("restores the child animation state after a default frame screenshot", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><title>animated-child</title><style>.subject{width:40px;height:40px;background:red;animation:pulse 60s linear infinite}@keyframes pulse{to{background:blue}}</style><div class="subject"></div>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe id="child"></iframe>'; document.querySelector('#child').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())
		_, err = root.Exists(ctx, engine.CSS("body"))
		Expect(err).NotTo(HaveOccurred(), "initialize the parent runtime so cleanup in the wrong world succeeds silently")

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"}, HasElement: selectorPtr(engine.CSS(".subject"))}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).To(Equal(root.TargetID()), "fixture must exercise a cross-origin frame sharing its renderer target")

		shot, err := frame.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(shot.PNG).NotTo(BeEmpty())
		state, err := frame.Evaluate(ctx, `[document.querySelectorAll('#_biloba-freeze').length, getComputedStyle(document.querySelector('.subject')).animationName]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal([]any{float64(0), "pulse"}))
	})
})
