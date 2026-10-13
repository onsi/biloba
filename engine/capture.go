package engine

import (
	"context"
	"fmt"
	"math"

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
func PageCaptureAction(img *[]byte, cssWidth *float64) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		_, _, _, cssLayoutViewport, _, cssContentSize, err := page.GetLayoutMetrics().Do(ctx)
		if err != nil {
			return err
		}
		if cssWidth != nil {
			*cssWidth = cssContentSize.Width
		}
		fits := cssLayoutViewport != nil &&
			cssContentSize.Width == float64(cssLayoutViewport.ClientWidth) &&
			cssContentSize.Height == float64(cssLayoutViewport.ClientHeight) &&
			cssContentSize.X == 0 && cssContentSize.Y == 0
		*img, err = page.CaptureScreenshot().
			WithFromSurface(true).
			WithCaptureBeyondViewport(!fits).
			WithFormat(page.CaptureScreenshotFormatPng).
			Do(ctx)
		return err
	}
}

// CapturePageContext captures the whole page.
func CapturePageContext(ctx context.Context, cssWidth *float64) ([]byte, error) {
	var img []byte
	if err := chromedp.Run(ctx, PageCaptureAction(&img, cssWidth)); err != nil {
		return nil, err
	}
	return img, nil
}

// CaptureClipContext captures one region of the page.
func CaptureClipContext(ctx context.Context, clip *page.Viewport, beyondViewport bool) ([]byte, error) {
	var img []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var captureErr error
		img, captureErr = page.CaptureScreenshot().
			WithClip(clip).
			WithFromSurface(true).
			WithCaptureBeyondViewport(beyondViewport).
			Do(runCtx)
		return captureErr
	}))
	if err != nil {
		return nil, err
	}
	return img, nil
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
	var img []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		capture := page.CaptureScreenshot().WithFromSurface(true).WithCaptureBeyondViewport(false).WithFormat(page.CaptureScreenshotFormatPng)
		if region != nil {
			_, _, _, _, cssVisualViewport, _, err := page.GetLayoutMetrics().Do(runCtx)
			if err != nil {
				return err
			}
			clip, err := ViewportRegionClip(region, cssVisualViewport.PageX, cssVisualViewport.PageY, cssVisualViewport.ClientWidth, cssVisualViewport.ClientHeight)
			if err != nil {
				return err
			}
			capture = capture.WithClip(clip)
		}
		var err error
		img, err = capture.Do(runCtx)
		return err
	}))
	return img, err
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
func CaptureFullScreenshotContext(ctx context.Context, quality int) (img []byte, title string, err error) {
	err = chromedp.Run(ctx,
		chromedp.Title(&title),
		chromedp.FullScreenshot(&img, quality),
	)
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
