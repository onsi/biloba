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
	// Chrome's Browser.setPermission takes the names navigator.permissions.query uses, and rejects
	// its internal PermissionType names ("videoCapture", "audioCapture").
	PermissionCamera     Permission = "camera"
	PermissionMicrophone Permission = "microphone"
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
		override := emulation.SetDeviceMetricsOverrideParams{Width: int64(metrics.Width), Height: int64(metrics.Height), DeviceScaleFactor: metrics.DeviceScaleFactor, Mobile: metrics.Mobile}
		if metrics.Landscape {
			override.ScreenOrientation = &emulation.ScreenOrientation{Type: emulation.ScreenOrientationTypeLandscapePrimary, Angle: 90}
		}
		s.emulated.deviceMetrics = true
		return chromedp.Do(opCtx,
			Step(emulation.SetDeviceMetricsOverride, override),
			Step(emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: metrics.Touch}),
		)
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
	return chromedp.Do(ctx,
		Step(emulation.ClearDeviceMetricsOverride, cdp.Empty{}),
		Step(emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: false}),
	)
}

func (s *Session) SetGeolocation(ctx context.Context, location Geolocation) error {
	if err := s.tabOnly("set geolocation"); err != nil {
		return err
	}
	if err := ValidateGeolocation(location); err != nil {
		return withOperation("set geolocation", err)
	}
	return s.serial(ctx, "set geolocation", func(opCtx context.Context) error {
		s.emulated.geolocation = true
		return SetGeolocationContext(opCtx, location)
	})
}

// ValidateGeolocation rejects a location no device could report.  Chrome would accept it.
func ValidateGeolocation(location Geolocation) error {
	if location.Latitude < -90 || location.Latitude > 90 || location.Longitude < -180 || location.Longitude > 180 || location.Accuracy < 0 {
		return &Error{Code: CodeInvalidArgument, Message: "latitude, longitude, or accuracy is outside its valid range", Observed: location}
	}
	return nil
}

// SetGeolocationContext overrides the position navigator.geolocation reports.
func SetGeolocationContext(ctx context.Context, location Geolocation) error {
	// Pointers send valid zero coordinates; a nil field would turn latitude/longitude 0 into
	// "position unavailable".
	_, err := chromedp.Call(ctx, emulation.SetGeolocationOverride, emulation.SetGeolocationOverrideParams{
		Latitude: ptr(location.Latitude), Longitude: ptr(location.Longitude), Accuracy: ptr(location.Accuracy),
	})
	return err
}

func (s *Session) ClearGeolocation(ctx context.Context) error {
	if err := s.tabOnly("clear geolocation"); err != nil {
		return err
	}
	return s.serial(ctx, "clear geolocation", func(opCtx context.Context) error {
		return s.clearOverride(&s.emulated.geolocation, opCtx, ClearGeolocationContext)
	})
}
func ClearGeolocationContext(ctx context.Context) error {
	_, err := chromedp.Call(ctx, emulation.ClearGeolocationOverride, cdp.Empty{})
	return err
}

func (s *Session) SetPermissions(ctx context.Context, origin string, permissions map[Permission]PermissionState) error {
	if err := s.tabOnly("set permissions"); err != nil {
		return err
	}
	if err := ValidatePermissions(permissions); err != nil {
		return withOperation("set permissions", err)
	}
	return s.serial(ctx, "set permissions", func(opCtx context.Context) error {
		// Permissions belong to the browser context, which outlives a spawned tab: the session that
		// owns the context resets them.
		s.contextOwner().permissionsSet.Store(true)
		return SetPermissionsContext(opCtx, s.browserContextID, origin, permissions)
	})
}

