package biloba

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/onsi/biloba/engine"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// b.Context carries no deadline, so `chromedp.Run(b.Context, ...)` is a command that can wait for a
// wedged Chrome forever.  That is not a hypothetical: it is issue #9, where five workers stopped
// inside Runtime.evaluate and the suite died on its own timeout with no failing spec to point at.
//
// The knobs do not save you.  WithTimeout bounds Gomega's Eventually loop, but a Runtime.evaluate
// that never returns blocks INSIDE the matcher call, and Gomega cannot preempt a synchronous
// blocking call - so the poll deadline never gets a chance to fire.
//
// The repair (cdp.go) routes every command through runCDP/cdpContext, which carries the backstop
// deadline.  This spec is what keeps it repaired: the class regrows one call site at a time, and a
// single unbounded `chromedp.Run(b.Context, ...)` added later reintroduces the whole failure mode on
// whatever path it sits on - silently, because a bounded suite and an unbounded one look identical
// until Chrome misbehaves.  go vet cannot see this, and neither can a reviewer skimming a diff for
// something that looks like the fifteen bounded calls around it.
var _ = ginkgo.Describe("how Biloba's own source talks to Chrome", ginkgo.Label("no-browser"), func() {
	// The tab context, unbounded, handed straight to chromedp along with at least one action or
	// command - chromedp.Run, Do, Call, or CallBrowser.  Whitespace-tolerant so a reformat cannot slip
	// one past.  The trailing comma is what makes the one legitimate form - the no-action
	// `chromedp.Do(b.Context)` in ensureChromedpAllocated, which exists precisely so a bounded context
	// is never the one chromedp allocates the target on - fall outside the pattern rather than need an
	// exemption list.
	unboundedTabRun := regexp.MustCompile(`chromedp\.(Run|Do|Call|CallBrowser)(\[[^\]]*\])?\(\s*(b|tab|h\.b|newG)\.Context\s*,`)

	ginkgo.It("never sends a command to a tab context without a deadline", func() {
		sources, err := filepath.Glob("*.go")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(sources).NotTo(gomega.BeEmpty(), "should have found Biloba's own source files")

		offenders := []string{}
		for _, source := range sources {
			if strings.HasSuffix(source, "_test.go") {
				continue
			}
			contents, err := os.ReadFile(source)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			for i, line := range strings.Split(string(contents), "\n") {
				if unboundedTabRun.MatchString(line) {
					offenders = append(offenders, source+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
				}
			}
		}

		gomega.Expect(offenders).To(gomega.BeEmpty(),
			"A tab's Context carries no deadline, so a command sent straight to it waits on a wedged Chrome\n"+
				"forever - the hang in issue #9, where the suite died on its own timeout with no failing spec.\n"+
				"Send it through b.runCDP(\"what it does\", actions...) instead, which applies the backstop\n"+
				"deadline and names the cause (tab_closed / page_crashed / browser_gone / deadline_exceeded).  A command\n"+
				"whose wait IS the user's wait - a Cat 5a waiting command - uses b.waitingContext(default)\n"+
				"and b.runCDPIn.  See cdp.go.  Offending lines:\n  %s", strings.Join(offenders, "\n  "))
	})
})

// diagnoseCDPError is what turns a bounded failure into an actionable one, and its three verdicts are
// asserted here rather than against real Chrome on purpose.
//
// page_crashed depends on Chrome announcing the crash, which it does promptly on macOS and several
// seconds later on Linux.  The vocabulary is tested here, where setting the flag is deterministic
// everywhere; cdp_test.go crashes a real renderer and waits for the announcement.
var _ = ginkgo.Describe("diagnosing why a command failed", ginkgo.Label("no-browser"), func() {
	var b *Biloba
	underlying := errors.New("some underlying failure")

	ginkgo.BeforeEach(func() {
		b = &Biloba{lock: &sync.Mutex{}, state: newTabState(), crash: &engine.CrashSignal{}, destroyed: &engine.CrashSignal{}}
	})

	ginkgo.It("passes a healthy call through as nil", func() {
		gomega.Expect(b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, nil)).To(gomega.Succeed())
	})

	ginkgo.It("names a crashed renderer, and says how to recover", func() {
		b.crash.Mark()
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, underlying)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("page_crashed: this tab's renderer crashed, so Chrome could not evaluate JavaScript in the page")))
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("Navigate the tab again to get a fresh renderer")))
		gomega.Expect(errors.Is(err, underlying)).To(gomega.BeTrue(), "the underlying error stays wrapped, so callers can still match on it")
	})

	ginkgo.It("names a deadline, with the bound it exceeded", func() {
		err := b.diagnoseCDPError("dispatch keyboard input", 250*time.Millisecond, context.DeadlineExceeded)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("deadline_exceeded: Chrome did not dispatch keyboard input within 250ms")))
		gomega.Expect(errors.Is(err, context.DeadlineExceeded)).To(gomega.BeTrue())
	})

	ginkgo.It("names a closed tab, and says it cannot come back", func() {
		b.destroyed.Mark()
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, underlying)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("tab_closed: this tab was closed, so Chrome could not evaluate JavaScript in the page")))
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("A closed tab cannot come back")))
		gomega.Expect(errors.Is(err, underlying)).To(gomega.BeTrue())
	})

	ginkgo.It("prefers the closed tab to a crash or a deadline, since nothing recovers it", func() {
		b.destroyed.Mark()
		b.crash.Mark()
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, context.DeadlineExceeded)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("tab_closed")))
		gomega.Expect(err).NotTo(gomega.MatchError(gomega.ContainSubstring("page_crashed")))
		gomega.Expect(err).NotTo(gomega.MatchError(gomega.ContainSubstring("deadline_exceeded")))
	})

	ginkgo.It("prefers the crash to the deadline, since the crash is the reason for the deadline", func() {
		b.crash.Mark()
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, context.DeadlineExceeded)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("page_crashed")))
		gomega.Expect(err).NotTo(gomega.MatchError(gomega.ContainSubstring("deadline_exceeded")))
	})

	ginkgo.It("names a lost browser even before chromedp has cancelled the tab's context", func() {
		// chromedp cancels the contexts of a lost browser from a goroutine of its own, so a command can
		// fail on the lost connection first: hold the connection closed and the context live.
		ctx, cancel := chromedp.NewContext(context.Background())
		defer cancel()
		lost := make(chan struct{})
		close(lost)
		chromedp.FromContext(ctx).Browser = &chromedp.Browser{LostConnection: lost}
		b.Context, b.root = ctx, b
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, fmt.Errorf("lost the connection to the browser: EOF: %w", context.Canceled))
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("browser_gone: the connection to Chrome is closed")))
	})

	ginkgo.It("leaves an ordinary error alone rather than dressing it in wedge language", func() {
		thrown := fmt.Errorf("encountered exception: ReferenceError: nope is not defined")
		gomega.Expect(b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, thrown)).To(gomega.MatchError(thrown))
	})

	ginkgo.It("treats the caller's own cancelled context as theirs, not as a wedge", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		b.pollingCtx = ctx
		gomega.Expect(b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, context.DeadlineExceeded)).To(gomega.MatchError(context.DeadlineExceeded))
	})
})

