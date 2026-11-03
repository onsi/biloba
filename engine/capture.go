package engine

import (
	"context"
	"fmt"
	"math"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// PageCaptureAction captures the whole page, expanding the viewport only when the document is
// actually bigger than it.
//
// The exact-equality test is the load-bearing part.  Anything looser - "fits within" - and the
// expanded capture and the plain one could differ in size, which would invalidate every baseline
// taken with the other one.  Chrome reports both in CSS pixels off the same layout, so a document
// that measures equal really is the viewport.  When the content already fits, the two captures are
// the same pixels, so skipping the expansion changes nothing except that the page stops being told
// its viewport resized - which is the app-shell case, a document that never scrolls because an
// inner pane does.
//
// It returns an Action rather than doing the work so callers can compose it into a Run alongside
// their own steps, which is how the visual-regression path settles a capture.
func PageCaptureAction(img *[]byte, cssWidth *float64) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		metrics, err := cdp.Call(ctx, t, page.GetLayoutMetrics, cdp.Empty{})
		if err != nil {
			return err
		}
		cssLayoutViewport, cssContentSize := metrics.CSSLayoutViewport, metrics.CSSContentSize
		if cssWidth != nil {
			*cssWidth = cssContentSize.Width
		}
		fits := cssLayoutViewport != nil &&
			cssContentSize.Width == float64(cssLayoutViewport.ClientWidth) &&
			cssContentSize.Height == float64(cssLayoutViewport.ClientHeight) &&
			cssContentSize.X == 0 && cssContentSize.Y == 0
		shot, err := cdp.Call(ctx, t, page.CaptureScreenshot, page.CaptureScreenshotParams{
			FromSurface:           ptr(true),
			CaptureBeyondViewport: ptr(!fits),
			Format:                page.CaptureScreenshotFormatPng,
		})
		*img = shot.Data
		return err
	})
}

// CapturePageContext captures the whole page.
func CapturePageContext(ctx context.Context, cssWidth *float64) ([]byte, error) {
	var img []byte
	if err := chromedp.Do(ctx, PageCaptureAction(&img, cssWidth)); err != nil {
		return nil, err
	}
	return img, nil
}

// CaptureClipContext captures one region of the page.
func CaptureClipContext(ctx context.Context, clip *page.Viewport, beyondViewport bool) ([]byte, error) {
	shot, err := chromedp.Call(ctx, page.CaptureScreenshot, page.CaptureScreenshotParams{
		Clip:                  clip,
		FromSurface:           ptr(true),
		CaptureBeyondViewport: ptr(beyondViewport),
	})
	if err != nil {
		return nil, err
	}
	return shot.Data, nil
}

// EnclosingClip rounds an element's clip outward to whole CSS pixels.  An element's box is usually
// fractional, and Chrome drops a partial pixel at the clip's far edge, so an unrounded capture comes
// back a row or column short of the element.  The small epsilon keeps a box that is a whole number of
// pixels up to floating-point noise from growing by one.
func EnclosingClip(clip *page.Viewport) *page.Viewport {
	const epsilon = 1e-3
	x, y := math.Floor(clip.X+epsilon), math.Floor(clip.Y+epsilon)
	x2, y2 := math.Ceil(clip.X+clip.Width-epsilon), math.Ceil(clip.Y+clip.Height-epsilon)
	return &page.Viewport{X: x, Y: y, Width: x2 - x, Height: y2 - y, Scale: clip.Scale}
}

// CaptureViewportContext captures what the viewport shows: region, in viewport CSS pixels, or the
// whole viewport when region is nil.  Unlike a full-page capture it never expands the viewport, so the
// page is not laid out again at a different size.  A region that reaches outside the viewport is an
// error.
func CaptureViewportContext(ctx context.Context, region *page.Viewport) ([]byte, error) {
	capture := page.CaptureScreenshotParams{FromSurface: ptr(true), CaptureBeyondViewport: ptr(false), Format: page.CaptureScreenshotFormatPng}
	if region != nil {
		viewport, err := visualViewport(ctx)
		if err != nil {
			return nil, err
		}
		clip, err := ViewportRegionClip(region, viewport.X, viewport.Y, viewport.Width, viewport.Height)
		if err != nil {
			return nil, err
		}
		capture.Clip = clip
	}
	shot, err := chromedp.Call(ctx, page.CaptureScreenshot, capture)
	return shot.Data, err
}

