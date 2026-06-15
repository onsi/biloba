package biloba

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const outlineMaxBytes = 32768 // 32 KB hard cap
const outlineTruncationMarker = "\n... [truncated]"

/*
Outline() returns the current page DOM as indented text suitable for reading or logging.
Script, style, and SVG element bodies are pruned (replaced with "…") to keep the output
compact; the surrounding tags are preserved. Runs of whitespace in text nodes are
collapsed to a single space. The total output is capped at ~32 KB; if truncated a
"... [truncated]" marker is appended.

Outline() is automatically attached as a report entry on spec failure so that the DOM
state at failure time is always readable — even in terminals or agents that cannot render
images.

Read https://onsi.github.io/biloba/#outline for details.
*/
func (b *Biloba) Outline() string {
	b.gt.Helper()
	b.ensureBiloba()
	resp := &bilobaJSResponse{}
	_, err := b.RunErr("_biloba.outline()", resp)
	if err != nil {
		b.gt.Fatalf("Failed to capture DOM outline:\n%s", err.Error())
		return ""
	}
	if resp.Error() != nil {
		b.gt.Fatalf("Failed to capture DOM outline:\n%s", resp.Error())
		return ""
	}
	return capOutline(resp.ResultString())
}

type tabOutline struct {
	title   string
	text    string
	failure string
}

func (b *Biloba) safeAllTabOutlines() []tabOutline {
	out := []tabOutline{}
	for _, tab := range b.AllTabs() {
		ctx, cancel := context.WithTimeout(tab.Context, time.Second)
		defer cancel()

		var title string
		if err := chromedp.Run(ctx, chromedp.Title(&title)); err != nil {
			out = append(out, tabOutline{failure: fmt.Sprintf("Failed to fetch title for DOM outline: %s", err.Error())})
			continue
		}
		if ctx.Err() != nil {
			out = append(out, tabOutline{failure: "Timed out attempting to capture DOM outline"})
			continue
		}

		resp := &bilobaJSResponse{}
		if _, err := tab.RunErr("_biloba.outline()", resp); err != nil || resp.Error() != nil {
			msg := ""
			if err != nil {
				msg = err.Error()
			} else {
				msg = resp.Error().Error()
			}
			out = append(out, tabOutline{failure: fmt.Sprintf("Failed to capture DOM outline for tab '%s': %s", title, msg)})
			continue
		}
		out = append(out, tabOutline{title: title, text: capOutline(resp.ResultString())})
	}
	return out
}

func capOutline(s string) string {
	return capOutlineWithCap(s, outlineMaxBytes)
}

func capOutlineWithCap(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Find a newline boundary near the cap so we don't cut mid-line.
	cut := strings.LastIndex(s[:maxBytes], "\n")
	if cut < 0 {
		cut = maxBytes
	}
	return s[:cut] + outlineTruncationMarker
}
