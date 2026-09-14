package engine_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame accessibility regressions", func() {
	It("returns the child accessibility tree without the owning tab's controls", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><title>Child accessibility</title><button>Child action</button>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.title = 'Parent accessibility';
			document.body.innerHTML = '<button>Parent action</button><iframe id="child"></iframe>';
			document.querySelector('#child').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{
			Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "Child accessibility"},
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).To(Equal(root.TargetID()), "the frame must share the tab's renderer target")
		blocked, err := root.Evaluate(ctx, `document.querySelector('#child').contentDocument === null`)
		Expect(err).NotTo(HaveOccurred())
		Expect(blocked).To(BeTrue(), "the fixture must cross a browser origin boundary")

		outline, err := frame.AccessibilityOutline(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(outline).To(And(
			ContainSubstring(`RootWebArea "Child accessibility"`),
			ContainSubstring(`button "Child action"`),
			Not(ContainSubstring("Parent accessibility")),
			Not(ContainSubstring("Parent action")),
		))
	})
})