// ValidatePermissions rejects a permission state other than granted, denied, or prompt.
func ValidatePermissions(permissions map[Permission]PermissionState) error {
	for _, state := range permissions {
		switch cdpbrowser.PermissionSetting(state) {
		case cdpbrowser.PermissionSettingGranted, cdpbrowser.PermissionSettingDenied, cdpbrowser.PermissionSettingPrompt:
		default:
			return &Error{Code: CodeInvalidArgument, Message: "permission state must be granted, denied, or prompt", Observed: state}
		}
	}
	return nil
}

// SetPermissionsContext sets permissions for origin ("" for every origin).  Permissions belong to a
// browser context, so every tab in browserContextID sees them.
func SetPermissionsContext(ctx context.Context, browserContextID cdp.BrowserContextID, origin string, permissions map[Permission]PermissionState) error {
	return withBrowserExecutor(ctx, func(browserCtx context.Context, browser cdp.Session) error {
		for permission, state := range permissions {
			descriptor := &cdpbrowser.PermissionDescriptor{Name: string(permission)}
			if _, err := cdp.Call(browserCtx, browser, cdpbrowser.SetPermission, cdpbrowser.SetPermissionParams{
				Permission: descriptor, Setting: cdpbrowser.PermissionSetting(state), Origin: origin, BrowserContextID: browserContextID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetPermissionsContext removes every permission override in a browser context.
func ResetPermissionsContext(ctx context.Context, browserContextID cdp.BrowserContextID) error {
	return withBrowserExecutor(ctx, func(browserCtx context.Context, browser cdp.Session) error {
		_, err := cdp.Call(browserCtx, browser, cdpbrowser.ResetPermissions, cdpbrowser.ResetPermissionsParams{BrowserContextID: browserContextID})
		return err
	})
}

// withOperation names the operation on a validation error built without one.
func withOperation(operation string, err error) error {
	if engineErr, ok := err.(*Error); ok && engineErr.Operation == "" {
		engineErr.Operation = operation
	}
	return err
}

func (s *Session) ResetPermissions(ctx context.Context) error {
	if err := s.tabOnly("reset permissions"); err != nil {
		return err
	}
	return s.serial(ctx, "reset permissions", s.resetPermissions)
}
func (s *Session) resetPermissions(ctx context.Context) error {
	err := ResetPermissionsContext(ctx, s.browserContextID)
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
		return s.override(&s.emulated.locale, locale != "", opCtx, func(c context.Context) error { return SetLocaleContext(c, locale) })
	})
}
func (s *Session) ClearLocale(ctx context.Context) error { return s.SetLocale(ctx, "") }
func SetLocaleContext(ctx context.Context, locale string) error {
	_, err := chromedp.Call(ctx, emulation.SetLocaleOverride, emulation.SetLocaleOverrideParams{Locale: locale})
	return err
}

func (s *Session) SetTimezone(ctx context.Context, timezone string) error {
	if err := s.tabOnly("set timezone"); err != nil {
		return err
	}
	return s.serial(ctx, "set timezone", func(opCtx context.Context) error {
		return s.override(&s.emulated.timezone, timezone != "", opCtx, func(c context.Context) error { return SetTimezoneContext(c, timezone) })
	})
}
func (s *Session) ClearTimezone(ctx context.Context) error { return s.SetTimezone(ctx, "") }
func SetTimezoneContext(ctx context.Context, timezone string) error {
	_, err := chromedp.Call(ctx, emulation.SetTimezoneOverride, emulation.SetTimezoneOverrideParams{TimezoneID: timezone})
	return err
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
	_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: media.Type, Features: features})
	return err
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
		{&s.emulated.geolocation, ClearGeolocationContext},
		{&s.emulated.userAgent, func(c context.Context) error { return SetUserAgentContext(c, "") }},
		{&s.emulated.locale, func(c context.Context) error { return SetLocaleContext(c, "") }},
		{&s.emulated.timezone, func(c context.Context) error { return SetTimezoneContext(c, "") }},
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
	_, err := chromedp.Call(ctx, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{UserAgent: userAgent})
	return err
}
