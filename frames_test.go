package biloba_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/biloba"
	"github.com/onsi/biloba/engine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The frame is served by crossOriginFixtureServer - another port on the same host - so it is a
// different origin on the same site.  Chrome runs it in the tab's renderer, which is the case frame
// handles exist for: there is no separate target to attach to, and the page that embeds it cannot
// read it.
var _ = Describe("Cross-origin iframes", func() {
	var frameURL string
	var checkout *biloba.Biloba

	BeforeEach(func() {
		frameURL = crossOriginFixtureServer + "/frame-form.html"
		b.Navigate(fixtureServer + "/frames.html?child=" + url.QueryEscape(frameURL))
		Eventually("#form-frame").Should(b.Exist())
		checkout = b.Frame(b.FrameMatching().WithURL(frameURL).WithDOMElement("#email"))
		Ω(checkout).ShouldNot(BeNil())
	})

	Describe("finding frames", func() {
		It("is out of reach of >>> from the embedding page", func() {
			Ω("#form-frame >>> #email").ShouldNot(b.Exist())
		})

		It("returns a frame handle scoped to the frame's document", func() {
			Ω(checkout.IsFrame()).Should(BeTrue())
			Ω(b.IsFrame()).Should(BeFalse())
			Ω(checkout.GetLocation()).Should(Equal(frameURL))
			Ω(checkout.GetTitle()).Should(Equal("Checkout Form"))
			Ω(b.GetTitle()).Should(Equal("Frames Parent"))
			Ω(checkout.HasElement("#parent-only")).Should(BeFalse())
			Ω(b.HasElement("#email")).Should(BeFalse())
		})

		It("matches on URL, title, and DOM element, and polls with HaveFrame", func() {
			Eventually(b).Should(b.HaveFrame().WithTitle("Checkout Form"))
			Eventually(b).Should(b.HaveFrame().WithURL(ContainSubstring("frame-form")).WithDOMElement("#pay"))
			Consistently(b, 100*time.Millisecond).ShouldNot(b.HaveFrame().WithTitle("Nope"))
			Ω(b.AllFrames()).Should(HaveLen(1))
			Ω(b.AllFrames().Find(b.FrameMatching().WithTitle("Checkout Form"))).Should(BeIdenticalTo(checkout), "a frame document has one handle")
			Ω(b.AllFrames().Filter(b.FrameMatching().WithTitle("Nope"))).Should(BeEmpty())
		})

		It("fails with the frames it searched when none matches in time", func() {
			Ω(b.WithTimeout(200 * time.Millisecond).Frame(b.FrameMatching().WithTitle("Nope"))).Should(BeNil())
			ExpectFailures(SatisfyAll(
				ContainSubstring("Expected to have a cross-origin frame with Title matching"),
				ContainSubstring(frameURL+" (Checkout Form)"),
			))
		})

		It("does not count iframes that share the page's origin, and fails immediately under Immediate()", func() {
			// an iframe with no src, or a srcdoc, inherits the page's origin: >>> reaches it
			b.Navigate(fixtureServer + "/frames.html")
			b.Run(`document.body.insertAdjacentHTML("beforeend", '<iframe id="inline" srcdoc="<p id=inline-p>inline</p>"></iframe>')`)
			Eventually("#inline >>> #inline-p").Should(b.Exist())
			Ω(b.AllFrames()).Should(BeEmpty())
			Ω(b.Immediate().Frame(b.FrameMatching())).Should(BeNil())
			ExpectFailures(ContainSubstring("There were no cross-origin frames to search."))
		})

		It("finds a cross-site frame, which Biloba's Chrome also runs in the tab's process", func() {
			crossSite := strings.Replace(crossOriginFixtureServer, "127.0.0.1", "localhost", 1) + "/frame-form.html"
			b.Run(`document.querySelector("#form-frame").src = "` + crossSite + `"`)
			frame := b.Frame(b.FrameMatching().WithURL(crossSite).WithDOMElement("#email"))
			frame.SetValue("#email", "ada@example.com")
			frame.Click("#pay")
			Eventually("#result").Should(frame.HaveInnerText("Paid by ada@example.com"))
		})

		It("finds frames nested inside a frame", func() {
			checkout.Run(`const nested = document.createElement("iframe"); nested.src = ` + "`" + fixtureServer + "/iframe-content.html`" + `; document.body.append(nested)`)
			nested := checkout.Frame(b.FrameMatching().WithURL(ContainSubstring("iframe-content")).WithDOMElement("#iframe-btn"))
			Ω(nested).ShouldNot(BeNil())
			Ω(nested.HasElement("#email")).Should(BeFalse())
			Eventually(b).Should(b.HaveFrame().WithURL(ContainSubstring("iframe-content")), "a tab sees every frame nested behind a cross-origin boundary")
			Ω(checkout.AllFrames()).Should(HaveLen(1))
		})
	})

	Describe("driving the frame", func() {
		It("runs actions, matchers, and getters in the frame's document", func() {
			checkout.SetValue("#email", "ada@example.com")
			Eventually("#email").Should(checkout.HaveValue("ada@example.com"))
			checkout.Click("#pay")
			Eventually("#result").Should(checkout.HaveInnerText("Paid by ada@example.com"))
			Ω(checkout.GetInnerText("#result")).Should(Equal("Paid by ada@example.com"))

			var result string
			Eventually("#result").Should(checkout.HaveInnerText(ContainSubstring("Paid")).Capture(&result))
			Ω(result).Should(Equal("Paid by ada@example.com"))
			Ω(b.GetProperty("#parent-button", "dataset")).ShouldNot(HaveKey("clicked"))
		})

		It("runs JavaScript in the frame's own environment", func() {
			Ω(checkout.Run("window.frameState.loaded")).Should(BeTrue())
			Ω(b.Run("typeof window.frameState")).Should(Equal("undefined"))
			Ω(checkout.Run(`(() => { try { void parent.document.body; return "allowed" } catch (e) { return e.name } })()`)).Should(Equal("SecurityError"), "the same-origin policy still applies")
			Ω(checkout.GetJSValue("window.frameState.loaded")).Should(BeTrue())
		})

		It("types and uploads into the frame", func() {
			checkout.Type("#email", "grace")
			Eventually("#email").Should(checkout.HaveValue("grace"))
			checkout.SetUpload("#upload", "fixtures/upload-sample.txt")
			Ω(checkout.Run(`document.querySelector("#upload").files[0].name`)).Should(Equal("upload-sample.txt"))
		})

		It("records the requests the frame makes once it has been found", func() {
			b.Run(`fetch("/api/from-parent")`)
			checkout.Run(`fetch("/api/from-frame")`)
			Eventually(checkout).Should(checkout.HaveMadeRequest(crossOriginFixtureServer + "/api/from-frame"))
			Eventually(checkout).Should(checkout.BeNetworkIdle())
			Eventually(b).Should(b.HaveMadeRequest(fixtureServer + "/api/from-parent"))
			Ω(checkout.AllRequests().Filter(b.RequestMatching(fixtureServer + "/api/from-parent"))).Should(BeEmpty())
			Ω(b.AllRequests().Filter(b.RequestMatching(crossOriginFixtureServer+"/api/frame-ready"))).ShouldNot(BeEmpty(), "the tab sees its frames' requests too")
		})

		It("outlines the frame's document and accessibility tree", func() {
			Ω(checkout.Outline()).Should(SatisfyAll(ContainSubstring("email"), Not(ContainSubstring("parent-only"))))
			Ω(checkout.A11yOutline()).Should(SatisfyAll(ContainSubstring(`button "Pay"`), Not(ContainSubstring(`"Parent"`))))
		})
	})

	Describe("realistic input", func() {
		It("lands real pointer and keyboard input in the frame", func() {
			rb := checkout.Realistic()
			rb.SetValue("#email", "real@example.com")
			rb.Click("#pay")
			Eventually("#result").Should(checkout.HaveInnerText("Paid by real@example.com"))
			rb.Hover("#hover-target")
			Eventually("#hover-target").Should(checkout.HaveComputedStyle("background-color", "rgb(0, 0, 255)"))
			Ω(b.GetProperty("#parent-button", "dataset")).ShouldNot(HaveKey("clicked"))
		})

		It("refuses to click through something in the embedding page that covers the frame", func() {
			b.Run(`document.querySelector("#overlay").style.display = "block"`)
			checkout.Realistic().WithTimeout(300 * time.Millisecond).Click("#pay")
			ExpectFailures(ContainSubstring("#pay"))
			Ω(b.GetProperty("#overlay", "dataset")).ShouldNot(HaveKey("clicked"))
			Ω(checkout.GetInnerText("#result")).Should(BeEmpty())

			b.Run(`document.querySelector("#overlay").style.display = "none"`)
			checkout.Realistic().Click("#pay")
			Eventually("#result").Should(checkout.HaveInnerText(HavePrefix("Paid by")))
		})
	})

	Describe("screenshots", func() {
		decode := func(raw []byte) image.Image {
			GinkgoHelper()
			img, err := png.Decode(bytes.NewReader(raw))
			Ω(err).ShouldNot(HaveOccurred())
			return img
		}
		rgb := func(img image.Image, x, y int) []uint32 {
			r, g, b, _ := img.At(x, y).RGBA()
			return []uint32{r >> 8, g >> 8, b >> 8}
		}

		It("captures the frame's viewport as its page screenshot", func() {
			img := decode(checkout.CaptureScreenshot())
			Ω(img.Bounds().Dx()).Should(BeNumerically("~", 360, 1))
			Ω(img.Bounds().Dy()).Should(BeNumerically("~", 260, 1))
			Ω(rgb(img, 2, img.Bounds().Dy()-3)).Should(Equal([]uint32{0, 170, 0}), "the frame's background, not the embedding page's")
		})

		It("captures an element inside the frame at the frame's position in the tab", func() {
			img := decode(checkout.CaptureScreenshotOf("#pay"))
			Ω(img.Bounds().Dx()).Should(BeNumerically("~", 120, 1))
			Ω(img.Bounds().Dy()).Should(BeNumerically("~", 40, 1))
		})

		It("compares a frame against a baseline, with masks measured in the frame", func() {
			root := GinkgoT().TempDir()
			baselinesDir := filepath.Join(root, "baselines")
			DeferCleanup(b.SetVisualDirsForTest(baselinesDir, filepath.Join(root, "artifacts")))
			func() {
				defer b.SetUpdateScreenshotsForTest(true)()
				Eventually(checkout).Should(checkout.HaveScreenshot("checkout", checkout.Mask("#email")))
			}()
			raw, err := os.ReadFile(filepath.Join(baselinesDir, "checkout.png"))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(decode(raw).Bounds().Dx()).Should(BeNumerically("~", 360, 1))

			// Set the value without focusing: focus moves into the frame and restyles its form controls,
			// which is a real change outside the mask.
			checkout.Run(`document.querySelector("#email").value = "masked out"`)
			Eventually(checkout).Should(checkout.HaveScreenshot("checkout", checkout.Mask("#email")), "a change inside the mask does not count")
		})
	})

	Describe("the frame's tab", func() {
		It("refuses tab-level methods on a frame handle", func() {
			checkout.Navigate(crossOriginFixtureServer + "/dom.html")
			checkout.SetWindowSize(400, 300)
			checkout.StubRequest("/api/anything", biloba.StubResponse{Body: "stubbed"})
			checkout.HandleAlertDialogs()
			checkout.SetCookie(biloba.Cookie{Name: "frame", Value: "cookie"})
			checkout.AllDownloads()
			ExpectFailures(
				"Navigate acts on the tab, not a frame: call it on the tab that owns this frame",
				"SetWindowSize acts on the tab, not a frame: call it on the tab that owns this frame",
				"StubRequest acts on the tab, not a frame: call it on the tab that owns this frame",
				"HandleAlertDialogs acts on the tab, not a frame: call it on the tab that owns this frame",
				"SetCookie acts on the tab, not a frame: call it on the tab that owns this frame",
				"AllDownloads acts on the tab, not a frame: call it on the tab that owns this frame",
			)
			Ω(b.GetTitle()).Should(Equal("Frames Parent"), "the tab must not have navigated")
		})

		It("covers the frame with the tab's network stubs and dialog handling", func() {
			b.StubRequest(ContainSubstring("/api/stubbed"), biloba.StubResponse{Body: "stubbed-body"})
			Ω(checkout.RunAsync(`return await (await fetch("/api/stubbed")).text()`)).Should(Equal("stubbed-body"))
			b.HandleConfirmDialogs().WithResponse(true)
			checkout.Run(`setTimeout(() => { document.querySelector("#result").textContent = confirm("Sure?") ? "confirmed" : "declined" }, 0)`)
			Eventually("#result").Should(checkout.HaveInnerText("confirmed"))
			Ω(b.Dialogs().MostRecent().Message).Should(Equal("Sure?"))
		})

		It("reports its tab's window", func() {
			width, height := b.WindowSize()
			frameWidth, frameHeight := checkout.WindowSize()
			Ω([]int{frameWidth, frameHeight}).Should(Equal([]int{width, height}))
		})
	})

	Describe("frame documents", func() {
		It("observes requests and network idle while the frame's JavaScript thread is busy", func() {
			started := make(chan struct{}, 1)
			signal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
				w.(http.Flusher).Flush()
				started <- struct{}{}
			}))
			DeferCleanup(signal.Close)

			// The server signal proves the scheduled callback has begun before either observation. The
			// loop is bounded so a failed assertion cannot leave this shared renderer wedged.
			checkout.Run(`setTimeout(() => {
				const signal = new Image();
				signal.src = "` + signal.URL + `/busy-start";
				const until = performance.now() + 1500;
				while (performance.now() < until) {}
			}, 0)`)
			Eventually(started).WithTimeout(2 * time.Second).Should(Receive())

			observationStarted := time.Now()
			Eventually(checkout).WithTimeout(500 * time.Millisecond).WithPolling(10 * time.Millisecond).
				Should(checkout.HaveMadeRequest(ContainSubstring("/busy-start")))
			Eventually(checkout).WithTimeout(500 * time.Millisecond).WithPolling(10 * time.Millisecond).
				Should(checkout.BeNetworkIdle())
			Ω(time.Since(observationStarted)).Should(BeNumerically("<", time.Second),
				"cached network observations must not wait for the renderer's busy loop")
		})

		It("fails a handle whose document has gone, and finds the replacement", func() {
			b.Run(`document.querySelector("#form-frame").src = "` + crossOriginFixtureServer + `/frame-form.html?replaced"`)
			replacement := b.Frame(b.FrameMatching().WithURL(ContainSubstring("?replaced")).WithDOMElement("#email"))
			Ω(replacement).ShouldNot(BeIdenticalTo(checkout))
			replacement.SetValue("#email", "new@example.com")

			checkout.Immediate().SetValue("#email", "old@example.com")
			ExpectFailures(ContainSubstring("frame_detached"))
			Ω(replacement.GetValue("#email")).Should(Equal("new@example.com"))
		})

		It("does not let a stale handle observe the replacement document", func() {
			checkout.Run(`fetch("/api/original-only")`)
			Eventually(checkout).Should(checkout.HaveMadeRequest(ContainSubstring("/api/original-only")))

			b.Run(`document.querySelector("#form-frame").src = "` + crossOriginFixtureServer + `/frame-form.html?replacement"`)
			replacement := b.Frame(b.FrameMatching().WithURL(ContainSubstring("?replacement")).WithDOMElement("#email"))
			replacement.Run(`document.body.insertAdjacentHTML("beforeend", '<h1>Replacement only</h1><iframe src="` + fixtureServer + `/iframe-content.html"></iframe>')`)
			Eventually(replacement).Should(replacement.HaveFrame().WithDOMElement("#iframe-btn"))

			Ω(checkout.AllFrames()).Should(BeEmpty())
			Ω(checkout.Immediate().Frame(checkout.FrameMatching())).Should(BeNil())
			Ω(checkout.A11yOutline()).Should(BeEmpty())
			ExpectFailures(
				ContainSubstring("frame_detached"),
				ContainSubstring("frame_detached"),
				ContainSubstring("frame_detached"),
			)

			matched, err := checkout.HaveFrame().Match(checkout)
			Ω(matched).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("frame_detached")))
			stop, ok := err.(interface{ IsStopTrying() bool })
			Ω(ok).Should(BeTrue())
			Ω(stop.IsStopTrying()).Should(BeTrue(), "HaveFrame must stop polling a stale document")

			replacement.Run(`fetch("/api/replacement-only")`)
			Eventually(replacement).Should(replacement.HaveMadeRequest(ContainSubstring("/api/replacement-only")))
			originalRequests := checkout.AllRequests()
			Ω(originalRequests.Filter(checkout.RequestMatching(ContainSubstring("/api/original-only")))).Should(HaveLen(1))
			Ω(originalRequests.Filter(checkout.RequestMatching(ContainSubstring("/api/replacement-only")))).Should(BeEmpty())

			matched, err = checkout.HaveMadeRequest(ContainSubstring("/api/replacement-only")).Match(checkout)
			Ω(matched).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("frame_detached")))
			stop, ok = err.(interface{ IsStopTrying() bool })
			Ω(ok).Should(BeTrue())
			Ω(stop.IsStopTrying()).Should(BeTrue(), "HaveMadeRequest must stop polling a stale document")
			matched, err = checkout.BeNetworkIdle().Match(checkout)
			Ω(matched).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("frame_detached")))
			stop, ok = err.(interface{ IsStopTrying() bool })
			Ω(ok).Should(BeTrue())
			Ω(stop.IsStopTrying()).Should(BeTrue(), "BeNetworkIdle must stop polling a stale document")
		})

		It("fails discovery through a closed handle", func() {
			checkout.Close()
			Ω(checkout.AllFrames()).Should(BeEmpty())
			Ω(checkout.Immediate().Frame(checkout.FrameMatching())).Should(BeNil())
			ExpectFailures(ContainSubstring("frame_detached"), ContainSubstring("frame_detached"))

			matched, err := checkout.HaveFrame().Match(checkout)
			Ω(matched).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("frame_detached")))
		})

		It("does not carry handles across Prepare", func() {
			b.Prepare()
			Ω(b.AllFrames()).Should(BeEmpty())
			checkout.Immediate().GetInnerText("#result")
			ExpectFailures(Not(BeEmpty()))
		})
	})
})

