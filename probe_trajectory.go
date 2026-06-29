package biloba

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxProbeSegments bounds the number of distinct-value segments kept for the current series.  Equal
// values collapse into one segment (run-length), so a flat "product never re-evaluated" trajectory is
// a single segment regardless of poll count; only a genuinely oscillating value approaches the cap.
// When exceeded the oldest segments are dropped (and counted) so the most recent tail is always kept.
const maxProbeSegments = 50

// probeSegment is one run of consecutive identical polled values: the rendered value, the elapsed time
// at which it was first seen (relative to the series start), the elapsed time it was last seen, and how
// many consecutive samples carried it.  This is the "delta + timestamp + intervening count" view.
type probeSegment struct {
	value   string
	firstAt time.Duration
	lastAt  time.Duration
	count   int
}

// probeRecorder records the (elapsed, value) trajectory of the most recently polled entity for one tab.
// It deliberately tracks a SINGLE series keyed by the probe (a Run script, or a getter's method+selector):
// when the key changes, the prior series is one that already resolved and moved on, so it is superseded.
// On failure attachFailureArtifactsIfFailed renders whatever series is current - almost always the
// Eventually that actually timed out.  Shared across a tab's clone-with-a-flag views via a pointer.
type probeRecorder struct {
	mu       sync.Mutex
	key      string
	start    time.Time
	segments []probeSegment
	dropped  int
}

// recordProbe appends value to this tab's trajectory under key, when the suite has opted into
// trajectory recording (BilobaConfigPollTrajectory).  It is a cheap no-op otherwise.
func (b *Biloba) recordProbe(key string, value any) {
	if !b.pollTrajectory || b.probes == nil {
		return
	}
	b.probes.record(key, value, time.Now())
}

func (p *probeRecorder) record(key string, value any, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rendered := renderProbeValue(value)
	if key != p.key {
		// a new polled entity supersedes the prior (resolved) series
		p.key = key
		p.start = now
		p.segments = nil
		p.dropped = 0
	}
	elapsed := now.Sub(p.start)
	if n := len(p.segments); n > 0 && p.segments[n-1].value == rendered {
		p.segments[n-1].count++
		p.segments[n-1].lastAt = elapsed
		return
	}
	p.segments = append(p.segments, probeSegment{value: rendered, firstAt: elapsed, lastAt: elapsed, count: 1})
	if len(p.segments) > maxProbeSegments {
		p.segments = p.segments[1:]
		p.dropped++
	}
}

// render returns the human-readable trajectory for the current series, or "" when nothing was recorded.
func (p *probeRecorder) render() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.segments) == 0 {
		return ""
	}

	total := 0
	for _, s := range p.segments {
		total += s.count
	}
	last := p.segments[len(p.segments)-1]

	out := &strings.Builder{}
	fmt.Fprintf(out, "Probe: %s\n", p.key)
	fmt.Fprintf(out, "%d samples over %ss, %d distinct values%s:\n", total, roundDuration(last.lastAt), len(p.segments)+p.dropped, probeShape(p.segments))
	if p.dropped > 0 {
		fmt.Fprintf(out, "  (%d earlier value-changes elided)\n", p.dropped)
	}
	for _, s := range p.segments {
		fmt.Fprintf(out, "  +%-6s %s", roundDuration(s.firstAt)+"s", s.value)
		if s.count > 1 {
			fmt.Fprintf(out, "   (held ×%d through +%ss)", s.count, roundDuration(s.lastAt))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// probeShape annotates the trajectory with the at-a-glance diagnosis the feedback called for: a single
// segment that never changed = the product computed once and never reconciled; a monotone approach =
// latency (it was getting there); a non-monotone series = a late reflow/rebound shoved it back.  Only
// emitted for numeric series (where direction is meaningful).
func probeShape(segments []probeSegment) string {
	if len(segments) == 1 {
		return " — flat (value never changed: the page is not re-evaluating this probe)"
	}
	nums := make([]float64, 0, len(segments))
	for _, s := range segments {
		f, err := strconv.ParseFloat(s.value, 64)
		if err != nil {
			return "" // non-numeric: no direction to read
		}
		nums = append(nums, f)
	}
	up, down := false, false
	for i := 1; i < len(nums); i++ {
		if nums[i] > nums[i-1] {
			up = true
		} else if nums[i] < nums[i-1] {
			down = true
		}
	}
	if up && down {
		return " — non-monotone (dip-then-rebound: a late reflow likely shoved it back)"
	}
	return " — monotone (a clean approach: latency, it was nearly there)"
}

// renderProbeValue renders a recorded value compactly.  Numbers print without scientific notation or a
// trailing exponent so a numeric series stays parseable by probeShape; structs print with their fields.
func renderProbeValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "<nil>"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case Box:
		return fmt.Sprintf("Box{Top:%g Left:%g Width:%g Height:%g}", v.Top, v.Left, v.Width, v.Height)
	case ScrollOffset:
		return fmt.Sprintf("ScrollOffset{Top:%g Left:%g MaxTop:%g MaxLeft:%g}", v.Top, v.Left, v.MaxTop, v.MaxLeft)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func roundDuration(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 2, 64)
}

// probeKey builds the recorder key for a getter/matcher: method name + the (already s/x-encoded)
// selector, so successive polls of the same getter+selector accumulate into one series while a switch
// to a different getter or selector starts a fresh one.
func probeKey(method string, selector any) string {
	return method + " " + fmt.Sprintf("%v", selector)
}
