package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame attachment cancellation", func() {
	It("reports a discovery deadline without closing a busy OOPIF or its owning tab", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<title>busy child</title><script>parent.postMessage('busy-child-starting', '*'); setTimeout(() => { while (true) {} }, 0)</script>`))
		}))
		DeferCleanup(child.Close)

		// The replacement below needs a site of its own. Chrome may put another localhost frame in the
		// busy child's renderer, which is still spinning after the child is removed, so the replacement
		// document would never start.
		isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Arguments:      []string{"--site-per-process", "--host-resolver-rules=MAP replacement.test 127.0.0.1"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolatedBrowser.Close)
		root, err := isolatedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		childURL := strings.Replace(child.URL, "127.0.0.1", "localhost", 1)
		_, err = root.Evaluate(ctx, `window.childStarted = false;
			addEventListener('message', event => { if (event.data === 'busy-child-starting') window.childStarted = true });
			const frame = document.createElement('iframe'); frame.src = `+strconv.Quote(childURL)+`; document.body.append(frame)`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() any {
			value, evaluateErr := root.Evaluate(ctx, `window.childStarted`)
			Expect(evaluateErr).NotTo(HaveOccurred())
			return value
		}).WithTimeout(2 * time.Second).Should(BeTrue())

		// Repeated discovery must clean up failed attachments, not kill the renderer to unblock
		// them. The second attempt would find no child at all if the first closed its target.
		for range 2 {
			discoveryCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_, discoverErr := root.Frames(discoveryCtx)
			cancel()
			Expect(errors.Is(discoverErr, context.DeadlineExceeded)).To(BeTrue(), "%v", discoverErr)
			_, evaluateErr := root.Evaluate(ctx, `document.body.dataset.parentStillUsable = 'yes'`)
			Expect(evaluateErr).NotTo(HaveOccurred())
		}

		// Removing the busy document and discovering a replacement also verifies that a canceled
		// attachment was not published as a permanently poisoned renderer connection.
		_, err = root.Evaluate(ctx, `document.querySelector('iframe').remove();
			const replacement = document.createElement('iframe'); replacement.src = `+strconv.Quote(strings.Replace(server.URL, "127.0.0.1", "replacement.test", 1)+"/destination")+`; document.body.append(replacement)`)
		Expect(err).NotTo(HaveOccurred())
		replacement, err := root.WaitForFrame(ctx, engine.FrameQuery{
			HasElement: selectorPtr(engine.TestID("destination")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(replacement.Close)
		text, err := replacement.Text(ctx, engine.TestID("destination"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("arrived"))
	})
})
