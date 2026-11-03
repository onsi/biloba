package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// The frame-world registry a frame handle looks its document up in starts from what the connection
// recorded from the session's first message, so attaching a tab costs no extra commands.
var _ = Describe("frame-world registries", func() {
	var root *engine.Session
	var rootCtx context.Context
	var childURL string

	// openPopupWithFrame opens a popup whose cross-origin frame has finished loading before anything
	// attaches to the popup's tab, and returns the popup's target.
	openPopupWithFrame := func(ctx context.Context) target.ID {
		GinkgoHelper()
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><div id="ready">ready</div><script>window.frameValue = "page-window"</script>`))
		}))
		DeferCleanup(child.Close)
		childURL = child.URL + "/"
		site := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/popup" {
				_, _ = response.Write([]byte(`<!doctype html><iframe src=` + strconv.Quote(child.URL) + ` onload="opener.postMessage('popup-frame-ready', '*')"></iframe>`))
				return
			}
			_, _ = response.Write([]byte(`<!doctype html><title>opener</title>`))
		}))
		DeferCleanup(site.Close)
		Expect(root.Navigate(ctx, site.URL)).To(Succeed())
		ready, err := root.EvaluateAsync(ctx, `new Promise((resolve, reject) => {
			addEventListener("message", event => { if (event.data === "popup-frame-ready") resolve(true) });
			if (!window.open("/popup", "_blank")) reject(new Error("popup was blocked"));
		})`)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeTrue(), "the popup's frame must finish loading before anything attaches to its tab")
		targets, err := chromedp.Targets(rootCtx)
		Expect(err).NotTo(HaveOccurred())
		for _, info := range targets {
			if info.Type == "page" && info.OpenerID == root.TargetID() {
				return info.TargetID
			}
		}
		Fail("the popup's target is not listed")
		return ""
	}

	BeforeEach(func(ctx SpecContext) {
		var err error
		root, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		rootCtx = engine.SessionContextForTest(root)
	})

	It("finds a frame the popup loaded before its tab was attached, without asking Chrome to announce its contexts again", func(ctx SpecContext) {
		popupID := openPopupWithFrame(ctx)

		var lock sync.Mutex
		disables := 0
		enables := map[target.SessionID]int{}
		DeferCleanup(engine.ObserveWritesForTest(func(method string, session target.SessionID) {
			lock.Lock()
			defer lock.Unlock()
			switch method {
			case runtime.Disable.Method:
				disables++
			case runtime.Enable.Method:
				enables[session]++
			}
		}))
		tabs, err := root.Tabs(ctx)
		Expect(err).NotTo(HaveOccurred())
		var popup *engine.Session
		for _, tab := range tabs {
			if tab.TargetID() == popupID {
				popup = tab
			}
		}
		Expect(popup).NotTo(BeNil())
		DeferCleanup(popup.Close)

		frame, err := popup.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectEqual, Expected: childURL},
			HasElement: selectorPtr(engine.CSS("#ready")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		value, err := frame.Evaluate(ctx, `window.frameValue`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("page-window"), "the frame's page world, which existed before the attach")

		lock.Lock()
		defer lock.Unlock()
		Expect(disables).To(BeZero(), "nothing disables Runtime to have Chrome announce the contexts again")
		Expect(enables).To(HaveLen(1), "only the popup's tab was attached")
		for _, count := range enables {
			Expect(count).To(Equal(1), "Runtime is enabled once, by chromedp's own attach")
		}
	})

	It("gives overlapping attaches of one tab each the registry of their own session", func(ctx SpecContext) {
		popupID := openPopupWithFrame(ctx)
		DeferCleanup(func() { _ = engine.CloseTargetContext(rootCtx, popupID) })
		tabs, err := root.Tabs(ctx)
		Expect(err).NotTo(HaveOccurred())
		var popup *engine.Session
		for _, tab := range tabs {
			if tab.TargetID() == popupID {
				popup = tab
			}
		}
		Expect(popup).NotTo(BeNil())
		frame, err := popup.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: childURL}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)

		// two more attachments of the popup's tab, brought up at the same time
		attach := func() (context.Context, context.CancelFunc, <-chan error) {
			attached, detach := engine.NewTabContext(rootCtx, popupID)
			done := make(chan error, 1)
			go func() { done <- chromedp.Do(attached, chromedp.Evaluate[chromedp.Void]("1")) }()
			return attached, detach, done
		}
		first, detachFirst, firstDone := attach()
		second, detachSecond, secondDone := attach()
		DeferCleanup(detachSecond)
		Eventually(firstDone).Should(Receive(BeNil()))
		Eventually(secondDone).Should(Receive(BeNil()))
		first, err = engine.TrackFrameWorlds(first)
		Expect(err).NotTo(HaveOccurred())
		second, err = engine.TrackFrameWorlds(second)
		Expect(err).NotTo(HaveOccurred())
		firstSession := chromedp.FromContext(first).Target.SessionID
		Expect(chromedp.FromContext(second).Target.SessionID).NotTo(Equal(firstSession), "each attach is a session of its own")

		firstWorld, err := engine.FrameWorldForTest(first, frame.FrameID())
		Expect(err).NotTo(HaveOccurred(), "an attach finds the frame's world, announced on its own session")
		secondWorld, err := engine.FrameWorldForTest(second, frame.FrameID())
		Expect(err).NotTo(HaveOccurred())
		Expect(secondWorld).To(Equal(firstWorld), "both sessions name the same document")

		detachFirst()
		Eventually(func() bool { return engine.HasFrameWorldsForTest(root, firstSession) }).Should(BeFalse(), "a detached session's registry goes with it")
		secondWorld, err = engine.FrameWorldForTest(second, frame.FrameID())
		Expect(err).NotTo(HaveOccurred(), "the other attach keeps its registry")
		Expect(secondWorld).To(Equal(firstWorld))
	})

	It("leaves no registry behind for an attach the caller gave up on", func(ctx SpecContext) {
		created, err := chromedp.CallBrowser(rootCtx, target.CreateTarget, target.CreateTargetParams{
			URL: "about:blank", BrowserContextID: engine.SessionBrowserContextForTest(root), NewWindow: new(true),
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = engine.CloseTargetContext(rootCtx, created.TargetID) })
		stalled, held, release, err := engine.StallAttachForTest(ctx, root, created.TargetID)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(release)

		gaveUp, cancel := context.WithCancel(ctx)
		recordedWhileStalled := make(chan bool, 1)
		go func() {
			select {
			case <-stalled:
				recordedWhileStalled <- engine.HasFrameWorldsForTest(root, held())
			case <-ctx.Done():
			}
			cancel()
		}()
		tab, err := engine.AttachSpawnedTabForTest(gaveUp, root, created.TargetID)
		Expect(tab).To(BeNil())
		Expect(err).To(HaveOccurred())
		Expect(stalled).To(BeClosed(), "the attach got through and had a session")
		Expect(recordedWhileStalled).To(Receive(BeTrue()), "the attach had Chrome announce the tab's contexts before it stalled")
		release()

		session := held()
		Expect(session).NotTo(BeEmpty())
		Eventually(func() bool { return engine.HasFrameWorldsForTest(root, session) }).Should(BeFalse())
	})
})
