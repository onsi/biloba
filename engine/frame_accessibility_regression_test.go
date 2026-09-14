package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame accessibility regressions", func() {
	It("scopes the accessibility tree to the child document, including navigation during a read", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.RawQuery == "replacement" {
				_, _ = response.Write([]byte(`<!doctype html><title>Replacement accessibility</title><button>Replacement action</button>`))
				return
			}
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

		scoped, err := engine.ScopeToFrameContext(engine.SessionContextForTest(frame.Session), frame.FrameID())
		Expect(err).NotTo(HaveOccurred())
		readCtx, cancel := context.WithTimeout(scoped, 3*time.Second)
		DeferCleanup(cancel)
		nodes, err := engine.AccessibilityTreeDuringNavigationForTest(readCtx, frame.FrameID(), func() {
			_, navigateErr := root.Evaluate(ctx, `document.querySelector('#child').src = `+strconv.Quote(child.URL+"?replacement"))
			Expect(navigateErr).NotTo(HaveOccurred())
			replacement, findErr := root.WaitForFrame(ctx, engine.FrameQuery{
				Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "Replacement accessibility"},
			}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
			Expect(findErr).NotTo(HaveOccurred())
			DeferCleanup(replacement.Close)
			replacementOutline, readErr := replacement.AccessibilityOutline(ctx)
			Expect(readErr).NotTo(HaveOccurred())
			Expect(replacementOutline).To(ContainSubstring(`button "Replacement action"`))
		})
		Expect(err).To(HaveOccurred())
		Expect(engine.FrameContextGone(err)).To(BeTrue(), "a frame ID must not allow reading the replacement document")
		Expect(nodes).To(BeNil())
	})
})
