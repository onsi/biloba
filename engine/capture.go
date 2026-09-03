package engine

import (
	"context"

	"github.com/chromedp/cdproto/emulation"
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
	params := emulation.SetEmulatedMedia()
	if scheme != "" {
		params = params.WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}})
	}
	return chromedp.Run(ctx, params)
}
