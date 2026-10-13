package biloba

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/chromedp/cdproto/cdp"
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

/*
SetUserAgent() overrides the user agent this tab reports - navigator.userAgent and the User-Agent
request header - until the spec ends and a DeferCleanup restores the previous one:

	b.SetUserAgent("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) ...")
	b.Navigate(url)

Call it before Navigate: a document reads its user agent when it loads.  It applies to a tab, not a
frame handle.  Combine it with [Biloba.SetWindowSize] and [Biloba.DeviceScaleFactor], [Biloba.Mobile],
and [Biloba.TouchScreen] to emulate a device.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetUserAgent(userAgent string) {
	b.gt.Helper()
	if b.refusedOnFrame("SetUserAgent") {
		return
	}
	b.guardConfig("SetUserAgent")
	b.lock.Lock()
	previous := b.state.userAgent
	b.lock.Unlock()
	if err := b.setUserAgent(userAgent); err != nil {
		b.gt.Fatalf("failed to set the user agent: %s", err.Error())
		return
	}
	b.gt.DeferCleanup(func() {
		if err := b.setUserAgent(previous); err != nil {
			b.gt.Fatalf("failed to reset the user agent: %s", err.Error())
		}
	})
}

func (b *Biloba) setUserAgent(userAgent string) error {
	err := b.runEngine("set the user agent", func(ctx context.Context) error {
		return engine.SetUserAgentContext(ctx, userAgent)
	})
	if err == nil {
		b.lock.Lock()
		b.state.userAgent = userAgent
		b.lock.Unlock()
	}
	return err
}

/*
Geolocation is the position [Biloba.SetGeolocation] reports: Latitude in [-90, 90] and Longitude in
[-180, 180] degrees, and Accuracy in meters.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
type Geolocation struct {
	Latitude, Longitude, Accuracy float64
}

/*
SetGeolocation() makes navigator.geolocation report location on this tab, across navigations, until
the spec ends and a DeferCleanup restores the previous position.  The page also needs the geolocation
permission to read it:

	b.SetPermissions(origin, map[biloba.Permission]biloba.PermissionState{biloba.PermissionGeolocation: biloba.PermissionGranted})
	b.SetGeolocation(biloba.Geolocation{Latitude: 48.8584, Longitude: 2.2945, Accuracy: 10})

A position outside the valid ranges fails the spec.  SetGeolocation applies to a tab, not a frame
handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetGeolocation(location Geolocation) {
	b.gt.Helper()
	if b.refusedOnFrame("SetGeolocation") {
		return
	}
	b.guardConfig("SetGeolocation")
	if err := engine.ValidateGeolocation(engine.Geolocation(location)); err != nil {
		b.gt.Fatalf("SetGeolocation: %s, got %+v", err.Error(), location)
		return
	}
	emulate(b, "SetGeolocation", "geolocation", geolocationSetting, &location, sendGeolocation)
}

/*
ClearGeolocation() removes this tab's emulated position until the spec ends and a DeferCleanup
restores it.  It applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) ClearGeolocation() {
	b.gt.Helper()
	if b.refusedOnFrame("ClearGeolocation") {
		return
	}
	b.guardConfig("ClearGeolocation")
	emulate(b, "ClearGeolocation", "geolocation", geolocationSetting, nil, sendGeolocation)
}

func geolocationSetting(e *emulationState) *override[*Geolocation] { return &e.geolocation }
func sendGeolocation(ctx context.Context, location *Geolocation) error {
	if location == nil {
		return engine.ClearGeolocationContext(ctx)
	}
	return engine.SetGeolocationContext(ctx, engine.Geolocation(*location))
}

/*
SetLocale() makes this tab's pages format dates, numbers, and collation for locale ("fr-FR", "ja-JP",
...): it sets the default locale the page's Intl APIs and toLocaleString() use.  It holds across
navigations until the spec ends and a DeferCleanup restores the previous locale.  "" removes the
override, and a locale Chrome does not know fails the spec.  It applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetLocale(locale string) {
	b.gt.Helper()
	if b.refusedOnFrame("SetLocale") {
		return
	}
	b.guardConfig("SetLocale")
	emulate(b, "SetLocale", "locale", localeSetting, locale, engine.SetLocaleContext)
}

func localeSetting(e *emulationState) *override[string] { return &e.locale }

/*
SetTimezone() makes this tab's pages run in the IANA time zone timezone ("Europe/Paris",
"America/New_York", ...).  It holds across navigations until the spec ends and a DeferCleanup
restores the previous time zone.  "" removes the override, and a time zone Chrome does not know fails
the spec.  It applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetTimezone(timezone string) {
	b.gt.Helper()
	if b.refusedOnFrame("SetTimezone") {
		return
	}
	b.guardConfig("SetTimezone")
	emulate(b, "SetTimezone", "time zone", timezoneSetting, timezone, engine.SetTimezoneContext)
}

func timezoneSetting(e *emulationState) *override[string] { return &e.timezone }

/*
Permission names a permission for [Biloba.SetPermissions].  The constants cover the common ones; any
name navigator.permissions.query accepts works.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
type Permission string

const (
	PermissionGeolocation    Permission = "geolocation"
	PermissionNotifications  Permission = "notifications"
	PermissionCamera         Permission = "camera"
	PermissionMicrophone     Permission = "microphone"
	PermissionClipboardRead  Permission = "clipboard-read"
	PermissionClipboardWrite Permission = "clipboard-write"
)

// PermissionState is the state [Biloba.SetPermissions] gives a permission.
type PermissionState string

const (
	PermissionGranted PermissionState = "granted"
	PermissionDenied  PermissionState = "denied"
	PermissionPrompt  PermissionState = "prompt"
)

/*
SetPermissions() sets permissions for origin ("" for every origin) without prompting.  It adds to the
permissions set earlier in the spec, and a DeferCleanup restores the previous ones when the spec ends:

	b.SetPermissions(fixtureServer, map[biloba.Permission]biloba.PermissionState{
		biloba.PermissionGeolocation:   biloba.PermissionGranted,
		biloba.PermissionNotifications: biloba.PermissionDenied,
	})

Permissions belong to the tab's browser context, so tabs the page opens share them; a tab from
[Biloba.NewTab] has its own.  SetPermissions applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetPermissions(origin string, permissions map[Permission]PermissionState) {
	b.gt.Helper()
	if b.refusedOnFrame("SetPermissions") {
		return
	}
	b.guardConfig("SetPermissions")
	for permission, state := range permissions {
		if err := engine.ValidatePermissions(map[engine.Permission]engine.PermissionState{engine.Permission(permission): engine.PermissionState(state)}); err != nil {
			b.gt.Fatalf("SetPermissions: %s, got %q for %q", err.Error(), state, permission)
			return
		}
	}
	b.root.lock.Lock()
	record := b.permissionRecord()
	previous := clonePermissionGrants(record.grants)
	record.outstanding = true
	b.root.lock.Unlock()
	if err := b.runEngine("set permissions", func(ctx context.Context) error {
		return engine.SetPermissionsContext(ctx, b.browserContextID, origin, enginePermissions(permissions))
	}); err != nil {
		b.gt.Fatalf("failed to set permissions: %s", err.Error())
		return
	}
	b.root.lock.Lock()
	grants := clonePermissionGrants(record.grants)
	if grants == nil {
		grants = map[string]map[Permission]PermissionState{}
	}
	if grants[origin] == nil {
		grants[origin] = map[Permission]PermissionState{}
	}
	maps.Copy(grants[origin], permissions)
	record.grants = grants
	b.root.lock.Unlock()
	b.gt.DeferCleanup(func() {
		if b.Context.Err() != nil {
			return
		}
		if err := b.restorePermissions(previous); err != nil {
			b.gt.Fatalf("failed to restore permissions: %s", err.Error())
		}
	})
}

/*
ResetPermissions() removes every permission SetPermissions set in this tab's browser context, until
the spec ends and a DeferCleanup restores them.  It applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) ResetPermissions() {
	b.gt.Helper()
	if b.refusedOnFrame("ResetPermissions") {
		return
	}
	b.guardConfig("ResetPermissions")
	b.root.lock.Lock()
	previous := clonePermissionGrants(b.permissionRecord().grants)
	b.root.lock.Unlock()
	if err := b.restorePermissions(nil); err != nil {
		b.gt.Fatalf("failed to reset permissions: %s", err.Error())
		return
	}
	b.gt.DeferCleanup(func() {
		if b.Context.Err() != nil {
			return
		}
		if err := b.restorePermissions(previous); err != nil {
			b.gt.Fatalf("failed to restore permissions: %s", err.Error())
		}
	})
}

// permissionRecord is what SetPermissions has set in one browser context since its last reset.  Like
// an emulation override, outstanding goes up before a command and only comes down when a reset leaves
// nothing behind, so Prepare() resets permissions only after a spec set some.
type permissionRecord struct {
	grants      map[string]map[Permission]PermissionState // by origin
	outstanding bool
}

// permissionRecord returns the record for b's browser context.  Permissions belong to the browser
// context rather than the tab, so the records live on the root tab.  The caller holds b.root.lock.
func (b *Biloba) permissionRecord() *permissionRecord {
	state := b.root.state
	if state.permissions == nil {
		state.permissions = map[cdp.BrowserContextID]*permissionRecord{}
	}
	record := state.permissions[b.browserContextID]
	if record == nil {
		record = &permissionRecord{}
		state.permissions[b.browserContextID] = record
	}
	return record
}

// restorePermissions resets the browser context's permissions and sets grants again.
func (b *Biloba) restorePermissions(grants map[string]map[Permission]PermissionState) error {
	b.root.lock.Lock()
	record := b.permissionRecord()
	if len(grants) > 0 {
		record.outstanding = true
	}
	b.root.lock.Unlock()
	err := b.runEngine("reset permissions", func(ctx context.Context) error {
		if err := engine.ResetPermissionsContext(ctx, b.browserContextID); err != nil {
			return err
		}
		for origin, permissions := range grants {
			if err := engine.SetPermissionsContext(ctx, b.browserContextID, origin, enginePermissions(permissions)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.root.lock.Lock()
	record.grants = grants
	record.outstanding = len(grants) > 0
	b.root.lock.Unlock()
	return nil
}

func enginePermissions(permissions map[Permission]PermissionState) map[engine.Permission]engine.PermissionState {
	out := make(map[engine.Permission]engine.PermissionState, len(permissions))
	for permission, state := range permissions {
		out[engine.Permission(permission)] = engine.PermissionState(state)
	}
	return out
}

func clonePermissionGrants(grants map[string]map[Permission]PermissionState) map[string]map[Permission]PermissionState {
	if grants == nil {
		return nil
	}
	out := make(map[string]map[Permission]PermissionState, len(grants))
	for origin, permissions := range grants {
		out[origin] = maps.Clone(permissions)
	}
	return out
}

/*
NetworkState is the network condition [Biloba.SetNetworkState] emulates.  The zero value is the
browser's own network:

  - Offline disconnects the tab: navigator.onLine is false and requests fail.
  - Latency is added to every request.
  - DownloadThroughput and UploadThroughput cap throughput in bytes per second; 0 leaves it unthrottled.
  - ConnectionType is what navigator.connection.type reports.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
type NetworkState struct {
	Offline                              bool
	Latency                              time.Duration
	DownloadThroughput, UploadThroughput float64
	ConnectionType                       NetworkConnectionType
}

// NetworkConnectionType is a connection type for [NetworkState].
type NetworkConnectionType string

const (
	NetworkConnectionNone       NetworkConnectionType = "none"
	NetworkConnectionCellular2G NetworkConnectionType = "cellular2g"
	NetworkConnectionCellular3G NetworkConnectionType = "cellular3g"
	NetworkConnectionCellular4G NetworkConnectionType = "cellular4g"
	NetworkConnectionBluetooth  NetworkConnectionType = "bluetooth"
	NetworkConnectionEthernet   NetworkConnectionType = "ethernet"
	NetworkConnectionWiFi       NetworkConnectionType = "wifi"
	NetworkConnectionWiMAX      NetworkConnectionType = "wimax"
	NetworkConnectionOther      NetworkConnectionType = "other"
)

func (s NetworkState) engine() engine.NetworkState {
	return engine.NetworkState{Offline: s.Offline, Latency: s.Latency, DownloadThroughput: s.DownloadThroughput, UploadThroughput: s.UploadThroughput, ConnectionType: engine.NetworkConnectionType(s.ConnectionType)}
}

/*
SetNetworkState() emulates a network condition on this tab - offline, added latency, capped
throughput - until the spec ends and a DeferCleanup restores the previous one:

	b.SetNetworkState(biloba.NetworkState{Latency: 400 * time.Millisecond, DownloadThroughput: 50_000})

Each call replaces the previous state, and NetworkState{} restores the browser's own network.  A
negative latency or throughput, or an unknown connection type, fails the spec.  It applies to a tab,
not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetNetworkState(state NetworkState) {
	b.gt.Helper()
	if b.refusedOnFrame("SetNetworkState") {
		return
	}
	b.guardConfig("SetNetworkState")
	b.emulateNetworkState("SetNetworkState", state)
}

/*
SetOffline() takes this tab offline (true) or back online (false), keeping the rest of its
[NetworkState], until the spec ends and a DeferCleanup restores the previous state.  It applies to a
tab, not a frame handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetOffline(offline bool) {
	b.gt.Helper()
	if b.refusedOnFrame("SetOffline") {
		return
	}
	b.guardConfig("SetOffline")
	b.lock.Lock()
	state := b.state.emulation.networkState.value
	b.lock.Unlock()
	state.Offline = offline
	b.emulateNetworkState("SetOffline", state)
}

func (b *Biloba) emulateNetworkState(method string, state NetworkState) {
	b.gt.Helper()
	if err := engine.ValidateNetworkState(state.engine()); err != nil {
		b.gt.Fatalf("%s: %s, got %+v", method, err.Error(), state)
		return
	}
	emulate(b, method, "network state", networkStateSetting, state, sendNetworkState)
}

func networkStateSetting(e *emulationState) *override[NetworkState] { return &e.networkState }
func sendNetworkState(ctx context.Context, state NetworkState) error {
	return engine.SetNetworkStateContext(ctx, state.engine())
}

/*
NetworkState() returns the network condition this tab emulates - the zero NetworkState when it
emulates none.  It is a snapshot and takes no poll-config knobs.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) NetworkState() NetworkState {
	b.gt.Helper()
	b.guardConfig("NetworkState")
	tab := b.tab()
	tab.lock.Lock()
	defer tab.lock.Unlock()
	return tab.state.emulation.networkState.value
}

/*
SetCacheEnabled() turns this tab's HTTP cache off (false) or back on (true) until the spec ends and a
DeferCleanup restores the previous setting.  Network interception ([Biloba.StubRequest] and friends)
turns the cache off while it is on, whatever SetCacheEnabled says.  It applies to a tab, not a frame
handle.

Read https://onsi.github.io/biloba/#emulating-media-and-devices to learn more about emulation
*/
func (b *Biloba) SetCacheEnabled(enabled bool) {
	b.gt.Helper()
	if b.refusedOnFrame("SetCacheEnabled") {
		return
	}
	b.guardConfig("SetCacheEnabled")
	emulate(b, "SetCacheEnabled", "HTTP cache setting", cacheSetting, !enabled, b.sendCacheDisabled)
}

