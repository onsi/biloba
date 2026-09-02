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
	// The tab context, unbounded, handed straight to chromedp along with at least one action.
	// Whitespace-tolerant so a reformat cannot slip one past.  The trailing comma is what makes the
	// one legitimate form - the no-action `chromedp.Run(b.Context)` in ensureChromedpAllocated, which
	// exists precisely so a bounded context is never the one chromedp allocates the target on - fall
	// outside the pattern rather than need an exemption list.
	unboundedTabRun := regexp.MustCompile(`chromedp\.Run\(\s*(b|tab|h\.b|newG)\.Context\s*,`)

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
				"deadline and names the cause (page_crashed / browser_gone / deadline_exceeded).  A command\n"+
				"whose wait IS the user's wait - a Cat 5a waiting command - uses b.waitingContext(default)\n"+
				"and b.runCDPIn.  See cdp.go.  Offending lines:\n  %s", strings.Join(offenders, "\n  "))
	})
})

// diagnoseCDPError is what turns a bounded failure into an actionable one, and its three verdicts are
// asserted here rather than against real Chrome on purpose.
//
// page_crashed depends on Chrome announcing the crash, and Chrome does not do that dependably: with
// the Inspector domain enabled, neither Page.crash nor a chrome://crash navigation produces
// inspector.targetCrashed or target.targetCrashed on Linux, on either the page session or the browser
// connection, while both arrive on macOS.  A spec that crashed a real renderer and demanded the
// wording would therefore assert a Chrome behaviour rather than a Biloba one, and would fail on the
// platform most suites run on.  So the trigger is tested against real Chrome (cdp_test.go) and the
// vocabulary is tested here, where setting the flag is deterministic everywhere.
var _ = ginkgo.Describe("diagnosing why a command failed", ginkgo.Label("no-browser"), func() {
	var b *Biloba
	underlying := errors.New("some underlying failure")

	ginkgo.BeforeEach(func() {
		b = &Biloba{lock: &sync.Mutex{}, state: newTabState()}
	})

	ginkgo.It("passes a healthy call through as nil", func() {
		gomega.Expect(b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, nil)).To(gomega.Succeed())
	})

	ginkgo.It("names a crashed renderer, and says how to recover", func() {
		b.state.targetCrashed = true
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

	ginkgo.It("prefers the crash to the deadline, since the crash is the reason for the deadline", func() {
		b.state.targetCrashed = true
		err := b.diagnoseCDPError("evaluate JavaScript in the page", time.Second, context.DeadlineExceeded)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("page_crashed")))
		gomega.Expect(err).NotTo(gomega.MatchError(gomega.ContainSubstring("deadline_exceeded")))
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
