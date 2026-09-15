package engine_test

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// A same-process frame handle shares its tab's renderer attachment. These specs pin the places where
// that attachment would otherwise leak the tab into a frame operation, or the frame into the tab.
var _ = Describe("frame handles and their tab", func() {
	var root *engine.Session
	var frame *engine.Frame

	engineCode := func(err error) engine.ErrorCode {
		GinkgoHelper()
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue(), "expected an engine error, got %v", err)
		return engineErr.Code
	}

	BeforeEach(func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/api" {
				_, _ = response.Write([]byte("api"))
				return
			}
			_, _ = response.Write([]byte(`<!doctype html><title>child</title><style>html,body{margin:0;background:rgb(0,170,0)}</style>
				<button id="child-button" style="position:absolute;left:20px;top:20px;width:120px;height:50px" onclick="this.textContent='clicked'">Child</button>`))
		}))
		DeferCleanup(child.Close)

		var err error
		root, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.style.background = 'rgb(255,0,0)';
			document.body.innerHTML = '<iframe id="child" style="position:absolute;left:60px;top:40px;width:300px;height:200px;border:0"></iframe><div id="overlay" style="position:absolute;left:0;top:0;width:500px;height:400px;display:none"></div>';
			document.querySelector('#overlay').onclick = event => { event.currentTarget.dataset.clicked = 'yes' };
			document.querySelector('#child').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())
		frame, err = root.WaitForFrame(ctx, engine.FrameQuery{HasElement: selectorPtr(engine.CSS("#child-button"))}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).To(Equal(root.TargetID()), "the fixture must exercise a frame sharing its tab's renderer")
	})

	It("rejects tab and browser-context controls without touching the tab", func(ctx SpecContext) {
		controls := map[string]func() error{
			"navigate":     func() error { return frame.Navigate(ctx, server.URL+"/destination") },
			"prepare":      func() error { return frame.Prepare(ctx) },
			"init script":  func() error { return frame.AddInitScript(ctx, `window.fromFrameInitScript = true`) },
			"activate":     func() error { return frame.Activate(ctx) },
			"window size":  func() error { return frame.SetWindowSize(ctx, 400, 300) },
			"cookies":      func() error { return frame.SetCookies(ctx, []engine.Cookie{{Name: "frame", Value: "cookie"}}) },
			"clear cookie": func() error { return frame.ClearCookies(ctx) },
			"device": func() error {
				return frame.SetDeviceMetrics(ctx, engine.DeviceMetrics{Width: 400, Height: 300, DeviceScaleFactor: 1})
			},
			"geolocation": func() error { return frame.SetGeolocation(ctx, engine.Geolocation{Latitude: 1, Longitude: 1}) },
			"permissions": func() error { return frame.ResetPermissions(ctx) },
			"locale":      func() error { return frame.SetLocale(ctx, "fr-FR") },
			"timezone":    func() error { return frame.SetTimezone(ctx, "Asia/Tokyo") },
			"media":       func() error { return frame.SetMedia(ctx, engine.Media{ColorScheme: "dark"}) },
			"offline":     func() error { return frame.SetNetworkState(ctx, engine.NetworkState{Offline: true}) },
			"cache":       func() error { return frame.SetCacheEnabled(ctx, false) },
			"hold": func() error {
				_, err := frame.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectContains, Expected: "/api"})
				return err
			},
			"dialogs": func() error {
				_, err := frame.RegisterDialogHandler(ctx, engine.DialogHandlerOptions{Type: engine.DialogAlert})
				return err
			},
			"stub": func() error {
				body := []byte("stubbed")
				_, err := frame.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{URL: engine.Expectation{Kind: engine.ExpectContains, Expected: "/api"}, Fulfill: &engine.ResponseOverride{Body: &body}})
				return err
			},
			"new tab": func() error { _, err := frame.NewTab(ctx); return err },
		}
		for name, control := range controls {
			Expect(engineCode(control())).To(Equal(engine.CodeInvalidArgument), name)
		}

		location, err := root.URL(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(location.Value).To(Equal(server.URL+"/"), "the tab must not have navigated")
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		installed, err := root.Evaluate(ctx, `window.fromFrameInitScript === true`)
		Expect(err).NotTo(HaveOccurred())
		Expect(installed).To(BeFalse(), "the frame's init script must not reach the tab")
	})

	It("does not report a realistic click that the embedding page's overlay would take", func(ctx SpecContext) {
		_, err := root.Evaluate(ctx, `document.querySelector('#overlay').style.display = 'block'`)
		Expect(err).NotTo(HaveOccurred())

		err = frame.RealisticClick(ctx, engine.CSS("#child-button"))
		Expect(engineCode(err)).To(Equal(engine.CodeActionFailed))
		Expect(err.Error()).To(ContainSubstring("obscured by the page that embeds its frame"))
		err = frame.ClickWith(ctx, engine.CSS("#child-button"), engine.ClickOptions{Mode: engine.Realistic, Button: engine.LeftButton, Count: 1, Offset: &engine.Point{X: 5, Y: 5}})
		Expect(engineCode(err)).To(Equal(engine.CodeActionFailed))
		clickable, err := frame.Clickable(ctx, engine.CSS("#child-button"))
		Expect(err).NotTo(HaveOccurred())
		Expect(clickable.Value).To(BeTrue(), "the fast-path check only sees the frame's own document")
		overlay, err := root.Evaluate(ctx, `document.querySelector('#overlay').dataset.clicked || ''`)
		Expect(err).NotTo(HaveOccurred())
		Expect(overlay).To(Equal(""))

		_, err = root.Evaluate(ctx, `document.querySelector('#overlay').style.display = 'none'`)
		Expect(err).NotTo(HaveOccurred())
		Expect(frame.RealisticClick(ctx, engine.CSS("#child-button"))).To(Succeed())
		text, err := frame.Text(ctx, engine.CSS("#child-button"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("clicked"))
	})

	It("captures the frame's viewport, not the tab, as its page screenshot", func(ctx SpecContext) {
		shot, err := frame.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		decoded, err := png.Decode(bytes.NewReader(shot.PNG))
		Expect(err).NotTo(HaveOccurred())
		bounds := decoded.Bounds()
		Expect(bounds.Dx()).To(BeNumerically("~", 300, 1))
		Expect(bounds.Dy()).To(BeNumerically("~", 200, 1))
		for _, point := range [][2]int{{2, 2}, {bounds.Dx() - 3, bounds.Dy() - 3}} {
			r, g, b, _ := decoded.At(point[0], point[1]).RGBA()
			Expect([]uint32{r >> 8, g >> 8, b >> 8}).To(Equal([]uint32{0, 170, 0}), "every corner must be the frame's background, not the tab's")
		}
	})

	It("records the frame's own console messages and requests", func(ctx SpecContext) {
		_, err := root.Evaluate(ctx, `console.log('from-parent')`)
		Expect(err).NotTo(HaveOccurred())
		_, err = frame.EvaluateAsync(ctx, `fetch('/api').then(response => response.text()).then(text => console.log('from-frame', text))`)
		Expect(err).NotTo(HaveOccurred())

		texts := func(session *engine.Session) []string {
			out := []string{}
			for _, message := range session.ConsoleMessages() {
				out = append(out, message.Text)
			}
			return out
		}
		Eventually(func() []string { return texts(frame.Session) }).Should(Equal([]string{"from-frame api"}))
		Expect(texts(root)).To(ContainElements("from-parent", "from-frame api"), "the tab still sees its same-process frames")
		requests := frame.RequestsMatching(engine.RequestQuery{URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/api"}})
		Expect(requests).To(HaveLen(1))
		Expect(frame.RequestsMatching(engine.RequestQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: server.URL + "/"}})).To(BeEmpty())
	})
})

