package biloba

import (
	"context"

	"github.com/chromedp/chromedp"
	"github.com/onsi/biloba/engine"
)

/*
DeviceOption configures the emulated device in [Biloba.SetWindowSize]: see [Biloba.DeviceScaleFactor],
[Biloba.Mobile], [Biloba.TouchScreen], and [Biloba.Landscape].  It is chromedp's EmulateViewportOption,
so chromedp's own options work too.
*/
type DeviceOption = chromedp.EmulateViewportOption

/*
DeviceScaleFactor(factor) is a [DeviceOption] that sets the device pixel ratio: screenshots come back
at factor times the CSS size, and window.devicePixelRatio reports factor.

	b.SetWindowSize(1194, 834, b.DeviceScaleFactor(2))

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) DeviceScaleFactor(factor float64) DeviceOption { return chromedp.EmulateScale(factor) }

/*
Mobile() is a [DeviceOption] that emulates a mobile browser: the page's <meta name="viewport"> is
honoured, overlay scrollbars are used, and text may autosize.  A page without a viewport meta tag lays
out at 980 CSS pixels wide, as on a phone.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) Mobile() DeviceOption { return chromedp.EmulateMobile }

/*
TouchScreen() is a [DeviceOption] that emulates a touch screen: navigator.maxTouchPoints is positive,
touch events are supported, and the (pointer: coarse) and (hover: none) media queries match.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) TouchScreen() DeviceOption { return chromedp.EmulateTouch }

/*
Landscape() is a [DeviceOption] that reports a landscape screen orientation (screen.orientation).

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) Landscape() DeviceOption { return chromedp.EmulateLandscape }

/*
SetWindowSize() sets the window size for this tab.  A DeferCleanup is automatically registered to reset the window size after the spec ends.

Pass [DeviceOption]s to emulate a device:

	b.SetWindowSize(1194, 834, b.DeviceScaleFactor(2), b.Mobile(), b.TouchScreen(), b.Landscape())

The reset restores the previous size and turns the options off again.

Read https://onsi.github.io/biloba/#window-size to learn more about the window size
*/
func (b *Biloba) SetWindowSize(width, height int, opts ...DeviceOption) {
	b.gt.Helper()
	if b.refusedOnFrame("SetWindowSize") {
		return
	}
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
	err := b.runEngine("set the window size", func(ctx context.Context) error {
		return engine.EmulateViewportContext(ctx, width, height, opts...)
	})
	if err != nil {
		b.gt.Fatalf("failed to set window size: %s", err.Error())
	}

	b.gt.DeferCleanup(func() {
		resetOpts := []chromedp.EmulateViewportOption{}
		if b.ChromeConnection.HighFidelity {
			resetOpts = append(resetOpts, emulateViewportMatchingScreen)
		}
		err := b.runEngine("reset the window size", func(ctx context.Context) error {
			return engine.EmulateViewportContext(ctx, originalWidth, originalHeight, resetOpts...)
		})
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
	return b.tab().windowSize() // a frame reports its tab's window, as the TypeScript client does
}

// windowSize is the unguarded substrate behind WindowSize, used internally (e.g. by SetWindowSize)
// so the public config guard fires only for a user who misconfigures a WindowSize call directly.
func (b *Biloba) windowSize() (int, int) {
	b.gt.Helper()
	var dimensions []int
	b.run(`[window.innerWidth, window.innerHeight]`, &dimensions)
	return dimensions[0], dimensions[1]
}