func cacheSetting(e *emulationState) *override[bool] { return &e.cacheDisabled }

// sendCacheDisabled keeps the cache off while interception is on: a response served from the cache
// raises no Fetch event, so a stub would silently miss it (see ensureFetchEnabled).
func (b *Biloba) sendCacheDisabled(ctx context.Context, disabled bool) error {
	b.lock.Lock()
	intercepting := b.state.fetchEnabled
	b.lock.Unlock()
	return engine.SetCacheDisabledContext(ctx, disabled || intercepting)
}

// emulationState is the tab's environment emulation: what SetGeolocation, SetLocale, SetTimezone,
// SetNetworkState/SetOffline, and SetCacheEnabled last applied.
type emulationState struct {
	geolocation   override[*Geolocation]
	locale        override[string]
	timezone      override[string]
	networkState  override[NetworkState]
	cacheDisabled override[bool]
}

// override is one emulated setting.  value is what the spec last applied (the zero value is the
// browser's own behaviour).  outstanding records that the target may carry an override: it goes up
// BEFORE a command that sets one (a command that reports an error may still have landed) and only
// comes down when a clear succeeds.  That is what lets Prepare() skip the round trip for a setting no
// spec touched.
type override[T comparable] struct {
	value       T
	outstanding bool
}

// emulate applies value to one of the tab's settings, failing the spec if Chrome refuses it, and
// registers a DeferCleanup that restores the value it replaced.
func emulate[T comparable](b *Biloba, method, noun string, setting func(*emulationState) *override[T], value T, send func(context.Context, T) error) {
	b.gt.Helper()
	b.lock.Lock()
	previous := setting(&b.state.emulation).value
	b.lock.Unlock()
	if err := applyEmulation(b, noun, setting, value, send); err != nil {
		b.gt.Fatalf("%s failed: %s", method, err.Error())
		return
	}
	b.gt.DeferCleanup(func() {
		if b.Context.Err() != nil {
			return // the tab is closed, and its emulation with it
		}
		if err := applyEmulation(b, noun, setting, previous, send); err != nil {
			b.gt.Fatalf("failed to restore the %s %s changed: %s", noun, method, err.Error())
		}
	})
}