// The real-crash spec in cdp_test.go cannot control when Chrome announces the crash relative to the
// commands it sends, so the two orderings are pinned here: a crash already announced refuses the
// command, and a crash announced while a command waits interrupts it.
var _ = ginkgo.Describe("running a command on a crashed tab", ginkgo.Label("no-browser"), func() {
	var b *Biloba

	ginkgo.BeforeEach(func() {
		b = &Biloba{lock: &sync.Mutex{}, state: newTabState(), crash: &engine.CrashSignal{}, destroyed: &engine.CrashSignal{}}
	})

	ginkgo.It("refuses the command at once, without sending it, once Chrome has announced the crash", func() {
		b.crash.Mark()
		sent := false
		err := b.runEngineIn(context.Background(), time.Minute, "evaluate JavaScript in the page", func(context.Context) error {
			sent = true
			return nil
		})
		gomega.Expect(sent).To(gomega.BeFalse())
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("page_crashed: this tab's renderer crashed, so Chrome could not evaluate JavaScript in the page")))
	})

	ginkgo.It("interrupts a command waiting on the renderer when the crash is announced, rather than at its deadline", func() {
		time.AfterFunc(50*time.Millisecond, func() { b.handleEventTargetCrashed(nil) })
		start := time.Now()
		err := b.runEngineIn(context.Background(), time.Minute, "evaluate JavaScript in the page", func(ctx context.Context) error {
			<-ctx.Done() // a renderer that never answers
			return ctx.Err()
		})
		gomega.Expect(time.Since(start)).To(gomega.BeNumerically("<", 5*time.Second))
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("page_crashed: this tab's renderer crashed")))
	})

	ginkgo.It("still sends a navigation, which is how a crashed tab recovers", func() {
		b.crash.Mark()
		sent := false
		err := b.runEngineRecoveringIn(context.Background(), time.Minute, "navigate", func(context.Context) error {
			sent = true
			return nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(sent).To(gomega.BeTrue())
	})

	ginkgo.It("lets commands through again once a frame navigates", func() {
		b.crash.Mark()
		b.handleEventFrameNavigated(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "main"}})
		gomega.Expect(b.runEngineIn(context.Background(), time.Minute, "evaluate JavaScript in the page", func(context.Context) error { return nil })).To(gomega.Succeed())
	})
})

