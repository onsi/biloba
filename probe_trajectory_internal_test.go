package biloba

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// These specs live in package biloba (not biloba_test) because they exercise the probe recorder's
// unexported types directly - no browser needed.  Ginkgo and Gomega are imported under their package
// names rather than dot-imported, matching tab_state_internal_test.go; see the note at the top of
// that file for why that's all it takes to avoid colliding with biloba's own exported names.
var _ = ginkgo.Describe("the poll-trajectory probe recorder", ginkgo.Label("no-browser"), func() {
	// at builds a deterministic timestamp offset seconds from a fixed base, so recorded series have
	// reproducible gaps between samples.
	at := func(seconds float64) time.Time {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		return base.Add(time.Duration(seconds * float64(time.Second)))
	}

	ginkgo.Describe("probeRecorder value series", func() {
		ginkgo.It("renders nothing until a series is blamed", func() {
			p := &probeRecorder{}
			for i := range 18 {
				p.record("Run x", 587.0, at(float64(i)*0.11))
			}
			gomega.Expect(p.render()).To(gomega.BeEmpty(), "an unblamed series is never rendered")
		})

		ginkgo.It("collapses a run of identical values into one flat segment once blamed", func() {
			// run-length collapsing: consecutive identical values fold into one segment and read as flat
			p := &probeRecorder{}
			for i := range 18 {
				p.record("Run x", 587.0, at(float64(i)*0.11))
			}
			p.blame("Run x")
			out := p.render()
			gomega.Expect(out).To(gomega.ContainSubstring("Probe: Run x"))
			gomega.Expect(out).To(gomega.ContainSubstring("18 samples"))
			gomega.Expect(out).To(gomega.ContainSubstring("held ×18"))
			gomega.Expect(out).To(gomega.ContainSubstring("flat"))
		})

		ginkgo.It("renders only the most recently blamed series, dropping a superseded one", func() {
			// most-recent-polled-entity: a new key supersedes the prior (resolved) series
			p := &probeRecorder{}
			p.record("Run a", 1.0, at(0))
			p.record("Run a", 1.0, at(0.1))
			p.record("Run b", 42.0, at(0.2))
			p.blame("Run b")
			out := p.render()
			gomega.Expect(out).To(gomega.ContainSubstring("Probe: Run b"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("Run a"))
			gomega.Expect(out).To(gomega.ContainSubstring("1 samples"))
		})

		ginkgo.It("labels a monotonically approaching series as monotone (a latency shape)", func() {
			p := &probeRecorder{}
			for i, v := range []float64{587, 540, 300, 130} {
				p.record("Run x", v, at(float64(i)*0.5))
			}
			p.blame("Run x")
			gomega.Expect(p.render()).To(gomega.ContainSubstring("monotone"))
			gomega.Expect(p.render()).NotTo(gomega.ContainSubstring("non-monotone"))
		})

		ginkgo.It("labels a dip-then-rebound series as non-monotone (a late-reflow shape)", func() {
			p := &probeRecorder{}
			for i, v := range []float64{587, 130, 24, 300} {
				p.record("Run x", v, at(float64(i)*0.5))
			}
			p.blame("Run x")
			gomega.Expect(p.render()).To(gomega.ContainSubstring("non-monotone"))
		})

		ginkgo.It("gives no direction verdict for a non-numeric series", func() {
			p := &probeRecorder{}
			p.record("Run x", "loading", at(0))
			p.record("Run x", "ready", at(0.5))
			p.blame("Run x")
			out := p.render()
			gomega.Expect(out).NotTo(gomega.ContainSubstring("monotone"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("flat"))
		})

		ginkgo.It("keeps the recent tail of a long series and counts the elided earlier changes", func() {
			p := &probeRecorder{}
			for i := range maxProbeSegments + 10 {
				p.record("Run x", float64(i), at(float64(i)*0.01))
			}
			p.blame("Run x")
			out := p.render()
			gomega.Expect(out).To(gomega.ContainSubstring("earlier value-changes elided"))
			gomega.Expect(out).To(gomega.ContainSubstring(renderProbeValue(float64(maxProbeSegments + 9))))
		})

		ginkgo.It("renders only the blamed series, and a single sample is never called flat", func() {
			// blame names ONE series: a series that is not the blamed one renders nothing, and a single
			// sample never claims the "value never changed" diagnosis - it cannot tell that from
			// "only read once"
			p := &probeRecorder{}
			p.record("GetJSValue window.ready", true, at(0))
			p.blame("HaveProperty:innerText s#x") // some other read is the one that failed
			gomega.Expect(p.render()).To(gomega.BeEmpty())
			p.blame("GetJSValue window.ready")
			out := p.render()
			gomega.Expect(out).To(gomega.ContainSubstring("1 samples"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("flat"))
		})

		ginkgo.It("drops the recorded series and blame on reset, so neither leaks into the next spec", func() {
			p := &probeRecorder{}
			p.record("GetJSValue window.ready", true, at(0))
			p.blame("GetJSValue window.ready")
			gomega.Expect(p.render()).NotTo(gomega.BeEmpty())
			p.reset()
			gomega.Expect(p.render()).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("the match trail (detached-node detection)", func() {
		ginkgo.It("reports a selector that matched and then stopped matching as likely detached", func() {
			p := &probeRecorder{}
			for i := range 6 {
				p.recordMatch("s#row-4", "#row-4", true, at(float64(i)*0.082))
			}
			gomega.Expect(p.renderDetachedNode()).To(gomega.BeEmpty()) // still matching: nothing to say
			for i := range 3 {
				p.recordMatch("s#row-4", "#row-4", false, at(0.5+float64(i)*0.1))
			}
			out := p.renderDetachedNode()
			gomega.Expect(out).To(gomega.ContainSubstring(`⚠ Selector "#row-4" matched 6× during this poll (+0.00s to +0.41s) then stopped matching`))
			gomega.Expect(out).To(gomega.ContainSubstring("the node was likely replaced, or its identifying attribute changed in place"))
		})

		ginkgo.It("stays quiet about a selector that never matched at all", func() {
			// the ordinary "could not find" failure already says this
			p := &probeRecorder{}
			for i := range 10 {
				p.recordMatch("s#typo", "#typo", false, at(float64(i)*0.05))
			}
			gomega.Expect(p.renderDetachedNode()).To(gomega.BeEmpty())
		})

		ginkgo.It("drops the prior (resolved) trail once a new selector supersedes it", func() {
			// a new selector supersedes the prior trail - single series, like the value axis
			p := &probeRecorder{}
			p.recordMatch("s#a", "#a", true, at(0))
			p.recordMatch("s#a", "#a", false, at(0.1))
			p.recordMatch("s#b", "#b", true, at(0.2))
			gomega.Expect(p.renderDetachedNode()).To(gomega.BeEmpty())
		})

		ginkgo.It("does not call a selector that comes back a detached node", func() {
			p := &probeRecorder{}
			p.recordMatch("s#a", "#a", true, at(0))
			p.recordMatch("s#a", "#a", false, at(0.1))
			p.recordMatch("s#a", "#a", true, at(0.2))
			gomega.Expect(p.renderDetachedNode()).To(gomega.BeEmpty())
		})

		ginkgo.It("keeps the match trail independent of the value series", func() {
			// the match trail survives value recording, and vice versa
			p := &probeRecorder{}
			p.recordMatch("s#a", "#a", true, at(0))
			p.record("GetBoundingBox s#a", 3.0, at(0))
			p.recordMatch("s#a", "#a", false, at(0.2))
			p.blame("GetBoundingBox s#a")
			gomega.Expect(p.renderDetachedNode()).To(gomega.ContainSubstring(`Selector "#a" matched 1×`))
			gomega.Expect(p.render()).To(gomega.ContainSubstring("Probe: GetBoundingBox s#a"))
		})

		ginkgo.It("drops the detached-node diagnosis on reset, so it can't leak into the next spec", func() {
			p := &probeRecorder{}
			p.recordMatch("s#a", "#a", true, at(0))
			p.recordMatch("s#a", "#a", false, at(0.2))
			gomega.Expect(p.renderDetachedNode()).NotTo(gomega.BeEmpty())
			p.reset()
			gomega.Expect(p.renderDetachedNode()).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("recordMatch gating on pollTrajectory", func() {
		ginkgo.It("records nothing when pollTrajectory is false", func() {
			off := &Biloba{pollTrajectory: false, probes: &probeRecorder{}}
			off.recordMatch("s#a", "#a", true)
			off.recordMatch("s#a", "#a", false)
			gomega.Expect(off.probes.renderDetachedNode()).To(gomega.BeEmpty())
		})

		ginkgo.It("records and renders when pollTrajectory is true", func() {
			on := &Biloba{pollTrajectory: true, probes: &probeRecorder{}}
			on.recordMatch("s#a", "#a", true)
			on.recordMatch("s#a", "#a", false)
			gomega.Expect(on.probes.renderDetachedNode()).To(gomega.ContainSubstring(`Selector "#a" matched 1×`))
		})

		ginkgo.It("is safe to call with a nil probes recorder", func() {
			nilRec := &Biloba{pollTrajectory: true, probes: nil}
			gomega.Expect(func() { nilRec.recordMatch("s#a", "#a", true) }).NotTo(gomega.Panic())
		})
	})

	ginkgo.Describe("renderProbeValue", func() {
		ginkgo.It("renders each supported value type", func() {
			gomega.Expect(renderProbeValue(587.0)).To(gomega.Equal("587"))
			gomega.Expect(renderProbeValue(120.5)).To(gomega.Equal("120.5"))
			gomega.Expect(renderProbeValue("hi")).To(gomega.Equal(`"hi"`))
			gomega.Expect(renderProbeValue(true)).To(gomega.Equal("true"))
			gomega.Expect(renderProbeValue(nil)).To(gomega.Equal("<nil>"))
			gomega.Expect(renderProbeValue(Box{Top: 1, Left: 2, Width: 3, Height: 4})).To(gomega.ContainSubstring("Box{Top:1 Left:2"))
		})
	})

	ginkgo.Describe("recordProbe/blameProbe gating on pollTrajectory", func() {
		ginkgo.It("records nothing when pollTrajectory is false", func() {
			off := &Biloba{pollTrajectory: false, probes: &probeRecorder{}}
			off.recordProbe("Run x", 1.0)
			off.blameProbe("Run x")
			gomega.Expect(off.probes.render()).To(gomega.BeEmpty())
		})

		ginkgo.It("records and renders when pollTrajectory is true", func() {
			on := &Biloba{pollTrajectory: true, probes: &probeRecorder{}}
			on.recordProbe("Run x", 1.0)
			on.blameProbe("Run x")
			gomega.Expect(on.probes.render()).To(gomega.ContainSubstring("Probe: Run x"))
		})

		ginkgo.It("is safe to call with a nil probes recorder", func() {
			nilRec := &Biloba{pollTrajectory: true, probes: nil}
			gomega.Expect(func() { nilRec.recordProbe("Run x", 1.0) }).NotTo(gomega.Panic())
			gomega.Expect(func() { nilRec.blameProbe("Run x") }).NotTo(gomega.Panic())
		})
	})
})
