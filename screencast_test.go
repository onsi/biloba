package biloba_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Screencasts", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/screencast.html")
		Eventually("#repaint").Should(b.Exist())
	})

	// Full Chrome can go a few hundred milliseconds without a frame while other tabs in the browser
	// are busy, so the specs wait for frames rather than sleeping for a fixed time.
	waitForFrames := func(rec *biloba.ScreencastRecording, n int) {
		GinkgoHelper()
		Eventually(func() int { return len(rec.FramesSoFarForTest()) }).WithTimeout(5 * time.Second).Should(BeNumerically(">=", n))
	}
	pauseSpinner := func() {
		b.Run(`document.querySelector("#spinner").classList.add("paused")`)
		b.RunAsync(`await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))`)
	}
	decodePNG := func(frame biloba.ScreencastFrame) image.Image {
		GinkgoHelper()
		img, err := png.Decode(bytes.NewReader(frame.Data))
		Ω(err).ShouldNot(HaveOccurred())
		return img
	}
	// recordPNG returns the last frame: with an emulated device scale factor, full Chrome can send the
	// first frame or two at the window's size before it applies MaxWidth and MaxHeight.
	recordPNG := func(options biloba.ScreencastOptions) image.Image {
		GinkgoHelper()
		options.Format = "png"
		rec := b.StartScreencast(options)
		waitForFrames(rec, 4)
		frames := rec.Stop()
		return decodePNG(frames[len(frames)-1])
	}

	It("records an animating page's frames, timestamped by Chrome, in order", func() {
		before := time.Now()
		rec := b.StartScreencast(biloba.ScreencastOptions{Format: "jpeg", Quality: 92})
		waitForFrames(rec, 6)
		frames := rec.Stop()
		after := time.Now()

		width, height := b.WindowSize()
		for i, frame := range frames {
			img, err := jpeg.Decode(bytes.NewReader(frame.Data))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(img.Bounds().Size()).Should(Equal(image.Pt(width, height)))
			Ω(frame.DeviceWidth).Should(BeNumerically("==", width))
			Ω(frame.DeviceHeight).Should(BeNumerically("==", height))
			Ω(frame.PageScaleFactor).Should(BeNumerically("==", 1))
			Ω(frame.ScrollOffsetY).Should(BeZero())
			Ω(frame.Timestamp).Should(BeTemporally("<=", after))
			if i > 0 {
				// The first frame is the one already on screen, so it can predate StartScreencast.
				Ω(frame.Timestamp).Should(BeTemporally(">=", before))
				Ω(frame.Timestamp).Should(BeTemporally(">=", frames[i-1].Timestamp))
			}
		}
	})

	It("timestamps each frame when it was drawn", func() {
		if b.ChromeConnection.HighFidelity {
			Skip("full Chrome can timestamp a frame hundreds of milliseconds after it was drawn - see the docs")
		}
		// The fixture moves a marker to x = Date.now() % 1000 on every animation frame, so each frame
		// shows the wall-clock millisecond it was drawn at.
		b.SetWindowSize(1100, 200)
		b.Navigate(fixtureServer + "/screencast_clock.html")
		Eventually("#marker").Should(b.Exist())
		rec := b.StartScreencast(biloba.ScreencastOptions{Format: "png"})
		waitForFrames(rec, 6)
		for _, frame := range rec.Stop() {
			img := decodePNG(frame)
			drawnAt := -1
			for x := 0; x < 1004 && drawnAt == -1; x++ {
				if c := color.RGBAModel.Convert(img.At(x, 10)).(color.RGBA); c.R > 192 && c.G < 64 {
					drawnAt = x
				}
			}
			Ω(drawnAt).ShouldNot(Equal(-1), "the marker is in the frame")
			lag := (int(frame.Timestamp.UnixMilli()%1000) - drawnAt + 1000) % 1000
			Ω(lag).Should(BeNumerically("<", 250), "the frame is timestamped shortly after it was drawn")
		}
	})

	It("records frames at the window's size, shrunk to fit MaxWidth and MaxHeight", func() {
		b.SetWindowSize(400, 300, b.DeviceScaleFactor(2))
		Ω(recordPNG(biloba.ScreencastOptions{}).Bounds().Size()).Should(Equal(image.Pt(400, 300)), "an emulated device scale factor does not enlarge the frames")
		Ω(recordPNG(biloba.ScreencastOptions{MaxWidth: 200}).Bounds().Size()).Should(Equal(image.Pt(200, 150)))
		Ω(recordPNG(biloba.ScreencastOptions{MaxWidth: 300, MaxHeight: 150}).Bounds().Size()).Should(Equal(image.Pt(200, 150)))
	})

	It("sends a static page's current frame and then nothing until it changes", func() {
		pauseSpinner()
		rec := b.StartScreencast(biloba.ScreencastOptions{Format: "png"})
		waitForFrames(rec, 1)
		Consistently(func() int { return len(rec.FramesSoFarForTest()) }, 200*time.Millisecond).Should(Equal(1))

		b.Click("#repaint")
		Eventually(func() color.Color {
			frames := rec.FramesSoFarForTest()
			return color.RGBAModel.Convert(decodePNG(frames[len(frames)-1]).At(50, 90))
		}).WithTimeout(5*time.Second).Should(Equal(color.RGBA{0, 128, 0, 255}), "a frame shows the repaint")
		frames := rec.Stop()
		Ω(color.RGBAModel.Convert(decodePNG(frames[len(frames)-1]).At(50, 90))).Should(Equal(color.RGBA{0, 128, 0, 255}))
	})

	It("delivers no frame after Stop, and Stop returns the same frames again", func() {
		rec := b.StartScreencast(biloba.ScreencastOptions{})
		waitForFrames(rec, 2)
		frames := rec.Stop()
		time.Sleep(100 * time.Millisecond)
		Ω(rec.FramesSoFarForTest()).Should(HaveLen(len(frames)))
		Ω(rec.Stop()).Should(Equal(frames))
	})

	Describe("a recording the spec forgot to stop", Ordered, func() {
		var forgotten *biloba.ScreencastRecording
		It("is left running", func() {
			forgotten = b.StartScreencast(biloba.ScreencastOptions{})
			waitForFrames(forgotten, 1)
		})

		It("was stopped when the spec ended", func() {
			// Prepare does not run between the specs of an Ordered container, so only StartScreencast's
			// cleanup can have freed the tab to record again.
			b.StartScreencast(biloba.ScreencastOptions{}).Stop()
			recorded := len(forgotten.FramesSoFarForTest())
			time.Sleep(100 * time.Millisecond)
			Ω(forgotten.FramesSoFarForTest()).Should(HaveLen(recorded), "the spinner is still spinning, but no frames arrive")
		})
	})

	It("records one screencast per tab at a time", func() {
		rec := b.StartScreencast(biloba.ScreencastOptions{})
		Ω(b.StartScreencast(biloba.ScreencastOptions{})).Should(BeNil())
		ExpectFailures("StartScreencast: this tab is already recording a screencast - Stop it before starting another")

		tab := b.NewTab()
		tab.Navigate(fixtureServer + "/screencast.html")
		other := tab.StartScreencast(biloba.ScreencastOptions{})
		waitForFrames(other, 1)
		Ω(other.Stop()).ShouldNot(BeEmpty())
		rec.Stop()
	})

	It("returns what it recorded when the tab closed underneath it", func() {
		tab := b.NewTab()
		tab.Navigate(fixtureServer + "/screencast.html")
		rec := tab.StartScreencast(biloba.ScreencastOptions{})
		waitForFrames(rec, 1)
		Ω(tab.Close()).Should(Succeed())
		Ω(rec.Stop()).ShouldNot(BeEmpty())
	})

	It("rejects invalid options and the poll-config knobs", func() {
		Ω(b.StartScreencast(biloba.ScreencastOptions{Format: "webp"})).Should(BeNil())
		Ω(b.StartScreencast(biloba.ScreencastOptions{Quality: 101})).Should(BeNil())
		Ω(b.StartScreencast(biloba.ScreencastOptions{MaxWidth: -1})).Should(BeNil())
		ExpectFailures(
			`StartScreencast: ScreencastOptions.Format must be "jpeg" or "png", got "webp"`,
			"StartScreencast: ScreencastOptions.Quality must be between 0 and 100, got 101",
			"StartScreencast: ScreencastOptions.MaxWidth must not be negative, got -1",
		)

		b.WithTimeout(time.Second).StartScreencast(biloba.ScreencastOptions{}).Stop()
		ExpectFailures("StartScreencast does not support WithTimeout")
	})
})