// Chrome's site isolation can put a cross-site frame in a renderer target of its own.  Whether it does
// under Biloba's launch depends on the build (chrome-headless-shell keeps cross-site frames in the
// tab's process; full Chrome gives them their own), so these specs start a Chrome with it forced on.
var _ = Describe("Out-of-process iframes", func() {
	var tab, frame *biloba.Biloba
	var frameURL string

	BeforeEach(func(ctx SpecContext) {
		chrome, _, err := engine.ResolveHeadlessShell(ctx, "", true)
		Ω(err).ShouldNot(HaveOccurred())
		// not ctx: the browser has to outlive this BeforeEach, and a SpecContext ends with its node
		isolated, err := engine.StartBrowser(context.Background(), engine.BrowserConfig{ExecutablePath: chrome, Arguments: []string{"--site-per-process"}})
		Ω(err).ShouldNot(HaveOccurred())
		DeferCleanup(isolated.Close)
		tab = biloba.ConnectToChrome(gt, biloba.BilobaConfigWithChromeConnection(biloba.ChromeConnection{WebSocketURL: isolated.WebSocketURL()}))
		Ω(tab).ShouldNot(BeNil())

		// localhost and 127.0.0.1 are different sites
		frameURL = strings.Replace(crossOriginFixtureServer, "127.0.0.1", "localhost", 1) + "/frame-form.html"
		tab.Navigate(fixtureServer + "/frames.html?child=" + url.QueryEscape(frameURL))
		frame = tab.Frame(tab.FrameMatching().WithURL(frameURL).WithDOMElement("#email"))
		Ω(frame).ShouldNot(BeNil())
		Ω(frame.FrameOutOfProcessForTest()).Should(BeTrue(), "the fixture must exercise an out-of-process frame")
	})

	It("drives the frame's document, including real input", func() {
		Ω(frame.GetLocation()).Should(Equal(frameURL))
		Ω(frame.Run("window.frameState.loaded")).Should(BeTrue())
		frame.SetValue("#email", "ada@example.com")
		frame.Click("#pay")
		Eventually("#result").Should(frame.HaveInnerText("Paid by ada@example.com"))

		rf := frame.Realistic()
		rf.SetValue("#email", "real@example.com")
		rf.Click("#pay")
		Eventually("#result").Should(frame.HaveInnerText("Paid by real@example.com"))
		rf.Hover("#hover-target")
		Eventually("#hover-target").Should(frame.HaveComputedStyle("background-color", "rgb(0, 0, 255)"))

		frame.SetUpload("#upload", "fixtures/upload-sample.txt")
		Ω(frame.Run(`document.querySelector("#upload").files[0].name`)).Should(Equal("upload-sample.txt"))
		Ω(frame.A11yOutline()).Should(ContainSubstring(`button "Pay"`))
	})

	It("records the frame's requests, and leaves them outside the tab's network stubs", func() {
		tab.StubRequest(ContainSubstring("/api/stubbed"), biloba.StubResponse{Body: "stubbed-body"})
		Ω(frame.RunAsync(`return await (await fetch("/api/stubbed")).text()`)).ShouldNot(Equal("stubbed-body"))
		Eventually(frame).Should(frame.HaveMadeRequest(ContainSubstring("/api/stubbed")))
	})

	It("has its dialogs handled by the tab", func() {
		tab.HandleConfirmDialogs().WithResponse(true)
		frame.Run(`setTimeout(() => { document.querySelector("#result").textContent = confirm("Sure?") ? "confirmed" : "declined" }, 0)`)
		Eventually("#result").Should(frame.HaveInnerText("confirmed"))
	})

	It("refuses a screenshot of the frame and points at the iframe element instead", func() {
		frame.CaptureScreenshot()
		frame.CaptureScreenshotOf("#pay")
		ExpectFailures(
			ContainSubstring("capture the iframe element from the tab instead"),
			ContainSubstring("capture the iframe element from the tab instead"),
		)
		Ω(tab.CaptureScreenshotOf("#form-frame")).ShouldNot(BeEmpty())
	})

	It("fails a handle whose document has gone, and finds the replacement", func() {
		tab.Run(`document.querySelector("#form-frame").src = "` + frameURL + `?replaced"`)
		replacement := tab.Frame(tab.FrameMatching().WithURL(frameURL + "?replaced").WithDOMElement("#email"))
		Ω(replacement).ShouldNot(BeNil())
		replacement.SetValue("#email", "new@example.com")
		frame.Immediate().SetValue("#email", "old@example.com")
		ExpectFailures(ContainSubstring("frame_detached"))
	})

	It("does not let a stale handle observe an out-of-process replacement", func() {
		frame.Run(`fetch("/api/original-only")`)
		Eventually(frame).Should(frame.HaveMadeRequest(ContainSubstring("/api/original-only")))

		tab.Run(`document.querySelector("#form-frame").src = "` + frameURL + `?replacement"`)
		replacement := tab.Frame(tab.FrameMatching().WithURL(frameURL + "?replacement").WithDOMElement("#email"))
		replacement.Run(`document.body.insertAdjacentHTML("beforeend", '<h1>Replacement only</h1><iframe src="` + fixtureServer + `/iframe-content.html"></iframe>')`)
		Eventually(replacement).Should(replacement.HaveFrame().WithDOMElement("#iframe-btn"))
		replacement.Run(`fetch("/api/replacement-only")`)
		Eventually(replacement).Should(replacement.HaveMadeRequest(ContainSubstring("/api/replacement-only")))

		Ω(frame.AllFrames()).Should(BeEmpty())
		Ω(frame.A11yOutline()).Should(BeEmpty())
		ExpectFailures(ContainSubstring("frame_detached"), ContainSubstring("frame_detached"))

		originalRequests := frame.AllRequests()
		Ω(originalRequests.Filter(frame.RequestMatching(ContainSubstring("/api/original-only")))).Should(HaveLen(1))
		Ω(originalRequests.Filter(frame.RequestMatching(ContainSubstring("/api/replacement-only")))).Should(BeEmpty())

		matched, err := frame.HaveMadeRequest(ContainSubstring("/api/replacement-only")).Match(frame)
		Ω(matched).Should(BeFalse())
		Ω(err).Should(MatchError(ContainSubstring("frame_detached")))
		matched, err = frame.BeNetworkIdle().Match(frame)
		Ω(matched).Should(BeFalse())
		Ω(err).Should(MatchError(ContainSubstring("frame_detached")))

		replacement.Close()
		Ω(replacement.AllFrames()).Should(BeEmpty())
		ExpectFailures(ContainSubstring("frame_detached"))
	})
})
