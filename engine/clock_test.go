package engine_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// clockPage records, in window.log, each thing its timers do and the fake time each happened at.
const clockPage = `<!doctype html><title>clock</title><script>
	window.log = []
	const note = (what) => window.log.push(what + "@" + (Date.now() - Date.UTC(2024, 1, 2, 8)))
	setTimeout(() => note("timeout"), 500)
	const interval = setInterval(() => note("interval"), 200)
	setTimeout(() => Promise.resolve().then(() => note("microtask")), 100)
	setTimeout(() => { throw new Error("from a fake timer") }, 50)
	requestAnimationFrame(() => note("frame"))
	window.stop = () => clearInterval(interval)
</script>`

var _ = Describe("the page clock", func() {
	var session *engine.Session
	t0 := time.Date(2024, 2, 2, 8, 0, 0, 0, time.UTC)
	var pages *httptest.Server

	evaluate := func(ctx SpecContext, s *engine.Session, script string) any {
		GinkgoHelper()
		value, err := s.Evaluate(ctx, script)
		Expect(err).NotTo(HaveOccurred())
		return value
	}
	code := func(err error) engine.ErrorCode {
		var engineErr *engine.Error
		if errors.As(err, &engineErr) {
			return engineErr.Code
		}
		return ""
	}

	BeforeEach(func(ctx SpecContext) {
		pages = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(clockPage))
		}))
		DeferCleanup(pages.Close)
		var err error
		session, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
	})

	It("runs the page's timers, in order and with promise callbacks in between, only when told to", func(ctx SpecContext) {
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(session.PauseClockAt(ctx, t0)).To(Succeed())
		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		Expect(evaluate(ctx, session, `[Date.now() === Date.UTC(2024, 1, 2, 8), window.log.length]`)).To(Equal([]any{true, float64(0)}))

		Expect(session.RunClockFor(ctx, 500*time.Millisecond, 0)).To(Succeed())
		Expect(evaluate(ctx, session, `window.log`)).To(Equal([]any{"frame@16", "microtask@100", "interval@200", "interval@400", "timeout@500"}))
		state, installed := session.ClockState()
		Expect(installed).To(BeTrue())
		Expect(state.Paused).To(BeTrue())
		Expect(state.Now).To(Equal(t0.Add(500 * time.Millisecond)))

		Expect(session.FastForwardClock(ctx, time.Hour)).To(Succeed())
		Expect(evaluate(ctx, session, `window.log.slice(5)`)).To(Equal([]any{"interval@3600500"}), "a jump fires a due interval once")

		Expect(session.SetClockFixedTime(ctx, t0)).To(Succeed())
		Expect(evaluate(ctx, session, `[Date.now() === Date.UTC(2024, 1, 2, 8), new Date().getTime() === Date.UTC(2024, 1, 2, 8)]`)).To(Equal([]any{true, true}))
		Expect(session.SetClockSystemTime(ctx, t0.Add(-time.Hour))).To(Succeed())
		Expect(evaluate(ctx, session, `Date.now() === Date.UTC(2024, 1, 2, 7)`)).To(BeTrue())
		Expect(session.RunClockFor(ctx, 200*time.Millisecond, 0)).To(Succeed())
		Expect(evaluate(ctx, session, `window.log.slice(6)`)).To(Equal([]any{"interval@-3599800"}), "the interval kept the delay it had left")

		Expect(session.ResumeClock(ctx)).To(Succeed())
		state, _ = session.ClockState()
		Expect(state.Paused).To(BeFalse())
		Eventually(func(ctx SpecContext) any {
			return evaluate(ctx, session, `window.log.length > 8`)
		}).WithContext(ctx).Should(BeTrue(), "time flows again")
	})

	It("installs in the current document and the documents that follow", func(ctx SpecContext) {
		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		before := evaluate(ctx, session, `performance.now()`).(float64)
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(session.PauseClockAt(ctx, t0)).To(Succeed())
		Expect(evaluate(ctx, session, `Date.now() === Date.UTC(2024, 1, 2, 8)`)).To(BeTrue())
		Expect(evaluate(ctx, session, `performance.now()`)).To(BeNumerically(">=", before), "performance.now() does not go backwards")

		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		Expect(evaluate(ctx, session, `[Date.now() === Date.UTC(2024, 1, 2, 8), window.log.length]`)).To(Equal([]any{true, float64(0)}), "a new document starts where the clock is")
	})

	It("advances every document in the tab, frames included", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(clockPage))
		}))
		DeferCleanup(child.Close)
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(session.PauseClockAt(ctx, t0)).To(Succeed())
		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		evaluate(ctx, session, `const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(child.URL)+`; document.body.appendChild(frame)`)
		frame, err := session.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectContains, Expected: child.URL}}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).To(Equal(session.TargetID()), "the fixture must exercise a frame sharing its tab's renderer")

		Expect(session.RunClockFor(ctx, 500*time.Millisecond, 0)).To(Succeed())
		Expect(evaluate(ctx, session, `window.log.length`)).To(Equal(float64(5)))
		Expect(evaluate(ctx, frame.Session, `window.log`)).To(Equal([]any{"frame@16", "microtask@100", "interval@200", "interval@400", "timeout@500"}))

		state, installed := frame.ClockState()
		Expect(installed).To(BeTrue(), "a frame handle reports the clock of the tab that covers it")
		Expect(state.Paused).To(BeTrue())
		Expect(state.Now).To(Equal(t0.Add(500 * time.Millisecond)))

		Expect(code(frame.InstallClock(ctx, t0))).To(Equal(engine.CodeInvalidArgument))
		Expect(code(frame.RunClockFor(ctx, time.Second, 0))).To(Equal(engine.CodeInvalidArgument))
	})

	It("bounds RunFor by real time and reports how far it got", func(ctx SpecContext) {
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(session.PauseClockAt(ctx, t0)).To(Succeed())
		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		evaluate(ctx, session, `setInterval(() => {}, 1)`)
		err := session.RunClockFor(ctx, time.Hour, 50*time.Millisecond)
		Expect(code(err)).To(Equal(engine.CodeTimeout))
		Expect(err.Error()).To(ContainSubstring("RunFor(1h0m0s) did not finish within 50ms"))
		var exceeded *engine.ClockRunExceeded
		Expect(errors.As(err, &exceeded)).To(BeTrue(), "a client can phrase the overrun in its own names")
		Expect([]time.Duration{exceeded.Duration, exceeded.Budget}).To(Equal([]time.Duration{time.Hour, 50 * time.Millisecond}))
		Expect(exceeded.Fired).To(BeNumerically(">", 0))
		state, _ := session.ClockState()
		Expect(exceeded.Reached).To(Equal(state.Now))
		Expect(state.Now).To(BeTemporally(">", t0))
		Expect(state.Now).To(BeTemporally("<", t0.Add(time.Hour)))
	})

	It("refuses a second install and operations without one, and Prepare removes it", func(ctx SpecContext) {
		Expect(code(session.RunClockFor(ctx, time.Second, 0))).To(Equal(engine.CodeInvalidArgument))
		_, installed := session.ClockState()
		Expect(installed).To(BeFalse())
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(code(session.InstallClock(ctx, t0))).To(Equal(engine.CodeInvalidArgument))

		Expect(session.Prepare(ctx)).To(Succeed())
		_, installed = session.ClockState()
		Expect(installed).To(BeFalse())
		Expect(session.Navigate(ctx, pages.URL)).To(Succeed())
		Expect(evaluate(ctx, session, `[typeof window._bilobaClock, Date.now() > Date.UTC(2025, 0, 1)]`)).To(Equal([]any{"undefined", true}))
		Expect(session.InstallClock(ctx, t0)).To(Succeed(), "the next spec can install its own")
	})

	It("keeps Biloba's own waits on real time while the page's clock is paused", func(ctx SpecContext) {
		Expect(session.InstallClock(ctx, t0)).To(Succeed())
		Expect(session.PauseClockAt(ctx, t0)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		Expect(session.RealisticClick(ctx, engine.CSS("button"))).To(Succeed())
		text, err := session.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("saved"))
	})
})
