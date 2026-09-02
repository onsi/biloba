package biloba

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onsi/gomega/types"
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
// It deliberately tracks a SINGLE series keyed by the probe (a getter/matcher's method+selector): when
// the key changes, the prior series is one that already resolved and moved on, so it is superseded.
//
// A current series is not the same thing as the series that FAILED, though, and rendering the former as
// the latter is worse than rendering nothing: a read that passed, attached to an unrelated failure with
// the "value never changed" diagnosis over it, is a confident wrong answer.  So a series is rendered
// only once it has been BLAMED - see probingMatcher, which stamps blame at the one moment Gomega tells
// Biloba an assertion failed.  No blame, no entry.  Shared across a tab's clone-with-a-flag views via a
// pointer.
type probeRecorder struct {
	mu       sync.Mutex
	key      string
	start    time.Time
	segments []probeSegment
	dropped  int
	blamed   string // the key of the read whose assertion failed; only that series is ever rendered
	match    matchTrail
}

// matchTrail is the presence axis of the current series - probeShape reads a numeric trajectory for
// direction; this reads the same trajectory for whether the selector RESOLVED.  It answers the one
// question a plain "never matched" failure cannot: did this selector match earlier in the poll and
// then stop?  Matched-then-stopped is the detached-node signature (the node was replaced, or its
// identifying attribute was swapped in place) and reads identically to "never matched" otherwise.
// Like the value series it is a SINGLE series: a new selector supersedes the prior (resolved) one.
type matchTrail struct {
	key       string        // the encoded selector this trail follows
	display   string        // the selector as the user wrote it (for the message)
	start     time.Time     // when this trail began
	firstAt   time.Duration // elapsed at the first match
	lastAt    time.Duration // elapsed at the most recent match
	matched   int           // how many samples resolved the selector
	unmatched bool          // the most recent sample did NOT resolve it
}

// recordProbe appends value to this tab's trajectory under key, when the suite has opted into
// trajectory recording (BilobaConfigPollTrajectory).  It is a cheap no-op otherwise.
//
// Recording alone never surfaces anything: the series is rendered only if the assertion polling this
// same key goes on to fail, which is what [Biloba.probing] arranges.  So every read that records must
// also be wrapped in b.probing(<the same method name>, ...) - otherwise its trajectory is dead weight.
func (b *Biloba) recordProbe(key string, value any) {
	if !b.pollTrajectory || b.probes == nil {
		return
	}
	b.probes.record(key, value, time.Now())
}

// probingMatcher wraps a matcher that records a trajectory and stamps the recorder with blame when
// Gomega asks it for a failure message.  Gomega asks exactly once, at the moment an assertion fails
// (FailureMessage for a failed Should, NegatedFailureMessage for a failed ShouldNot) and never when one
// passes - which is the only signal Biloba gets that THIS read is the one the spec died on.  Without it
// the recorder could only offer "the most recent series", and a read that resolved and moved on would be
// attributed to whatever failed next.
type probingMatcher struct {
	types.GomegaMatcher
	b      *Biloba
	method string
}

func (p probingMatcher) FailureMessage(actual any) string {
	p.b.blameProbe(probeKey(p.method, actual))
	return p.GomegaMatcher.FailureMessage(actual)
}

func (p probingMatcher) NegatedFailureMessage(actual any) string {
	p.b.blameProbe(probeKey(p.method, actual))
	return p.GomegaMatcher.NegatedFailureMessage(actual)
}

// probing wraps matcher so that a failed assertion over it claims this tab's trajectory for method +
// the selector it was polled with - the same key the matcher records under.  It is the identity when
// trajectory recording is off, so a suite that opted out pays nothing.
func (b *Biloba) probing(method string, matcher types.GomegaMatcher) types.GomegaMatcher {
	if !b.pollTrajectory || b.probes == nil {
		return matcher
	}
	return probingMatcher{GomegaMatcher: matcher, b: b, method: method}
}

// blameProbe marks key as the read whose assertion failed.
func (b *Biloba) blameProbe(key string) {
	if !b.pollTrajectory || b.probes == nil {
		return
	}
	b.probes.blame(key)
}

