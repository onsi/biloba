package engine_test

import (
	"context"
	"time"

	"github.com/onsi/biloba/engine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Both clients keep one CrashSignal per tab.  The real-crash paths are in cdp_test.go (Go) and
// typescript/test/e2e/crash.e2e.test.ts (TypeScript); these pin the rules they rely on.
var _ = Describe("CrashSignal", Label("no-browser"), func() {
	It("records a crash until it is cleared, and tolerates Chrome announcing it twice", func() {
		crash := &engine.CrashSignal{}
		Expect(crash.Crashed()).To(BeFalse())
		crash.Mark()
		crash.Mark()
		Expect(crash.Crashed()).To(BeTrue())
		crash.Clear()
		Expect(crash.Crashed()).To(BeFalse())
	})

	It("interrupts a command already waiting when the crash is announced", func() {
		crash := &engine.CrashSignal{}
		ctx, cancel := crash.Interrupt(context.Background())
		defer cancel()
		Consistently(ctx.Done(), "50ms").ShouldNot(BeClosed())
		crash.Mark()
		Eventually(ctx.Done()).Should(BeClosed())
	})

	It("keeps watching for a crash across a navigation that clears nothing", func() {
		// The Go client clears the signal whenever a frame navigates, which happens while other commands
		// are in flight.  Clearing a signal that records no crash must not orphan their watch.
		crash := &engine.CrashSignal{}
		ctx, cancel := crash.Interrupt(context.Background())
		defer cancel()
		crash.Clear()
		crash.Mark()
		Eventually(ctx.Done(), time.Second).Should(BeClosed())
	})
})