// VisualViewportContext reports the visual viewport: where its top-left corner sits in page
// coordinates (X, Y) and its size, all in CSS pixels.
func VisualViewportContext(ctx context.Context) (*page.Viewport, error) {
	return visualViewport(ctx)
}

func visualViewport(ctx context.Context) (*page.Viewport, error) {
	metrics, err := chromedp.Call(ctx, page.GetLayoutMetrics, cdp.Empty{})
	if err != nil {
		return nil, err
	}
	cssVisualViewport := metrics.CSSVisualViewport
	return &page.Viewport{X: cssVisualViewport.PageX, Y: cssVisualViewport.PageY, Width: cssVisualViewport.ClientWidth, Height: cssVisualViewport.ClientHeight, Scale: 1}, nil
}

// ViewportRegionClip turns region, in the CSS pixels of a viewport whose top-left sits at (originX,
// originY) in page coordinates, into a page-coordinate clip - refusing a region that is empty or
// reaches outside the viewport's width and height, since nothing was painted there.
func ViewportRegionClip(region *page.Viewport, originX, originY, width, height float64) (*page.Viewport, error) {
	if region.Width <= 0 || region.Height <= 0 {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "capture a viewport region", Message: "the region must have a positive width and height"}
	}
	if region.X < 0 || region.Y < 0 || region.X+region.Width > width || region.Y+region.Height > height {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "capture a viewport region", Message: fmt.Sprintf("the region (%g, %g, %g x %g) reaches outside the %g x %g viewport", region.X, region.Y, region.Width, region.Height, width, height)}
	}
	return &page.Viewport{X: originX + region.X, Y: originY + region.Y, Width: region.Width, Height: region.Height, Scale: 1}, nil
}

// CaptureFullScreenshotContext captures the page at full size and reads its title in the same round
// trip, which is what the on-failure artifact path wants: a picture and something to label it with.
// It always expands the viewport, so the page sees a resize: it is for a spec that has already failed.
func CaptureFullScreenshotContext(ctx context.Context) (img []byte, title string, err error) {
	if title, err = chromedp.Run(ctx, chromedp.Title()); err != nil {
		return nil, "", err
	}
	img, err = chromedp.Run(ctx, chromedp.FullScreenshot(100))
	return img, title, err
}

// CaptureViewportScreenshotContext is CaptureFullScreenshotContext for a spec that is still running: it
// captures only what the viewport shows, so the page cannot tell it happened.
func CaptureViewportScreenshotContext(ctx context.Context) (img []byte, title string, err error) {
	if title, err = chromedp.Run(ctx, chromedp.Title()); err != nil {
		return nil, "", err
	}
	img, err = CaptureViewportContext(ctx, nil)
	return img, title, err
}

// EmulateColorSchemeContext overrides the page's prefers-color-scheme, or clears the override when
// scheme is empty.  Callers track whether an override is outstanding themselves: a command that
// reports an error may still have landed, so the flag has to go up before the call and only come
// down when a clear actually succeeds.
func EmulateColorSchemeContext(ctx context.Context, scheme string) error {
	return EmulateMediaContext(ctx, Media{ColorScheme: scheme})
}

// EmulateMediaContext replaces the page's emulated CSS media with media; the zero Media clears every
// override.  The override is per target and outlives navigation, so the same outstanding-override
// bookkeeping applies as for EmulateColorSchemeContext.
func EmulateMediaContext(ctx context.Context, media Media) error { return setMedia(ctx, media) }
