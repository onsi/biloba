package biloba

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

// These are plain testing-based units (not Ginkgo specs) because dot-importing Ginkgo into package
// biloba collides with biloba's own GinkgoTInterface.  They exercise the pure recorder logic - no
// browser needed.
func TestProbeRecorder(t *testing.T) {
	g := NewWithT(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(seconds float64) time.Time {
		return base.Add(time.Duration(seconds * float64(time.Second)))
	}

	// run-length collapsing: consecutive identical values fold into one segment and read as flat
	p := &probeRecorder{}
	for i := range 18 {
		p.record("Run x", 587.0, at(float64(i)*0.11))
	}
	out := p.render()
	g.Expect(out).To(ContainSubstring("Probe: Run x"))
	g.Expect(out).To(ContainSubstring("18 samples"))
	g.Expect(out).To(ContainSubstring("held ×18"))
	g.Expect(out).To(ContainSubstring("flat"))

	// most-recent-polled-entity: a new key supersedes the prior (resolved) series
	p = &probeRecorder{}
	p.record("Run a", 1.0, at(0))
	p.record("Run a", 1.0, at(0.1))
	p.record("Run b", 42.0, at(0.2))
	out = p.render()
	g.Expect(out).To(ContainSubstring("Probe: Run b"))
	g.Expect(out).NotTo(ContainSubstring("Run a"))
	g.Expect(out).To(ContainSubstring("1 samples"))

	// shape: monotone approach == latency
	p = &probeRecorder{}
	for i, v := range []float64{587, 540, 300, 130} {
		p.record("Run x", v, at(float64(i)*0.5))
	}
	g.Expect(p.render()).To(ContainSubstring("monotone"))
	g.Expect(p.render()).NotTo(ContainSubstring("non-monotone"))

	// shape: dip-then-rebound == late reflow
	p = &probeRecorder{}
	for i, v := range []float64{587, 130, 24, 300} {
		p.record("Run x", v, at(float64(i)*0.5))
	}
	g.Expect(p.render()).To(ContainSubstring("non-monotone"))

	// non-numeric series: no direction
	p = &probeRecorder{}
	p.record("Run x", "loading", at(0))
	p.record("Run x", "ready", at(0.5))
	out = p.render()
	g.Expect(out).NotTo(ContainSubstring("monotone"))
	g.Expect(out).NotTo(ContainSubstring("flat"))

	// bounding the segment count: keep the recent tail, count elided changes
	p = &probeRecorder{}
	for i := range maxProbeSegments + 10 {
		p.record("Run x", float64(i), at(float64(i)*0.01))
	}
	out = p.render()
	g.Expect(out).To(ContainSubstring("earlier value-changes elided"))
	g.Expect(out).To(ContainSubstring(renderProbeValue(float64(maxProbeSegments + 9))))
}

func TestMatchTrail(t *testing.T) {
	g := NewWithT(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(seconds float64) time.Time {
		return base.Add(time.Duration(seconds * float64(time.Second)))
	}

	// matched, then stopped matching: the detached-node signature
	p := &probeRecorder{}
	for i := range 6 {
		p.recordMatch("s#row-4", "#row-4", true, at(float64(i)*0.082))
	}
	g.Expect(p.renderDetachedNode()).To(BeEmpty()) // still matching: nothing to say
	for i := range 3 {
		p.recordMatch("s#row-4", "#row-4", false, at(0.5+float64(i)*0.1))
	}
	out := p.renderDetachedNode()
	g.Expect(out).To(ContainSubstring(`⚠ Selector "#row-4" matched 6× during this poll (+0.00s to +0.41s) then stopped matching`))
	g.Expect(out).To(ContainSubstring("the node was likely replaced, or its identifying attribute changed in place"))

	// never matched at all: the ordinary "could not find" failure already says this - stay quiet
	p = &probeRecorder{}
	for i := range 10 {
		p.recordMatch("s#typo", "#typo", false, at(float64(i)*0.05))
	}
	g.Expect(p.renderDetachedNode()).To(BeEmpty())

	// a new selector supersedes the prior (resolved) trail - single series, like the value axis
	p = &probeRecorder{}
	p.recordMatch("s#a", "#a", true, at(0))
	p.recordMatch("s#a", "#a", false, at(0.1))
	p.recordMatch("s#b", "#b", true, at(0.2))
	g.Expect(p.renderDetachedNode()).To(BeEmpty())

	// a selector that comes BACK is not a detached node
	p = &probeRecorder{}
	p.recordMatch("s#a", "#a", true, at(0))
	p.recordMatch("s#a", "#a", false, at(0.1))
	p.recordMatch("s#a", "#a", true, at(0.2))
	g.Expect(p.renderDetachedNode()).To(BeEmpty())

	// the match trail is independent of the value series and survives value recording
	p = &probeRecorder{}
	p.recordMatch("s#a", "#a", true, at(0))
	p.record("GetBoundingBox s#a", 3.0, at(0))
	p.recordMatch("s#a", "#a", false, at(0.2))
	g.Expect(p.renderDetachedNode()).To(ContainSubstring(`Selector "#a" matched 1×`))
	g.Expect(p.render()).To(ContainSubstring("Probe: GetBoundingBox s#a"))

	// reset drops the diagnosis so it can't leak into the next spec
	p.resetMatch()
	g.Expect(p.renderDetachedNode()).To(BeEmpty())
}

func TestRecordMatchGating(t *testing.T) {
	g := NewWithT(t)

	off := &Biloba{pollTrajectory: false, probes: &probeRecorder{}}
	off.recordMatch("s#a", "#a", true)
	off.recordMatch("s#a", "#a", false)
	g.Expect(off.probes.renderDetachedNode()).To(BeEmpty())

	on := &Biloba{pollTrajectory: true, probes: &probeRecorder{}}
	on.recordMatch("s#a", "#a", true)
	on.recordMatch("s#a", "#a", false)
	g.Expect(on.probes.renderDetachedNode()).To(ContainSubstring(`Selector "#a" matched 1×`))

	nilRec := &Biloba{pollTrajectory: true, probes: nil}
	g.Expect(func() { nilRec.recordMatch("s#a", "#a", true) }).NotTo(Panic())
}

func TestRenderProbeValue(t *testing.T) {
	g := NewWithT(t)
	g.Expect(renderProbeValue(587.0)).To(Equal("587"))
	g.Expect(renderProbeValue(120.5)).To(Equal("120.5"))
	g.Expect(renderProbeValue("hi")).To(Equal(`"hi"`))
	g.Expect(renderProbeValue(true)).To(Equal("true"))
	g.Expect(renderProbeValue(nil)).To(Equal("<nil>"))
	g.Expect(renderProbeValue(Box{Top: 1, Left: 2, Width: 3, Height: 4})).To(ContainSubstring("Box{Top:1 Left:2"))
}

func TestRecordProbeGating(t *testing.T) {
	g := NewWithT(t)

	off := &Biloba{pollTrajectory: false, probes: &probeRecorder{}}
	off.recordProbe("Run x", 1.0)
	g.Expect(off.probes.render()).To(BeEmpty())

	on := &Biloba{pollTrajectory: true, probes: &probeRecorder{}}
	on.recordProbe("Run x", 1.0)
	g.Expect(on.probes.render()).To(ContainSubstring("Probe: Run x"))

	nilRec := &Biloba{pollTrajectory: true, probes: nil}
	g.Expect(func() { nilRec.recordProbe("Run x", 1.0) }).NotTo(Panic())
}