var _ = Describe("frames that share their parent's origin", func() {
	It("leaves about:blank and srcdoc iframes, which inherit the page's origin, to >>>", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		// Chrome reports "://" as the security origin of all three; only the data: document is opaque.
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe id="blank"></iframe><iframe id="inline" srcdoc="<p id=inline-p>inline</p>"></iframe><iframe id="opaque" src="data:text/html,<p id=opaque-p>opaque</p>"></iframe>'`)
		Expect(err).NotTo(HaveOccurred())
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{HasElement: selectorPtr(engine.CSS("#opaque-p"))}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		exists, err := root.Exists(ctx, engine.CSS("#inline >>> #inline-p"))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists.Value).To(BeTrue())

		frames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(frames).To(HaveLen(1), "only the opaque data: document is behind a cross-origin boundary")
		Expect(frames[0].URL()).To(HavePrefix("data:"))
	})
})

var _ = Describe("out-of-process frame screenshots", func() {
	It("fails with an actionable error instead of Chrome's top-level-target refusal", func(ctx SpecContext) {
		isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath(), Arguments: []string{"--site-per-process"}})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolatedBrowser.Close)
		root, err := isolatedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `const frame = document.createElement('iframe'); frame.id = 'oopif'; frame.src = `+strconv.Quote(strings.Replace(server.URL, "127.0.0.1", "localhost", 1)+"/destination")+`; document.body.append(frame)`)
		Expect(err).NotTo(HaveOccurred())
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{HasElement: selectorPtr(engine.TestID("destination"))}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).NotTo(Equal(root.TargetID()), "the fixture must exercise an out-of-process frame")

		_, err = frame.CaptureElementScreenshot(ctx, engine.TestID("destination"), engine.ScreenshotCaptureOptions{})
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeInvalidArgument))
		Expect(engineErr.Message).To(ContainSubstring("capture the iframe element from the session that owns the frame"))

		shotCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		shot, err := root.CaptureElementScreenshot(shotCtx, engine.CSS("#oopif"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred(), "the workaround the error names must work")
		Expect(shot.Width).To(BeNumerically(">", 0))
	})
})
