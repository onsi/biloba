package biloba

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
