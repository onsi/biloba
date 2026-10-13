package engine_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("Session screencasts", func() {
	var session *engine.Session

	BeforeEach(func(ctx SpecContext) {
		fixtures := httptest.NewServer(http.FileServer(http.Dir("../fixtures")))
		DeferCleanup(fixtures.Close)
		var err error
		session, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, fixtures.URL+"/screencast.html")).To(Succeed())
	})

	// Full Chrome can go a few hundred milliseconds without a frame while other tabs are busy, so the
	// specs wait for frames rather than sleeping for a fixed time.
	waitForFrames := func(framesSoFar func() int, n int) {
		GinkgoHelper()
		Eventually(framesSoFar).WithTimeout(5 * time.Second).Should(BeNumerically(">=", n))
	}

	It("records an animating page's frames in timestamp order, and stops for good", func(ctx SpecContext) {
		id, err := session.StartScreencast(ctx, engine.ScreencastParams{Format: "jpeg", Quality: 92})
		Expect(err).NotTo(HaveOccurred())
		framesSoFar := engine.ScreencastFramesSoFarForTest(session, id)
		waitForFrames(framesSoFar, 4)
		frames, err := session.StopScreencast(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		width, height, err := session.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		for i, frame := range frames {
			img, err := jpeg.Decode(bytes.NewReader(frame.Data))
			Expect(err).NotTo(HaveOccurred())
			Expect(img.Bounds().Size()).To(Equal(image.Pt(width, height)))
			Expect(frame.DeviceWidth).To(BeNumerically("==", width))
			if i > 0 {
				Expect(frame.Timestamp).To(BeTemporally(">=", frames[i-1].Timestamp))
			}
		}

		time.Sleep(100 * time.Millisecond)
		Expect(framesSoFar()).To(Equal(len(frames)), "no frame arrives after the stop")
		again, err := session.StopScreencast(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(again).To(Equal(frames))

		session.ReleaseScreencast(id)
		_, err = session.StopScreencast(ctx, id)
		Expect(pointerErrorCode(err)).To(Equal(engine.CodeNotFound))
	})

	It("records one screencast per tab at a time, and validates its options", func(ctx SpecContext) {
		id, err := session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred())
		_, err = session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).To(MatchError(ContainSubstring("this tab is already recording a screencast")))
		Expect(pointerErrorCode(err)).To(Equal(engine.CodeInvalidArgument))

		tab, err := session.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		other, err := tab.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred(), "another tab records on its own")
		_, err = tab.StopScreencast(ctx, other)
		Expect(err).NotTo(HaveOccurred())

		_, err = session.StopScreencast(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		next, err := session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred(), "a stopped recording frees the tab")
		_, err = session.StopScreencast(ctx, next)
		Expect(err).NotTo(HaveOccurred())

		for params, message := range map[engine.ScreencastParams]string{
			{Format: "webp"}:     `format must be "jpeg" or "png", got "webp"`,
			{Quality: 101}:       "quality must be between 0 and 100, got 101",
			{MaxWidth: -1}:       "maxWidth must not be negative, got -1",
			{EveryNthFrame: -2}:  "everyNthFrame must not be negative, got -2",
			{MaxHeight: -3}:      "maxHeight must not be negative, got -3",
			{Format: "png"}:      "",
			{Format: "jpeg"}:     "",
			{MaxWidth: 1}:        "",
			{EveryNthFrame: 100}: "",
		} {
			if message == "" {
				Expect(params.Validate()).To(Succeed())
			} else {
				Expect(params.Validate()).To(MatchError(ContainSubstring(message)))
			}
		}
	})

	It("stops a running recording and discards every recording when the session prepares", func(ctx SpecContext) {
		stopped, err := session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred())
		_, err = session.StopScreencast(ctx, stopped)
		Expect(err).NotTo(HaveOccurred())
		running, err := session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred())
		framesSoFar := engine.ScreencastFramesSoFarForTest(session, running)
		waitForFrames(framesSoFar, 1)

		Expect(session.Prepare(ctx)).To(Succeed())
		recorded := framesSoFar()
		time.Sleep(100 * time.Millisecond)
		Expect(framesSoFar()).To(Equal(recorded), "no frame arrives after prepare")
		for _, id := range []string{stopped, running} {
			_, err = session.StopScreencast(ctx, id)
			Expect(pointerErrorCode(err)).To(Equal(engine.CodeNotFound))
		}
		next, err := session.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred(), "prepare freed the tab to record again")
		_, err = session.StopScreencast(ctx, next)
		Expect(err).NotTo(HaveOccurred())
	})

	It("drops a running recording when its tab closes", func(ctx SpecContext) {
		tab, err := session.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		id, err := tab.StartScreencast(ctx, engine.ScreencastParams{})
		Expect(err).NotTo(HaveOccurred())
		framesSoFar := engine.ScreencastFramesSoFarForTest(tab, id)
		Expect(tab.Close()).To(Succeed())
		recorded := framesSoFar()
		time.Sleep(100 * time.Millisecond)
		Expect(framesSoFar()).To(Equal(recorded))
		_, err = tab.StopScreencast(ctx, id)
		Expect(pointerErrorCode(err)).To(Equal(engine.CodeNotFound))
	})
})
