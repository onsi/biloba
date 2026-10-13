package engine

import (
	"context"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

type DeviceMetrics struct {
	Width             int
	Height            int
	DeviceScaleFactor float64
	Mobile            bool
	Touch             bool
	Landscape         bool
}

type Geolocation struct{ Latitude, Longitude, Accuracy float64 }

type Permission string

const (
	PermissionGeolocation   Permission = "geolocation"
	PermissionNotifications Permission = "notifications"
	PermissionCamera        Permission = "videoCapture"
	PermissionMicrophone    Permission = "audioCapture"
)

type PermissionState string

const (
	PermissionGranted PermissionState = "granted"
	PermissionDenied  PermissionState = "denied"
	PermissionPrompt  PermissionState = "prompt"
)

type Media struct {
	Type          string
	ColorScheme   string
	ReducedMotion string
}

func (s *Session) SetDeviceMetrics(ctx context.Context, metrics DeviceMetrics) error {
	if err := s.tabOnly("set device metrics"); err != nil {
		return err
	}
	if metrics.Width <= 0 || metrics.Height <= 0 || metrics.DeviceScaleFactor <= 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set device metrics", Message: "width, height, and device scale factor must be positive"}
	}
	return s.serial(ctx, "set device metrics", func(opCtx context.Context) error {
		override := emulation.SetDeviceMetricsOverride(int64(metrics.Width), int64(metrics.Height), metrics.DeviceScaleFactor, metrics.Mobile)
		if metrics.Landscape {
			override = override.WithScreenOrientation(&emulation.ScreenOrientation{Type: emulation.OrientationTypeLandscapePrimary, Angle: 90})
		}
		s.emulated.deviceMetrics = true
		return chromedp.Run(opCtx, override, emulation.SetTouchEmulationEnabled(metrics.Touch))
	})
}

func (s *Session) ClearDeviceMetrics(ctx context.Context) error {
	if err := s.tabOnly("clear device metrics"); err != nil {
		return err
	}
	return s.serial(ctx, "clear device metrics", func(opCtx context.Context) error {
		return s.clearOverride(&s.emulated.deviceMetrics, opCtx, clearDeviceMetrics)
	})
}
func clearDeviceMetrics(ctx context.Context) error {
	return chromedp.Run(ctx, emulation.ClearDeviceMetricsOverride(), emulation.SetTouchEmulationEnabled(false))
}

func (s *Session) SetGeolocation(ctx context.Context, location Geolocation) error {
	if err := s.tabOnly("set geolocation"); err != nil {
		return err
	}
	if location.Latitude < -90 || location.Latitude > 90 || location.Longitude < -180 || location.Longitude > 180 || location.Accuracy < 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set geolocation", Message: "latitude, longitude, or accuracy is outside its valid range"}
	}
	return s.serial(ctx, "set geolocation", func(opCtx context.Context) error {
		s.emulated.geolocation = true
		return chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
			// Pointers preserve valid zero coordinates. The generated CDP params use omitzero and would
			// otherwise turn latitude/longitude 0 into "position unavailable".
			params := struct {
				Latitude  *float64 `json:"latitude"`
				Longitude *float64 `json:"longitude"`
				Accuracy  *float64 `json:"accuracy"`
			}{Latitude: &location.Latitude, Longitude: &location.Longitude, Accuracy: &location.Accuracy}
			return cdp.Execute(runCtx, emulation.CommandSetGeolocationOverride, &params, nil)
		}))
	})
}

func (s *Session) ClearGeolocation(ctx context.Context) error {
	if err := s.tabOnly("clear geolocation"); err != nil {
		return err
	}
	return s.serial(ctx, "clear geolocation", func(opCtx context.Context) error {
		return s.clearOverride(&s.emulated.geolocation, opCtx, clearGeolocation)
	})
}
func clearGeolocation(ctx context.Context) error {
	return chromedp.Run(ctx, emulation.ClearGeolocationOverride())
}

