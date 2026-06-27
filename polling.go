package biloba

import (
	"context"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

/*
WithTimeout(d) returns a lightweight view of this tab whose polling DOM interactions use d as their
Eventually timeout instead of Gomega's global default.  Like [Biloba.Realistic] it is a shallow
clone-with-a-flag, so use it per-call:

	b.WithTimeout(5 * time.Second).Click("#submit")   // poll for up to 5s

Biloba polls by default; [Biloba.WithTimeout], [Biloba.WithPolling], and [Biloba.WithContext] tune
that poll while [Biloba.Immediate] opts out of it.  Which knobs a method accepts follows a four-bucket
model:

  - Polling methods (the action methods and value-getters) honor all four.
  - Waiting commands (Navigate, screenshots) keep their own default deadline but honor WithTimeout and
    WithContext; WithPolling and Immediate are a hard error.
  - Snapshot queries (Count, the Current*ForEach getters, ...) and one-shot mutations (Run, the
    *Immediately actions, dialog/cookie/storage setters, ...) reject every knob.
  - A call that resolves to a bare matcher (a Cat 6 matcher, or the under-applied matcher form of a
    dual method) rejects every knob too - configure the Eventually/Expect that wraps it instead.

So WithTimeout applies to the polling methods and the waiting commands; setting it on a snapshot, a
one-shot mutation, or a bare matcher is a hard error.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) WithTimeout(d time.Duration) *Biloba {
	nb := *b
	nb.timeout = &d
	return &nb
}

/*
WithPolling(d) returns a lightweight view of this tab whose polling DOM interactions use d as their
Eventually polling interval instead of Gomega's global default.  Like [Biloba.Realistic] it is a
shallow clone-with-a-flag:

	b.WithPolling(50 * time.Millisecond).Click("#submit")

WithPolling only applies to methods that poll.  Setting it on a waiting command, a snapshot, a
one-shot mutation, or a bare matcher is a hard error.  See [Biloba.WithTimeout] for the four-bucket
model that governs which knobs each method accepts.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) WithPolling(d time.Duration) *Biloba {
	nb := *b
	nb.pollingInterval = &d
	return &nb
}

/*
WithContext(ctx) returns a lightweight view of this tab whose polling DOM interactions thread ctx
into Eventually, so a cancelled/expired context aborts the poll.  Like [Biloba.Realistic] it is a
shallow clone-with-a-flag:

	b.WithContext(ctx).Click("#submit")

WithContext applies to methods that poll and to the waiting commands.  Setting it on a snapshot, a
one-shot mutation, or a bare matcher is a hard error.  See [Biloba.WithTimeout] for the four-bucket
model that governs which knobs each method accepts.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) WithContext(ctx context.Context) *Biloba {
	nb := *b
	nb.pollingCtx = ctx
	return &nb
}

/*
Immediate() returns a lightweight view of this tab whose polling DOM interactions act once and
fail fast instead of polling - the opt-in escape hatch from Biloba's poll-by-default behavior.  Like
[Biloba.Realistic] it is a shallow clone-with-a-flag:

	b.Immediate().Click("#submit")   // act once; fail immediately if not yet clickable

Immediate only applies to methods that poll.  Setting it on a waiting command, a snapshot, a one-shot
mutation, or a bare matcher is a hard error.  See [Biloba.WithTimeout] for the four-bucket model that
governs which knobs each method accepts.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Immediate() *Biloba {
	nb := *b
	nb.immediate = true
	return &nb
}

// pollOrImmediate is the heart of poll-by-default: it runs matcher against selector either by polling
// (the default - Eventually, honoring any WithTimeout/WithPolling/WithContext) or, when Immediate()
// is set, by asserting once (Expect).  It binds to b.gt via NewWithT - NOT the global fail handler -
// so the failure-capture harness and Helper() offsets keep working.  The matcher's (false, nil) =
// "not ready, retry" and (false, err) = "genuine error" semantics are preserved by Gomega: while
// polling both retry until the deadline; immediately both fail.
func (b *Biloba) pollOrImmediate(selector any, matcher types.GomegaMatcher) bool {
	b.gt.Helper()
	g := gomega.NewWithT(b.gt)
	if b.immediate {
		return g.Expect(selector).To(matcher)
	}
	assertion := g.Eventually(selector)
	if b.timeout != nil {
		assertion = assertion.WithTimeout(*b.timeout)
	}
	if b.pollingInterval != nil {
		assertion = assertion.WithPolling(*b.pollingInterval)
	}
	if b.pollingCtx != nil {
		assertion = assertion.WithContext(b.pollingCtx)
	}
	return assertion.Should(matcher)
}

// waitingContext builds the bounded context a Cat 5a waiting command (Navigate, the screenshot
// captures) runs under.  These commands keep a purpose-built default deadline (Navigate ~30s,
// screenshots ~5s) rather than inheriting Gomega's 1s default, but they DO honor the two knobs the
// four-bucket model allows them: WithTimeout overrides the default deadline and WithContext aborts
// the wait when the supplied context is cancelled.  WithPolling/Immediate are rejected upstream by
// guardConfig.
//
// The returned context is always parented on b.Context so chromedp's executor stays in the chain (a
// user's WithContext is typically a plain context.Background-derived context with no executor);
// WithContext is honored for cancellation by tying it to the returned context via context.AfterFunc.
func (b *Biloba) waitingContext(defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
	timeout := defaultTimeout
	if b.timeout != nil {
		timeout = *b.timeout
	}
	ctx, cancel := context.WithTimeout(b.Context, timeout)
	if b.pollingCtx != nil {
		stop := context.AfterFunc(b.pollingCtx, cancel)
		return ctx, func() { stop(); cancel() }
	}
	return ctx, cancel
}

// configKnob names one of the four poll-config knobs so guardConfig can report exactly which
// misapplied knob tripped the four-bucket model.
type configKnob struct {
	name string
	set  func(*Biloba) bool
}

var (
	knobTimeout   = configKnob{"WithTimeout", func(b *Biloba) bool { return b.timeout != nil }}
	knobPolling   = configKnob{"WithPolling", func(b *Biloba) bool { return b.pollingInterval != nil }}
	knobContext   = configKnob{"WithContext", func(b *Biloba) bool { return b.pollingCtx != nil }}
	knobImmediate = configKnob{"Immediate", func(b *Biloba) bool { return b.immediate }}

	allKnobs = []configKnob{knobTimeout, knobPolling, knobContext, knobImmediate}
)

// guardConfig enforces the four-bucket model: it fails the spec if any poll-config knob that is set
// on b is not in allowed.  Polling methods support every knob and so skip the guard entirely; waiting
// commands pass {knobTimeout, knobContext}; snapshots and one-shot mutations pass nothing.  method is
// the human name used in the failure message.
func (b *Biloba) guardConfig(method string, allowed ...configKnob) {
	b.gt.Helper()
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k.name] = true
	}
	for _, k := range allKnobs {
		if k.set(b) && !ok[k.name] {
			b.gt.Fatalf("%s does not support %s", method, k.name)
			return
		}
	}
}

// guardBareMatcher rejects every poll-config knob on a call that resolves to a bare Gomega matcher
// (a Cat 6 matcher, or the under-applied matcher form of a dual method): you configure the
// Eventually/Expect that wraps the matcher, not the matcher itself.
func (b *Biloba) guardBareMatcher(method string) {
	b.gt.Helper()
	for _, k := range allKnobs {
		if k.set(b) {
			b.gt.Fatalf("%s(...) returns a matcher - configure the Eventually/Expect that polls it, not %s with %s", method, method, k.name)
			return
		}
	}
}
