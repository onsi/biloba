package biloba

import (
	"github.com/chromedp/chromedp"
)

/*
SetWindowSize() sets the window size for this tab.  A DeferCleanup is automatically registered to reset the window size after the spec ends
*/
func (b *Biloba) SetWindowSize(width, height int, opts ...chromedp.EmulateViewportOption) {
	originalWidth, originalHeight := b.WindowSize()
	b.gt.Helper()
	err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(width), int64(height), opts...))
	if err != nil {
		b.gt.Fatalf("failed to set window size: %s", err.Error())
	}

	b.gt.DeferCleanup(func() {
		err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(originalWidth), int64(originalHeight), chromedp.EmulatePortrait))
		if err != nil {
			b.gt.Fatalf("failed to reset window size: %s", err.Error())
		}
	})
}

/*
WindowSize() returns the current window size of this tab.
*/
func (b *Biloba) WindowSize() (int, int) {
	b.gt.Helper()
	var dimensions []int
	b.Run(`[window.innerWidth, window.innerHeight]`, &dimensions)
	return dimensions[0], dimensions[1]
}