func (p *probeRecorder) blame(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blamed = key
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

// recordMatch folds one poll sample's presence verdict into this tab's match trail, when the suite has
// opted into trajectory recording (BilobaConfigPollTrajectory).  Cheap no-op otherwise; it rides
// runBilobaHandler, which already knows whether the selector resolved (biloba.js's `found`).
func (b *Biloba) recordMatch(key, display string, found bool) {
	if !b.pollTrajectory || b.probes == nil {
		return
	}
	b.probes.recordMatch(key, display, found, time.Now())
}

func (p *probeRecorder) recordMatch(key, display string, found bool, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key != p.match.key {
		// a new selector supersedes the prior (resolved) trail
		p.match = matchTrail{key: key, display: display, start: now}
	}
	elapsed := now.Sub(p.match.start)
	if found {
		if p.match.matched == 0 {
			p.match.firstAt = elapsed
		}
		p.match.matched++
		p.match.lastAt = elapsed
	}
	p.match.unmatched = !found
}

// renderDetachedNode returns the detached-node annotation when the current trail matched at least once
// and then stopped matching, and "" otherwise.  A selector that NEVER matched gets nothing - the
// ordinary "could not find DOM element matching selector" failure already says that, and a diagnostic
// that fires on the ordinary case is noise.
func (p *probeRecorder) renderDetachedNode() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.match.matched == 0 || !p.match.unmatched {
		return ""
	}
	return fmt.Sprintf("⚠ Selector %q matched %d× during this poll (+%ss to +%ss) then stopped matching\n  — the node was likely replaced, or its identifying attribute changed in place.\n",
		p.match.display, p.match.matched, roundDuration(p.match.firstAt), roundDuration(p.match.lastAt))
}

// reset drops both series - the value trajectory and the match trail - along with the blame stamp, so a
// diagnosis can't leak from one spec into the next.
func (p *probeRecorder) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key, p.start, p.segments, p.dropped, p.blamed = "", time.Time{}, nil, 0, ""
	p.match = matchTrail{}
}

// render returns the human-readable trajectory for the current series, or "" when nothing was recorded
// or when the current series is not the one that failed (see probeRecorder's blame stamp).
func (p *probeRecorder) render() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.segments) == 0 || p.blamed == "" || p.blamed != p.key {
		return ""
	}

	total := 0
	for _, s := range p.segments {
		total += s.count
	}
	last := p.segments[len(p.segments)-1]

	out := &strings.Builder{}
	fmt.Fprintf(out, "Probe: %s\n", p.key)
	fmt.Fprintf(out, "%d samples over %ss, %d distinct values%s:\n", total, roundDuration(last.lastAt), len(p.segments)+p.dropped, probeShape(p.segments, total))
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
func probeShape(segments []probeSegment, total int) string {
	if len(segments) == 1 {
		if total < 2 {
			// one sample is not a trajectory: it cannot tell "never changed" from "only read once"
			return ""
		}
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
	case BoxDelta:
		return fmt.Sprintf("BoxDelta{Top:%g Left:%g CenterX:%g CenterY:%g Width:%g Height:%g}", v.Top, v.Left, v.CenterX, v.CenterY, v.Width, v.Height)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func roundDuration(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 2, 64)
}

// resetPollDiagnostics clears the per-spec poll diagnostics - the value trajectory and the match trail
// behind the detached-node signal, and the occluded-click ring - across the root tab and every
// registered tab.
// attachFailureArtifactsIfFailed calls it on the way out of every spec (pass or fail).  It walks the
// registered-tab map rather than AllTabs() deliberately: AllTabs costs a CDP round-trip, and this
// runs on the happy path too.
func (b *Biloba) resetPollDiagnostics() {
	root := b.root
	if root == nil {
		root = b
	}
	root.lock.Lock()
	tabs := make([]*Biloba, 0, len(root.tabs)+1)
	tabs = append(tabs, root)
	for _, tab := range root.tabs {
		tabs = append(tabs, tab)
	}
	root.lock.Unlock()
	for _, tab := range tabs {
		if tab.probes != nil {
			tab.probes.reset()
		}
		if tab.occlusions != nil {
			tab.occlusions.reset()
		}
	}
}

// probeKey builds the recorder key for a getter/matcher: method name + the (already s/x-encoded)
// selector, so successive polls of the same getter+selector accumulate into one series while a switch
// to a different getter or selector starts a fresh one.
func probeKey(method string, selector any) string {
	return method + " " + fmt.Sprintf("%v", selector)
}
