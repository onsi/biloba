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
	if metrics.Width <= 0 || metrics.Height <= 0 || metrics.DeviceScaleFactor <= 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set device metrics", Message: "width, height, and device scale factor must be positive"}
	}
	return s.serial(ctx, "set device metrics", func(opCtx context.Context) error {
		return chromedp.Run(opCtx, emulation.SetDeviceMetricsOverride(int64(metrics.Width), int64(metrics.Height), metrics.DeviceScaleFactor, metrics.Mobile))
	})
}

func (s *Session) ClearDeviceMetrics(ctx context.Context) error {
	return s.serial(ctx, "clear device metrics", clearDeviceMetrics)
}
func clearDeviceMetrics(ctx context.Context) error {
	return chromedp.Run(ctx, emulation.ClearDeviceMetricsOverride())
}

func (s *Session) SetGeolocation(ctx context.Context, location Geolocation) error {
	if location.Latitude < -90 || location.Latitude > 90 || location.Longitude < -180 || location.Longitude > 180 || location.Accuracy < 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set geolocation", Message: "latitude, longitude, or accuracy is outside its valid range"}
	}
	return s.serial(ctx, "set geolocation", func(opCtx context.Context) error {
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
	return s.serial(ctx, "clear geolocation", clearGeolocation)
}
func clearGeolocation(ctx context.Context) error {
	return chromedp.Run(ctx, emulation.ClearGeolocationOverride())
}

func (s *Session) SetPermissions(ctx context.Context, origin string, permissions map[Permission]PermissionState) error {
	return s.serial(ctx, "set permissions", func(opCtx context.Context) error {
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
	return s.serial(ctx, "reset permissions", s.resetPermissions)
}
func (s *Session) resetPermissions(ctx context.Context) error {
	return s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
		return cdpbrowser.ResetPermissions().WithBrowserContextID(s.browserContextID).Do(browserCtx)
	})
}

func (s *Session) SetLocale(ctx context.Context, locale string) error {
	return s.serial(ctx, "set locale", func(opCtx context.Context) error { return setLocale(opCtx, locale) })
}
func (s *Session) ClearLocale(ctx context.Context) error { return s.SetLocale(ctx, "") }
func setLocale(ctx context.Context, locale string) error {
	return chromedp.Run(ctx, emulation.SetLocaleOverride().WithLocale(locale))
}

func (s *Session) SetTimezone(ctx context.Context, timezone string) error {
	return s.serial(ctx, "set timezone", func(opCtx context.Context) error { return setTimezone(opCtx, timezone) })
}
func (s *Session) ClearTimezone(ctx context.Context) error { return s.SetTimezone(ctx, "") }
func setTimezone(ctx context.Context, timezone string) error {
	return chromedp.Run(ctx, emulation.SetTimezoneOverride(timezone))
}

func (s *Session) SetMedia(ctx context.Context, media Media) error {
	return s.serial(ctx, "set media", func(opCtx context.Context) error { return setMedia(opCtx, media) })
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

func (s *Session) resetEmulation(ctx context.Context) error {
	for _, reset := range []func(context.Context) error{clearDeviceMetrics, clearGeolocation, func(c context.Context) error { return setLocale(c, "") }, func(c context.Context) error { return setTimezone(c, "") }, func(c context.Context) error { return setMedia(c, Media{}) }, s.resetPermissions} {
		if err := reset(ctx); err != nil {
			return err
		}
	}
	return nil
}
