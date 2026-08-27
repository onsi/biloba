package engine_test

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("cross-origin frame targets", func() {
	It("discovers an isolated frame and drives its DOM through a scoped handle", func(ctx SpecContext) {
		isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Arguments:      []string{"--site-per-process"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolatedBrowser.Close)
		root, err := isolatedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())

		frameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/destination"
		_, err = root.Evaluate(ctx, `(() => { const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(frameURL)+`; document.body.append(frame) })()`)
		Expect(err).NotTo(HaveOccurred())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"},
			HasElement: selectorPtr(engine.TestID("destination")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)

		parsed, err := url.Parse(frame.URL())
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Hostname()).To(Equal("localhost"))
		text, err := frame.Text(ctx, engine.TestID("destination"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("arrived"))
		Expect(frame.Close()).To(Succeed())
		stillPresent, err := root.Evaluate(ctx, `document.querySelector("iframe") !== null`)
		Expect(err).NotTo(HaveOccurred())
		Expect(stillPresent).To(BeTrue())
		_, err = frame.Text(ctx, engine.TestID("destination"))
		Expect(err).To(MatchError(ContainSubstring("session is closed")))

		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(sibling.Close)
		Expect(sibling.Navigate(ctx, server.URL)).To(Succeed())
		siblingFrameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/dom-surface"
		_, err = sibling.Evaluate(ctx, `(() => { const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(siblingFrameURL)+`; document.body.append(frame) })()`)
		Expect(err).NotTo(HaveOccurred())
		siblingFrame, err := sibling.WaitForFrame(ctx, engine.FrameQuery{
			URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/dom-surface"},
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		Expect(siblingFrame.Close()).To(Succeed())

		rootFrames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, discovered := range rootFrames {
				_ = discovered.Close()
			}
		})
		Expect(rootFrames).To(HaveLen(1), "a sibling tab's frame must not leak into this session")
		Expect(rootFrames[0].URL()).To(HaveSuffix("/destination"))
	})
})
