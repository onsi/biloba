package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	"github.com/onsi/biloba/engine"
)

// pointerEvent is one entry of fixtures/pointer.html's window.pointerLog
type pointerEvent struct {
	Type        string  `json:"type"`
	PointerType string  `json:"pointerType"`
	Buttons     int     `json:"buttons"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Pressure    float64 `json:"pressure"`
}

func readPointerLog(ctx context.Context, session *engine.Session) []pointerEvent {
	GinkgoHelper()
	value, err := session.Evaluate(ctx, `window.pointerLog`)
	Expect(err).NotTo(HaveOccurred())
	encoded, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred())
	var log []pointerEvent
	Expect(json.Unmarshal(encoded, &log)).To(Succeed())
	return log
}

// settleEnginePointerLog empties the log once the page has rendered two frames: on Linux,
// chrome-headless-shell moves the mouse to (0, 0) when a page from a new site renders its first
// frame, and the log would otherwise open with a pointermove nobody sent.  See settlePointerLog in
// pointer_test.go at the repository root.
func settleEnginePointerLog(ctx context.Context, session *engine.Session) {
	GinkgoHelper()
	Eventually(func() (any, error) { return session.Evaluate(ctx, `window.framesRendered === true`) }).WithTimeout(5 * time.Second).Should(BeTrue())
	_, err := session.Evaluate(ctx, `window.pointerLog = []`)
	Expect(err).NotTo(HaveOccurred())
}

func pointerEventAt(typ, pointerType string, buttons int, x, y float64) OmegaMatcher {
	return MatchFields(IgnoreExtras, Fields{
		"Type":        Equal(typ),
		"PointerType": Equal(pointerType),
		"Buttons":     Equal(buttons),
		"X":           BeNumerically("~", x, 1),
		"Y":           BeNumerically("~", y, 1),
	})
}

func pointerErrorCode(err error) engine.ErrorCode {
	var engineErr *engine.Error
	if errors.As(err, &engineErr) {
		return engineErr.Code
	}
	return ""
}

var _ = Describe("Session pointers", func() {
	var session *engine.Session
	var fixtures *httptest.Server
	var released atomic.Int64

	BeforeEach(func(ctx SpecContext) {
		released.Store(0)
		files := http.FileServer(http.Dir("../fixtures"))
		fixtures = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/released" {
				released.Add(1)
				return
			}
			files.ServeHTTP(response, request)
		}))
		DeferCleanup(fixtures.Close)
		var err error
		session, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, fixtures.URL+"/pointer.html")).To(Succeed())
		settleEnginePointerLog(ctx, session)
	})

	pointer := func(id uint64, pointerType engine.PointerType) *engine.SessionPointer {
		GinkgoHelper()
		p, err := session.Pointer(id, pointerType)
		Expect(err).NotTo(HaveOccurred())
		return p
	}

	It("dispatches real moves, presses, and releases where it is told, with buttons=1 while pressed", func(ctx SpecContext) {
		p := pointer(1, engine.MousePointer)
		Expect(p.MoveTo(ctx, 30, 30)).To(Succeed())
		Expect(p.Down(ctx)).To(Succeed())
		Expect(p.MoveTo(ctx, 60, 40)).To(Succeed())
		Expect(p.Up(ctx)).To(Succeed())
		Expect(p.MoveTo(ctx, 100, 60)).To(Succeed())
		Expect(readPointerLog(ctx, session)).To(HaveExactElements(
			pointerEventAt("pointermove", "mouse", 0, 30, 30),
			pointerEventAt("pointerdown", "mouse", 1, 30, 30),
			pointerEventAt("pointermove", "mouse", 1, 60, 40),
			pointerEventAt("pointerup", "mouse", 0, 60, 40),
			pointerEventAt("pointermove", "mouse", 0, 100, 60),
		))
	})

	It("dispatches pen events with pressure while the pen touches", func(ctx SpecContext) {
		pen := pointer(1, engine.PenPointer)
		Expect(pen.MoveTo(ctx, 30, 30)).To(Succeed())
		Expect(pen.Down(ctx)).To(Succeed())
		Expect(pen.MoveTo(ctx, 60, 40)).To(Succeed())
		Expect(pen.Up(ctx)).To(Succeed())
		log := readPointerLog(ctx, session)
		Expect(log).To(HaveExactElements(
			pointerEventAt("pointermove", "pen", 0, 30, 30),
			pointerEventAt("pointerdown", "pen", 1, 30, 30),
			pointerEventAt("pointermove", "pen", 1, 60, 40),
			pointerEventAt("pointerup", "pen", 0, 60, 40),
		))
		Expect([]float64{log[0].Pressure, log[1].Pressure, log[2].Pressure, log[3].Pressure}).To(Equal([]float64{0, 0.5, 0.5, 0}))
	})

	It("keeps each pointer's state under its own id", func(ctx SpecContext) {
		first, second := pointer(1, engine.MousePointer), pointer(2, engine.MousePointer)
		Expect(first.MoveTo(ctx, 30, 30)).To(Succeed())
		Expect(second.MoveTo(ctx, 90, 50)).To(Succeed())
		Expect(first.Down(ctx)).To(Succeed())
		Expect(first.Up(ctx)).To(Succeed())
		Expect(readPointerLog(ctx, session)[2:]).To(HaveExactElements(
			pointerEventAt("pointerdown", "mouse", 1, 30, 30),
			pointerEventAt("pointerup", "mouse", 0, 30, 30),
		))
		Expect(pointerErrorCode(pointer(1, engine.PenPointer).MoveTo(ctx, 1, 1))).To(Equal(engine.CodeInvalidArgument))
		_, err := session.Pointer(3, "touch")
		Expect(pointerErrorCode(err)).To(Equal(engine.CodeInvalidArgument))
	})

	It("moves to an element's center, or to an offset from its top-left corner", func(ctx SpecContext) {
		p := pointer(1, engine.MousePointer)
		Expect(p.MoveToElement(ctx, engine.CSS("#pad"), nil)).To(Succeed())
		Expect(p.MoveToElement(ctx, engine.CSS("#pad"), &engine.Point{X: 5, Y: 10})).To(Succeed())
		Expect(readPointerLog(ctx, session)).To(HaveExactElements(
			pointerEventAt("pointermove", "mouse", 0, 120, 60),
			pointerEventAt("pointermove", "mouse", 0, 25, 30),
		))
	})

	It("refuses, retryably and without scrolling, an element that is missing or outside the viewport", func(ctx SpecContext) {
		p := pointer(1, engine.MousePointer)
		err := p.MoveToElement(ctx, engine.CSS("#missing"), nil)
		Expect(err).To(HaveOccurred())
		Expect(engine.IsFatal(err)).To(BeFalse())
		err = p.MoveToElement(ctx, engine.CSS("#far"), nil)
		Expect(err).To(MatchError(ContainSubstring("the pointer does not scroll")))
		Expect(engine.IsFatal(err)).To(BeFalse())
		Expect(session.Evaluate(ctx, `window.scrollY`)).To(BeNumerically("==", 0))
		Expect(readPointerLog(ctx, session)).To(BeEmpty())
	})

	It("refuses to press a pressed pointer or release a released one", func(ctx SpecContext) {
		p := pointer(1, engine.MousePointer)
		Expect(pointerErrorCode(p.Up(ctx))).To(Equal(engine.CodeInvalidArgument))
		Expect(p.Down(ctx)).To(Succeed())
		err := p.Down(ctx)
		Expect(pointerErrorCode(err)).To(Equal(engine.CodeInvalidArgument))
		Expect(err).To(MatchError(ContainSubstring("the pointer is already pressed")))
		Expect(p.Up(ctx)).To(Succeed())
		Expect(readPointerLog(ctx, session)).To(HaveExactElements(
			pointerEventAt("pointerdown", "mouse", 1, 0, 0),
			pointerEventAt("pointerup", "mouse", 0, 0, 0),
		))
	})

	It("releases a pointer left pressed when the session prepares, and forgets it", func(ctx SpecContext) {
		_, err := session.Evaluate(ctx, `document.addEventListener('pointerup', () => navigator.sendBeacon('/released'))`)
		Expect(err).NotTo(HaveOccurred())
		p := pointer(1, engine.MousePointer)
		Expect(p.MoveTo(ctx, 40, 40)).To(Succeed())
		Expect(p.Down(ctx)).To(Succeed())
		Expect(session.Prepare(ctx)).To(Succeed())
		Eventually(released.Load).Should(BeNumerically("==", 1))

		Expect(session.Navigate(ctx, fixtures.URL+"/pointer.html")).To(Succeed())
		settleEnginePointerLog(ctx, session)
		Expect(p.Down(ctx)).To(Succeed(), "prepare forgot the pointer, so it starts released")
		Expect(p.Up(ctx)).To(Succeed())
		Expect(readPointerLog(ctx, session)[0]).To(pointerEventAt("pointerdown", "mouse", 1, 0, 0), "and back at (0, 0)")
	})

	Describe("on a same-process cross-origin frame", func() {
		var frame *engine.Frame
		var frameURL string

		BeforeEach(func(ctx SpecContext) {
			child := httptest.NewServer(http.FileServer(http.Dir("../fixtures")))
			DeferCleanup(child.Close)
			frameURL = child.URL + "/pointer.html"
			Expect(session.Navigate(ctx, fixtures.URL+"/frames.html?child="+url.QueryEscape(frameURL))).To(Succeed())
			var err error
			frame, err = session.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: frameURL}, HasElement: selectorPtr(engine.CSS("#pad"))}, engine.PollPolicy{Timeout: 5 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(frame.Close)
			Expect(frame.TargetID()).To(Equal(session.TargetID()), "these specs exercise the translation into the tab")
			settleEnginePointerLog(ctx, frame.Session)
		})

		It("works in the frame's viewport", func(ctx SpecContext) {
			p, err := frame.Pointer(1, engine.MousePointer)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.MoveTo(ctx, 30, 30)).To(Succeed())
			Expect(p.Down(ctx)).To(Succeed())
			Expect(p.MoveToElement(ctx, engine.CSS("#pad"), &engine.Point{X: 100, Y: 40})).To(Succeed())
			Expect(p.Up(ctx)).To(Succeed())
			Expect(readPointerLog(ctx, frame.Session)).To(HaveExactElements(
				pointerEventAt("pointermove", "mouse", 0, 30, 30),
				pointerEventAt("pointerdown", "mouse", 1, 30, 30),
				pointerEventAt("pointermove", "mouse", 1, 120, 60),
				pointerEventAt("pointerup", "mouse", 0, 120, 60),
			))
		})

		It("releases a pointer the frame handle left pressed when the handle closes", func(ctx SpecContext) {
			p, err := frame.Pointer(1, engine.MousePointer)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.MoveTo(ctx, 40, 40)).To(Succeed())
			Expect(p.Down(ctx)).To(Succeed())
			Expect(frame.Close()).To(Succeed())

			again, err := session.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: frameURL}}, engine.PollPolicy{Timeout: 5 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(again.Close)
			Expect(readPointerLog(ctx, again.Session)).To(HaveExactElements(
				pointerEventAt("pointermove", "mouse", 0, 40, 40),
				pointerEventAt("pointerdown", "mouse", 1, 40, 40),
				pointerEventAt("pointerup", "mouse", 0, 40, 40),
			))
		})
	})

	It("drags a slider in an out-of-process frame, dispatching to the frame's own target", func(ctx SpecContext) {
		isolated, err := engine.StartBrowser(context.Background(), engine.BrowserConfig{ExecutablePath: chromePath(), Arguments: []string{"--site-per-process"}, ArtifactDir: GinkgoT().TempDir()})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolated.Close)
		root, err := isolated.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		frameURL := strings.Replace(fixtures.URL, "127.0.0.1", "localhost", 1) + "/pointer.html"
		Expect(root.Navigate(ctx, fixtures.URL+"/frames.html?child="+url.QueryEscape(frameURL))).To(Succeed())
		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{HasElement: selectorPtr(engine.CSS("#track"))}, engine.PollPolicy{Timeout: 5 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).NotTo(Equal(root.TargetID()), "the frame must run in its own renderer")

		p, err := frame.Pointer(1, engine.MousePointer)
		Expect(err).NotTo(HaveOccurred())
		Expect(p.MoveToElement(ctx, engine.CSS("#track"), &engine.Point{X: 0, Y: 10})).To(Succeed())
		Expect(p.Down(ctx)).To(Succeed())
		for x := 30.0; x <= 170; x += 10 {
			Expect(p.MoveTo(ctx, x, 170)).To(Succeed())
		}
		Expect(p.Up(ctx)).To(Succeed())
		Expect(frame.Evaluate(ctx, `document.querySelector('#track').dataset.value`)).To(Equal("75"))
	})
})
