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
