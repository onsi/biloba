package engine_test

import (
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

var _ = Describe("frame discovery polling", func() {
	It("returns an invalid frame element selector on the first attempt", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>candidate</title><div>ready</div>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe></iframe>'; document.querySelector('iframe').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())
		ready, err := root.WaitForFrame(ctx, engine.FrameQuery{
			URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"},
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		Expect(ready.Close()).To(Succeed())

		invalid := engine.CSS("[")
		_, err = root.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"},
			HasElement: &invalid,
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engine.IsFatal(err)).To(BeTrue())
		Expect(engineErr.AttemptCount).To(Equal(1))
		Expect(err).To(MatchError(ContainSubstring("SyntaxError")))
	})

	It("reacquires a frame whose document is replaced while its predicate is pending", func(ctx SpecContext) {
		readyURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/destination"
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>replacing</title><script>setTimeout(() => location.replace(` + strconv.Quote(readyURL) + `), 25)</script>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe></iframe>'; document.querySelector('iframe').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())

		destination := engine.TestID("destination")
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{HasElement: &destination}, engine.PollPolicy{
			Timeout:  2 * time.Second,
			Interval: time.Millisecond,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.URL()).To(Equal(readyURL))
		text, err := frame.Text(ctx, destination)
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("arrived"))
	})
})