// A closed tab is the crash's counterpart with no way back: Chrome answers nothing sent to a target it
// detached, so the same two orderings are pinned, and a navigation is refused too.
var _ = ginkgo.Describe("running a command on a closed tab", ginkgo.Label("no-browser"), func() {
	var b *Biloba

	ginkgo.BeforeEach(func() {
		b = &Biloba{lock: &sync.Mutex{}, state: newTabState(), crash: &engine.CrashSignal{}, destroyed: &engine.CrashSignal{}}
	})

	ginkgo.It("refuses a command, a navigation included, at once and without sending it", func() {
		b.destroyed.Mark()
		sent := false
		send := func(context.Context) error {
			sent = true
			return nil
		}
		err := b.runEngineIn(context.Background(), time.Minute, "evaluate JavaScript in the page", send)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("tab_closed: this tab was closed, so Chrome could not evaluate JavaScript in the page")))
		err = b.runEngineRecoveringIn(context.Background(), time.Minute, "navigate", send)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("tab_closed: this tab was closed, so Chrome could not navigate")))
		gomega.Expect(sent).To(gomega.BeFalse())
	})

	ginkgo.It("interrupts a command waiting on the tab when it closes, rather than at its deadline", func() {
		time.AfterFunc(50*time.Millisecond, b.destroyed.Mark)
		start := time.Now()
		err := b.runEngineIn(context.Background(), time.Minute, "evaluate JavaScript in the page", func(ctx context.Context) error {
			<-ctx.Done() // a command nothing will answer
			return ctx.Err()
		})
		gomega.Expect(time.Since(start)).To(gomega.BeNumerically("<", 5*time.Second))
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("tab_closed: this tab was closed")))
	})
})

var _ = ginkgo.Describe("cdpContext's WithContext handling", ginkgo.Label("no-browser"), func() {
	ginkgo.It("is already cancelled when the caller's WithContext was cancelled before the call", func() {
		b := &Biloba{lock: &sync.Mutex{}, state: newTabState(), Context: context.Background()}
		ctx, cancelPolling := context.WithCancel(context.Background())
		cancelPolling()
		b.pollingCtx = ctx

		cdpCtx, cancel := b.cdpContext(time.Second)
		defer cancel()

		gomega.Expect(cdpCtx.Err()).To(gomega.HaveOccurred())
	})
})
