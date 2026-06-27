package biloba

import (
	"github.com/chromedp/chromedp"
)

/*
SetWindowSize() sets the window size for this tab.  A DeferCleanup is automatically registered to reset the window size after the spec ends
*/
func (b *Biloba) SetWindowSize(width, height int, opts ...chromedp.EmulateViewportOption) {
	b.gt.Helper()
	b.guardConfig("SetWindowSize")
	originalWidth, originalHeight := b.windowSize()
	// In high-fidelity mode the compositor's real input surface is clamped to a small virtual screen,
	// so we grow the emulated screen to match the viewport (see emulateViewportMatchingScreen) - this
	// keeps realistic-mode wheel/scroll input working all the way to the bottom of the resized
	// viewport.  We prepend it so caller-supplied opts can still override.  The default
	// (chrome-headless-shell) lane has no such clamp, so it's left alone.
	if b.ChromeConnection.HighFidelity {
		opts = append([]chromedp.EmulateViewportOption{emulateViewportMatchingScreen}, opts...)
	}
	err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(width), int64(height), opts...))
	if err != nil {
		b.gt.Fatalf("failed to set window size: %s", err.Error())
	}

	b.gt.DeferCleanup(func() {
		resetOpts := []chromedp.EmulateViewportOption{chromedp.EmulatePortrait}
		if b.ChromeConnection.HighFidelity {
			resetOpts = append(resetOpts, emulateViewportMatchingScreen)
		}
		err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(originalWidth), int64(originalHeight), resetOpts...))
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
	b.guardConfig("WindowSize")
	return b.windowSize()
}

// windowSize is the unguarded substrate behind WindowSize, used internally (e.g. by SetWindowSize)
// so the public config guard fires only for a user who misconfigures a WindowSize call directly.
func (b *Biloba) windowSize() (int, int) {
	b.gt.Helper()
	var dimensions []int
	b.run(`[window.innerWidth, window.innerHeight]`, &dimensions)
	return dimensions[0], dimensions[1]
}
