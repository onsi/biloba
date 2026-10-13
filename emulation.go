package biloba

import (
	"context"
	"fmt"

	"github.com/onsi/biloba/engine"
)

/*
Media is the CSS media a tab emulates - see [Biloba.SetMedia].  An empty field leaves that aspect to
the browser:

  - Type is the media type: "screen", "print", or "".
  - ColorScheme drives prefers-color-scheme: "light", "dark", or "".
  - ReducedMotion drives prefers-reduced-motion: "reduce", "no-preference", or "".

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
type Media struct {
	Type          string
	ColorScheme   string
	ReducedMotion string
}

func (m Media) validate() error {
	for _, field := range []struct {
		name, value string
		allowed     []string
	}{
		{"Type", m.Type, []string{"", "screen", "print"}},
		{"ColorScheme", m.ColorScheme, []string{"", "light", "dark"}},
		{"ReducedMotion", m.ReducedMotion, []string{"", "reduce", "no-preference"}},
	} {
		ok := false
		for _, allowed := range field.allowed {
			ok = ok || field.value == allowed
		}
		if !ok {
			return fmt.Errorf("Media.%s must be one of %q, got %q", field.name, field.allowed[1:], field.value)
		}
	}
	return nil
}

/*
SetMedia() emulates CSS media for this tab: prefers-color-scheme, prefers-reduced-motion, and the
media type.  It replaces whatever media the tab emulated before, holds across navigations, and is
undone by a DeferCleanup when the spec ends:

	b.SetMedia(biloba.Media{ColorScheme: "dark", ReducedMotion: "reduce"})

[Biloba.HaveScreenshot] with [Biloba.InColorSchemes] overrides only the colour scheme for its own
captures and restores this media afterwards.  SetMedia applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetMedia(media Media) {
	b.gt.Helper()
	if b.refusedOnFrame("SetMedia") {
		return
	}
	b.guardConfig("SetMedia")
	if err := media.validate(); err != nil {
		b.gt.Fatalf("SetMedia: %s", err.Error())
		return
	}
	previous := b.media.media
	if err := b.setMedia(media); err != nil {
		b.gt.Fatalf("failed to set the emulated media: %s", err.Error())
		return
	}
	b.gt.DeferCleanup(func() {
		if err := b.setMedia(previous); err != nil {
			b.gt.Fatalf("failed to reset the emulated media: %s", err.Error())
		}
	})
}

// setMedia makes media the tab's own emulated media - the media a HaveScreenshot colour scheme is
// layered over and restored to.
func (b *Biloba) setMedia(media Media) error {
	err := b.media.apply(media, func(m engine.Media) error {
		return b.runEngine("emulate media", func(ctx context.Context) error {
			return engine.EmulateMediaContext(ctx, m)
		})
	})
	if err == nil {
		b.media.media = media
	}
	return err
}

// mediaEmulation is a tab's emulated CSS media, shared by every view and frame handle of the tab.
type mediaEmulation struct {
	// media is the tab's own media, as set by SetMedia.
	media Media
	// outstanding records that the target may carry an override.  It goes up BEFORE any command that
	// sets one (a command that reports an error may still have landed) and only comes down when a
	// clear actually succeeds - which is what lets Prepare() skip the round trip when there is nothing
	// to clear.
	outstanding bool
}

// apply sends media to the target through run, keeping outstanding honest.
func (e *mediaEmulation) apply(media Media, run func(engine.Media) error) error {
	clearing := media == Media{}
	if !clearing {
		e.outstanding = true
	}
	err := run(engine.Media{Type: media.Type, ColorScheme: media.ColorScheme, ReducedMotion: media.ReducedMotion})
	if clearing && err == nil {
		e.outstanding = false
	}
	return err
}

// clearEmulatedMedia is Prepare()'s half of the media teardown, called from resetBrowsingState.  The
// override outlives Navigate, so a SetMedia whose cleanup did not land, or a screenshot capture whose
// restore did not (an expired capture timeout, a cancelled context), would otherwise leave every later
// spec in this process rendering with it.
func (b *Biloba) clearEmulatedMedia() {
	b.media.media = Media{}
	if !b.media.outstanding {
		return
	}
	if err := b.setMedia(Media{}); err != nil {
		b.gt.Printf("Failed to clear leaked media emulation during Prepare():\n  %s\n", err.Error())
	}
}