func (s *Session) SetPermissions(ctx context.Context, origin string, permissions map[Permission]PermissionState) error {
	if err := s.tabOnly("set permissions"); err != nil {
		return err
	}
	return s.serial(ctx, "set permissions", func(opCtx context.Context) error {
		// Permissions belong to the browser context, which outlives a spawned tab: the session that
		// owns the context resets them.
		s.contextOwner().permissionsSet.Store(true)
		return s.withBrowserExecutor(opCtx, func(browserCtx context.Context) error {
			for permission, state := range permissions {
				setting := cdpbrowser.PermissionSetting(state)
				if setting != cdpbrowser.PermissionSettingGranted && setting != cdpbrowser.PermissionSettingDenied && setting != cdpbrowser.PermissionSettingPrompt {
					return &Error{Code: CodeInvalidArgument, Operation: "set permissions", Message: "permission state must be granted, denied, or prompt", Observed: state}
				}
				descriptor := &cdpbrowser.PermissionDescriptor{Name: string(permission)}
				if err := cdpbrowser.SetPermission(descriptor, setting).WithOrigin(origin).WithBrowserContextID(s.browserContextID).Do(browserCtx); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func (s *Session) ResetPermissions(ctx context.Context) error {
	if err := s.tabOnly("reset permissions"); err != nil {
		return err
	}
	return s.serial(ctx, "reset permissions", s.resetPermissions)
}
func (s *Session) resetPermissions(ctx context.Context) error {
	err := s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
		return cdpbrowser.ResetPermissions().WithBrowserContextID(s.browserContextID).Do(browserCtx)
	})
	if err == nil {
		s.contextOwner().permissionsSet.Store(false)
	}
	return err
}

// contextOwner is the session that owns this session's browser context and resets what lives on it.
func (s *Session) contextOwner() *Session {
	if s.root != nil {
		return s.root
	}
	return s
}

func (s *Session) SetLocale(ctx context.Context, locale string) error {
	if err := s.tabOnly("set locale"); err != nil {
		return err
	}
	return s.serial(ctx, "set locale", func(opCtx context.Context) error {
		return s.override(&s.emulated.locale, locale != "", opCtx, func(c context.Context) error { return setLocale(c, locale) })
	})
}
func (s *Session) ClearLocale(ctx context.Context) error { return s.SetLocale(ctx, "") }
func setLocale(ctx context.Context, locale string) error {
	return chromedp.Run(ctx, emulation.SetLocaleOverride().WithLocale(locale))
}

func (s *Session) SetTimezone(ctx context.Context, timezone string) error {
	if err := s.tabOnly("set timezone"); err != nil {
		return err
	}
	return s.serial(ctx, "set timezone", func(opCtx context.Context) error {
		return s.override(&s.emulated.timezone, timezone != "", opCtx, func(c context.Context) error { return setTimezone(c, timezone) })
	})
}
func (s *Session) ClearTimezone(ctx context.Context) error { return s.SetTimezone(ctx, "") }
func setTimezone(ctx context.Context, timezone string) error {
	return chromedp.Run(ctx, emulation.SetTimezoneOverride(timezone))
}

func (s *Session) SetMedia(ctx context.Context, media Media) error {
	if err := s.tabOnly("set media"); err != nil {
		return err
	}
	return s.serial(ctx, "set media", func(opCtx context.Context) error {
		if err := s.applyMedia(opCtx, media); err != nil {
			return err
		}
		s.media = media
		return nil
	})
}

// applyMedia replaces the emulated media, keeping the outstanding-override flag Prepare reads.
func (s *Session) applyMedia(ctx context.Context, media Media) error {
	return s.override(&s.emulated.media, media != Media{}, ctx, func(c context.Context) error { return setMedia(c, media) })
}
func (s *Session) ClearMedia(ctx context.Context) error { return s.SetMedia(ctx, Media{}) }
func setMedia(ctx context.Context, media Media) error {
	features := []*emulation.MediaFeature{}
	if media.ColorScheme != "" {
		features = append(features, &emulation.MediaFeature{Name: "prefers-color-scheme", Value: media.ColorScheme})
	}
	if media.ReducedMotion != "" {
		features = append(features, &emulation.MediaFeature{Name: "prefers-reduced-motion", Value: media.ReducedMotion})
	}
	return chromedp.Run(ctx, emulation.SetEmulatedMedia().WithMedia(media.Type).WithFeatures(features))
}

func (s *Session) SetUserAgent(ctx context.Context, userAgent string) error {
	if err := s.tabOnly("set user agent"); err != nil {
		return err
	}
	return s.serial(ctx, "set user agent", func(opCtx context.Context) error {
		return s.override(&s.emulated.userAgent, userAgent != "", opCtx, func(c context.Context) error { return SetUserAgentContext(c, userAgent) })
	})
}
func (s *Session) ClearUserAgent(ctx context.Context) error { return s.SetUserAgent(ctx, "") }

// emulationOverrides records which overrides a session has outstanding, so Prepare clears only those
// and a spec that never emulated anything pays no round trips for them.  A flag goes up before the
// command that sets its override (a command that reports an error may still have landed) and comes
// down only after a clear succeeds.  The session's serial lock guards the fields.
type emulationOverrides struct {
	deviceMetrics, geolocation, userAgent, locale, timezone, media bool
}

// override runs one emulation command under its outstanding flag - see emulationOverrides.
func (s *Session) override(flag *bool, setting bool, ctx context.Context, run func(context.Context) error) error {
	if setting {
		*flag = true
		return run(ctx)
	}
	return s.clearOverride(flag, ctx, run)
}

func (s *Session) clearOverride(flag *bool, ctx context.Context, clear func(context.Context) error) error {
	if err := clear(ctx); err != nil {
		return err
	}
	*flag = false
	return nil
}

func (s *Session) resetEmulation(ctx context.Context) error {
	for _, reset := range []struct {
		flag  *bool
		clear func(context.Context) error
	}{
		{&s.emulated.deviceMetrics, clearDeviceMetrics},
		{&s.emulated.geolocation, clearGeolocation},
		{&s.emulated.userAgent, func(c context.Context) error { return SetUserAgentContext(c, "") }},
		{&s.emulated.locale, func(c context.Context) error { return setLocale(c, "") }},
		{&s.emulated.timezone, func(c context.Context) error { return setTimezone(c, "") }},
		{&s.emulated.media, func(c context.Context) error { return setMedia(c, Media{}) }},
	} {
		if !*reset.flag {
			continue
		}
		if err := s.clearOverride(reset.flag, ctx, reset.clear); err != nil {
			return err
		}
	}
	if s.permissionsSet.Load() {
		if err := s.resetPermissions(ctx); err != nil {
			return err
		}
	}
	s.media = Media{}
	return nil
}

// SetUserAgentContext overrides the user agent the page reports (navigator.userAgent and the
// User-Agent request header); the empty string removes the override.
func SetUserAgentContext(ctx context.Context, userAgent string) error {
	return chromedp.Run(ctx, emulation.SetUserAgentOverride(userAgent))
}
