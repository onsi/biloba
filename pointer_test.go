package biloba_test

import (
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	"github.com/onsi/biloba"
)

// pointerEvent is one entry of fixtures/pointer.html's window.pointerLog
type pointerEvent struct {
	Type        string  `json:"type"`
	PointerType string  `json:"pointerType"`
	Buttons     int     `json:"buttons"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Pressure    float64 `json:"pressure"`
	Target      string  `json:"target"`
}

func pointerLog(tab *biloba.Biloba) []pointerEvent {
	var log []pointerEvent
	tab.Run("window.pointerLog", &log)
	return log
}

func clearPointerLog(tab *biloba.Biloba) {
	tab.Run("window.pointerLog = []")
}

func pointerEventMatching(typ, pointerType string, buttons int, x, y float64) OmegaMatcher {
	return MatchFields(IgnoreExtras, Fields{
		"Type":        Equal(typ),
		"PointerType": Equal(pointerType),
		"Buttons":     Equal(buttons),
		"X":           BeNumerically("~", x, 1),
		"Y":           BeNumerically("~", y, 1),
	})
}

var _ = Describe("Pointer", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/pointer.html")
		Eventually("#pad").Should(b.Exist())
	})

	It("dispatches real mouse moves, presses, and releases where it is told", func() {
		p := b.Pointer()
		p.MoveTo(30, 30)
		p.Down()
		p.MoveTo(60, 40)
		p.MoveTo(90, 50)
		p.Up()
		p.MoveTo(100, 60)

		Ω(pointerLog(b)).Should(HaveExactElements(
			pointerEventMatching("pointermove", "mouse", 0, 30, 30),
			pointerEventMatching("pointerdown", "mouse", 1, 30, 30),
			pointerEventMatching("pointermove", "mouse", 1, 60, 40),
			pointerEventMatching("pointermove", "mouse", 1, 90, 50),
			pointerEventMatching("pointerup", "mouse", 0, 90, 50),
			pointerEventMatching("pointermove", "mouse", 0, 100, 60),
		))
	})

	It("dispatches pen events, with pressure while the pen touches", func() {
		pen := b.Pointer(b.Pen())
		pen.MoveTo(30, 30)
		pen.Down()
		pen.MoveTo(60, 40)
		pen.Up()

		log := pointerLog(b)
		Ω(log).Should(HaveExactElements(
			pointerEventMatching("pointermove", "pen", 0, 30, 30),
			pointerEventMatching("pointerdown", "pen", 1, 30, 30),
			pointerEventMatching("pointermove", "pen", 1, 60, 40),
			pointerEventMatching("pointerup", "pen", 0, 60, 40),
		))
		Ω([]float64{log[0].Pressure, log[1].Pressure, log[2].Pressure, log[3].Pressure}).Should(Equal([]float64{0, 0.5, 0.5, 0}))
	})

	It("draws pen strokes on an emulated tablet", func() {
		b.SetWindowSize(820, 1180, b.Mobile(), b.TouchScreen())
		b.SetUserAgent("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
		b.Navigate(fixtureServer + "/pointer.html")
		Eventually("#pad").Should(b.Exist())

		pen := b.Pointer(b.Pen())
		pen.MoveTo(30, 60)
		pen.Down()
		for i := 1; i <= 14; i++ {
			time.Sleep(16 * time.Millisecond)
			pen.MoveTo(30+float64(i)*10, 60)
		}
		pen.Up()

		log := pointerLog(b)
		Ω(log).Should(HaveLen(17))
		Ω(log[1]).Should(pointerEventMatching("pointerdown", "pen", 1, 30, 60))
		Ω(log[15]).Should(pointerEventMatching("pointermove", "pen", 1, 170, 60))
		Ω(log[16]).Should(pointerEventMatching("pointerup", "pen", 0, 170, 60))
		for _, event := range log {
			Ω(event.PointerType).Should(Equal("pen"))
		}
	})

	It("drags a range input and a custom slider", func() {
		p := b.Pointer()
		p.MoveToElement("#range", b.At(2, 5))
		p.Down()
		for x := 30.0; x <= 200; x += 20 {
			p.MoveTo(x, 128)
		}
		p.Up()
		Ω(b.GetProperty("#range", "valueAsNumber")).Should(BeNumerically(">", 80))

		p.MoveToElement("#track", b.At(0, 10))
		p.Down()
		p.MoveTo(70, 170)
		Ω(b.GetAttribute("#track", "data-value")).Should(Equal("25"))
		p.MoveTo(170, 170)
		p.Up()
		Ω(b.GetAttribute("#track", "data-value")).Should(Equal("75"))
	})

	It("moves to an element's center, or to an offset from its top-left corner", func() {
		p := b.Pointer()
		p.MoveToElement("#pad")
		p.MoveToElement("#pad", b.At(5, 10))
		Ω(pointerLog(b)).Should(HaveExactElements(
			pointerEventMatching("pointermove", "mouse", 0, 120, 60),
			pointerEventMatching("pointermove", "mouse", 0, 25, 30),
		))
	})

	It("polls for an element that shows up late", func() {
		b.Run(`setTimeout(() => { let d = document.createElement("div"); d.id = "late"; d.style.cssText = "position:absolute;left:300px;top:300px;width:40px;height:40px"; document.body.appendChild(d) }, 200)`)
		p := b.Pointer()
		p.MoveToElement("#late")
		Ω(pointerLog(b)).Should(HaveExactElements(pointerEventMatching("pointermove", "mouse", 0, 320, 320)))
	})

	It("polls with the knobs of the view the pointer was made from", func() {
		b.WithTimeout(100 * time.Millisecond).Pointer().MoveToElement("#missing")
		ExpectFailures(SatisfyAll(
			ContainSubstring("Timed out after 0."), // the view's 100ms, not Gomega's 1s default (and slack for a loaded machine)
			ContainSubstring("could not find DOM element matching selector: #missing"),
		))

		b.Immediate().Pointer().MoveToElement("#missing")
		ExpectFailures(ContainSubstring("could not find DOM element matching selector: #missing"))
		Ω(pointerLog(b)).Should(BeEmpty())
	})

	It("does not scroll: an element outside the viewport is an error", func() {
		b.Immediate().Pointer().MoveToElement("#far")
		ExpectFailures(SatisfyAll(
			ContainSubstring("is outside the"),
			ContainSubstring("the pointer does not scroll"),
			ContainSubstring("#far"),
		))
		Ω(b.Run("window.scrollY")).Should(BeNumerically("==", 0))

		b.ScrollIntoView("#far")
		b.Pointer().MoveToElement("#far")
		Ω(pointerLog(b)).Should(HaveLen(1))
	})

	It("dispatches MoveTo, Down, and Up at once whatever view it was made from", func() {
		p := b.WithTimeout(time.Minute).Pointer()
		p.MoveTo(30, 30)
		p.Down()
		p.Up()
		Ω(pointerLog(b)).Should(HaveLen(3))
	})

	It("refuses to press a pressed pointer or release a released one", func() {
		p := b.Pointer()
		p.Up()
		ExpectFailures(ContainSubstring("Pointer.Up: the pointer is not pressed"))
		p.Down()
		p.Down()
		ExpectFailures(ContainSubstring("Pointer.Down: the pointer is already pressed"))
		p.Up()
		Ω(pointerLog(b)).Should(HaveExactElements(
			pointerEventMatching("pointerdown", "mouse", 1, 0, 0),
			pointerEventMatching("pointerup", "mouse", 0, 0, 0),
		))
	})

	Describe("on a cross-origin frame handle", func() {
		var frame *biloba.Biloba

		BeforeEach(func() {
			frameURL := crossOriginFixtureServer + "/pointer.html"
			b.Navigate(fixtureServer + "/frames.html?child=" + url.QueryEscape(frameURL))
			frame = b.Frame(b.FrameMatching().WithURL(frameURL).WithDOMElement("#pad"))
			Ω(frame).ShouldNot(BeNil())
			Ω(frame.FrameOutOfProcessForTest()).Should(BeFalse(), "these specs exercise the translation into the tab")
		})

		It("works in the frame's viewport", func() {
			p := frame.Pointer()
			p.MoveTo(30, 30)
			p.Down()
			p.MoveToElement("#pad", b.At(100, 40))
			p.Up()
			Ω(pointerLog(frame)).Should(HaveExactElements(
				pointerEventMatching("pointermove", "mouse", 0, 30, 30),
				pointerEventMatching("pointerdown", "mouse", 1, 30, 30),
				pointerEventMatching("pointermove", "mouse", 1, 120, 60),
				pointerEventMatching("pointerup", "mouse", 0, 120, 60),
			))
		})

		It("drags a slider in the frame", func() {
			p := frame.Pointer()
			p.MoveToElement("#track", b.At(0, 10))
			p.Down()
			for x := 30.0; x <= 170; x += 10 {
				p.MoveTo(x, 170)
			}
			p.Up()
			Ω(frame.GetAttribute("#track", "data-value")).Should(Equal("75"))
		})
	})
})

// Ordered, so the page (and its pointer log) survives from one spec to the next: Prepare runs once
var _ = Describe("A pointer left pressed when the spec ends", Ordered, func() {
	var p *biloba.Pointer

	BeforeAll(func() {
		b.Navigate(fixtureServer + "/pointer.html")
		Eventually("#pad").Should(b.Exist())
	})

	It("is pressed by the first spec", func() {
		p = b.Pointer()
		p.MoveTo(40, 40)
		p.Down()
		Ω(pointerLog(b)).Should(HaveLen(2))
	})

	It("has been released when the next one starts", func() {
		Ω(pointerLog(b)).Should(HaveExactElements(
			pointerEventMatching("pointermove", "mouse", 0, 40, 40),
			pointerEventMatching("pointerdown", "mouse", 1, 40, 40),
			pointerEventMatching("pointerup", "mouse", 0, 40, 40),
		))
		clearPointerLog(b)
		p.Down()
		p.Up()
		Ω(pointerLog(b)).Should(HaveLen(2))
	})
})
