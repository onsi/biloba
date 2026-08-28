package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/onsi/gomega"
)

func TestRequestContextErrorPreservesDeadlineCause(t *testing.T) {
	g := gomega.NewWithT(t)
	requestCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-requestCtx.Done()

	err := requestContextError("evaluate", requestCtx, context.Canceled)

	g.Expect(errors.Is(err, context.DeadlineExceeded)).To(gomega.BeTrue())
	var engineErr *Error
	g.Expect(errors.As(err, &engineErr)).To(gomega.BeTrue())
	g.Expect(engineErr.Code).To(gomega.Equal(CodeDeadline))
}

// These are plain testing-based units (not Ginkgo specs) because dot-importing Gomega into package
// engine collides with the engine's own Assertion type.  They need no browser: the diagnostics
// outline just has to be truncated exactly the way the Go runner's Outline() truncates it (see the
// root package's outline.go) - same cap, same newline-boundary cut, same marker, same
// BILOBA_OUTLINE_MAX override.

func TestCapOutlineWithCap(t *testing.T) {
	g := gomega.NewWithT(t)

	// under the cap: left alone
	g.Expect(capOutlineWithCap("a\nb\nc", 1024)).To(gomega.Equal("a\nb\nc"))

	// over the cap: cut back to a newline boundary, marker appended
	truncated := capOutlineWithCap("a\nb\nc\nd\ne\nf", 5)
	g.Expect(truncated).To(gomega.HaveSuffix("\n... [truncated]"))
	g.Expect(truncated).NotTo(gomega.ContainSubstring("f"))

	// no newline to cut at: cut at the cap itself
	g.Expect(capOutlineWithCap("abcdefgh", 3)).To(gomega.Equal("abc\n... [truncated]"))

	// cap disabled
	g.Expect(capOutlineWithCap("a\nb\nc\nd\ne\nf", -1)).To(gomega.Equal("a\nb\nc\nd\ne\nf"))
}

func TestOutlineCap(t *testing.T) {
	for _, testCase := range []struct {
		env      string
		expected int
	}{
		{"", outlineMaxBytes},
		{"131072", 131072},
		{"0", -1},
		{"OFF", -1},
		{"none", -1},
		{"unlimited", -1},
		{"lots", outlineMaxBytes},
		{"-5", outlineMaxBytes},
	} {
		t.Run("BILOBA_OUTLINE_MAX="+testCase.env, func(t *testing.T) {
			g := gomega.NewWithT(t)
			t.Setenv("BILOBA_OUTLINE_MAX", testCase.env)
			g.Expect(outlineCap()).To(gomega.Equal(testCase.expected))
		})
	}
}