func applyEmulation[T comparable](b *Biloba, noun string, setting func(*emulationState) *override[T], value T, send func(context.Context, T) error) error {
	var zero T
	b.lock.Lock()
	if value != zero {
		setting(&b.state.emulation).outstanding = true
	}
	b.lock.Unlock()
	if err := b.runEngine("emulate the "+noun, func(ctx context.Context) error { return send(ctx, value) }); err != nil {
		return err
	}
	b.lock.Lock()
	s := setting(&b.state.emulation)
	s.value = value
	if value == zero {
		s.outstanding = false
	}
	b.lock.Unlock()
	return nil
}

// clearLeakedSetting clears a setting whose DeferCleanup did not land (an expired deadline, a
// cancelled context), which would otherwise leave every later spec in this process running with it.
// It costs nothing for a setting no spec touched.
func clearLeakedSetting[T comparable](b *Biloba, noun string, setting func(*emulationState) *override[T], send func(context.Context, T) error) {
	var zero T
	b.lock.Lock()
	s := setting(&b.state.emulation)
	s.value = zero
	outstanding := s.outstanding
	b.lock.Unlock()
	if !outstanding {
		return
	}
	if err := applyEmulation(b, noun, setting, zero, send); err != nil {
		b.gt.Printf("Failed to clear leaked %s emulation during Prepare():\n  %s\n", noun, err.Error())
	}
}

// clearLeakedEmulation is Prepare()'s half of the emulation teardown, called from resetBrowsingState
// after the tab state reset (which carries the emulation record over).
func (b *Biloba) clearLeakedEmulation() {
	clearLeakedSetting(b, "geolocation", geolocationSetting, sendGeolocation)
	clearLeakedSetting(b, "locale", localeSetting, engine.SetLocaleContext)
	clearLeakedSetting(b, "time zone", timezoneSetting, engine.SetTimezoneContext)
	clearLeakedSetting(b, "network state", networkStateSetting, sendNetworkState)
	clearLeakedSetting(b, "HTTP cache setting", cacheSetting, b.sendCacheDisabled)

	b.lock.Lock()
	record := b.state.permissions[b.browserContextID]
	b.state.permissions = nil
	b.lock.Unlock()
	if record == nil || !record.outstanding {
		return
	}
	b.lock.Lock()
	b.state.permissions = map[cdp.BrowserContextID]*permissionRecord{b.browserContextID: {outstanding: true}}
	b.lock.Unlock()
	if err := b.restorePermissions(nil); err != nil {
		b.gt.Printf("Failed to clear leaked permissions during Prepare():\n  %s\n", err.Error())
	}
}
