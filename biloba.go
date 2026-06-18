/*
Biloba builds on top of [chromedp] to bring stable, performant, automated browser testing to Ginkgo.  It embraces three principles:
  - Performance via parallelization
  - Stability via pragmatism
  - Conciseness via Ginkgo and Gomega

The godoc documentation you are reading now is meant to be a sparse reference.  To build a mental model for how to use Biloba please peruse the [documentation].

[chromedp]: https://github.com/chromedp/chromedp/
[documentation]: https://onsi.github.io/biloba
*/
package biloba

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/jehiah/agentdetection"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const BILOBA_VERSION = "0.5.0"

/*
GinkgoTInterface is the interface by which Biloba receives GinkgoT()
*/
type GinkgoTInterface interface {
	Name() string
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	TempDir() string
	Logf(format string, args ...any)
	Failed() bool

	GinkgoRecover()
	DeferCleanup(args ...any)
	Print(args ...any)
	Printf(format string, args ...any)
	Println(a ...any)
	F(format string, args ...any) string
	Fi(indentation uint, format string, args ...any) string
	Fiw(indentation uint, maxWidth uint, format string, args ...any) string
	AddReportEntryVisibilityFailureOrVerbose(name string, args ...any)
	ParallelProcess() int
	ParallelTotal() int
	AttachProgressReporter(func() string) func()
	RenderTimeline() string
}

/*
ChromeConnection captures the details necessary for [ConnectToChrome] to connect to Chrome
*/
type ChromeConnection struct {
	WebSocketURL string
	WindowWidth  int
	WindowHeight int
	// HighFidelity is true when Chrome was spun up in full ("new") headless mode.  SpinUpChrome
	// sets it and ConnectToChrome reads it to decide whether the new-headless viewport workaround
	// is needed.
	HighFidelity bool
}

func (gc ChromeConnection) encode() []byte {
	data, _ := json.Marshal(gc)
	return data
}

/*
SpinUpOption configures how [SpinUpChrome] launches Chrome.  See [HighFidelityHeadless], [AutoInstallHeadlessShell], [HeadlessShellPath], [StartingWindowSize], and [ChromeFlags].
*/
type SpinUpOption func(*spinUpConfig)

type spinUpConfig struct {
	execAllocatorOptions []chromedp.ExecAllocatorOption
	highFidelity         bool
	autoInstall          bool
	headlessShellPath    string
}

/*
HighFidelityHeadless opts out of Biloba's default lightweight chrome-headless-shell and runs the full ("new") headless Chrome - the real browser - instead.

By default Biloba favors pragmatism over realism: it drives chrome-headless-shell, the lightweight //content-based headless build, which is dramatically faster and parallelizes across processes.  Pass HighFidelityHeadless to [SpinUpChrome] when you need the realism of the full browser (precise compositing/rendering, extensions, etc.) and are willing to pay for it in speed.

Read https://onsi.github.io/biloba/#headless-fidelity to learn more
*/
func HighFidelityHeadless() SpinUpOption {
	return func(c *spinUpConfig) { c.highFidelity = true }
}

/*
AutoInstallHeadlessShell tells [SpinUpChrome] to download chrome-headless-shell (via Chrome for Testing) into Biloba's cache if it cannot be found locally, instead of failing with installation instructions.  Biloba never downloads anything by default; opt in to auto-install for zero-config setups such as ephemeral CI.  Has no effect under [HighFidelityHeadless].

Read https://onsi.github.io/biloba/#headless-fidelity to learn more
*/
func AutoInstallHeadlessShell() SpinUpOption {
	return func(c *spinUpConfig) { c.autoInstall = true }
}

/*
HeadlessShellPath explicitly points Biloba at a chrome-headless-shell binary, bypassing the search.  You can also set the BILOBA_CHROME_HEADLESS_SHELL environment variable.

Read https://onsi.github.io/biloba/#headless-fidelity to learn more
*/
func HeadlessShellPath(path string) SpinUpOption {
	return func(c *spinUpConfig) { c.headlessShellPath = path }
}

/*
StartingWindowSize sets the default window size for all tabs.  Pass it to [SpinUpChrome].
*/
func StartingWindowSize(width int, height int) SpinUpOption {
	return func(c *spinUpConfig) {
		c.execAllocatorOptions = append(c.execAllocatorOptions, chromedp.WindowSize(width, height))
	}
}

/*
ChromeFlags passes raw [chromedp.ExecAllocatorOption] flags through to the Chrome process launched by [SpinUpChrome]:

	biloba.SpinUpChrome(GinkgoT(), biloba.ChromeFlags(chromedp.Flag("lang", "es")))

Read https://onsi.github.io/biloba/#configuration to learn more
*/
func ChromeFlags(options ...chromedp.ExecAllocatorOption) SpinUpOption {
	return func(c *spinUpConfig) {
		c.execAllocatorOptions = append(c.execAllocatorOptions, options...)
	}
}

// emulateViewportMatchingScreen is a chromedp.EmulateViewportOption that, in addition to the layout
// viewport EmulateViewport already sets, overrides the emulated *screen* dimensions to match.  Full
// ("new") headless Chrome composites into a small virtual screen (default 800x600) regardless of the
// requested window size, and the compositor's trusted-input surface is clamped to that screen - so a
// plain SetDeviceMetricsOverride grows the layout viewport while leaving real wheel/scroll input to
// be silently dropped below the screen's bottom edge.  Growing the emulated screen to the viewport
// size lifts that clamp, keeping the layout viewport and the real input surface in agreement.
func emulateViewportMatchingScreen(p1 *emulation.SetDeviceMetricsOverrideParams, _ *emulation.SetTouchEmulationEnabledParams) {
	p1.ScreenWidth = p1.Width
	p1.ScreenHeight = p1.Height
}

// applyHighFidelityViewport (re)asserts the high-fidelity viewport emulation for this tab.  Full
// ("new") headless renders into a small virtual screen (default 800x600) regardless of the requested
// --window-size, so an un-emulated tab reports window.innerHeight well below the window height.  We
// EmulateViewport to the requested window dimensions (captured by SpinUpChrome) to give the tab the
// correct layout viewport, growing the emulated *screen* to match (emulateViewportMatchingScreen) so
// the compositor's real trusted-input surface extends to the full viewport - otherwise CDP
// wheel/scroll input is silently dropped below the small screen's bottom edge, making measurePoint's
// inViewport check lie to realistic-mode interactions.  The compositor surface is (re)sized from the
// device metrics in effect at page commit, so this must be re-applied after each navigation, not just
// once at connect time.  It is a no-op in the default chrome-headless-shell lane, which has no such
// clamp (and leaves WindowWidth/Height at 0).
func (b *Biloba) applyHighFidelityViewport() error {
	if !b.ChromeConnection.HighFidelity || b.ChromeConnection.WindowWidth <= 0 || b.ChromeConnection.WindowHeight <= 0 {
		return nil
	}
	return chromedp.Run(b.Context, chromedp.EmulateViewport(
		int64(b.ChromeConnection.WindowWidth),
		int64(b.ChromeConnection.WindowHeight),
		emulateViewportMatchingScreen,
	))
}

// reassertViewportForCompositor re-applies the viewport emulation at this tab's *current* size after a
// navigation.  In high-fidelity mode the override itself survives a navigation (window.innerHeight
// stays put), but the compositor's trusted-input surface is only (re)sized from the device metrics in
// effect at page commit - so without re-asserting, real wheel/scroll input is silently dropped below
// the small virtual screen even though the layout viewport says the point is in view.  We re-apply at
// the current inner size (not the connect-time default) so a SetWindowSize done earlier in the spec is
// preserved.  A no-op in the default chrome-headless-shell lane.
func (b *Biloba) reassertViewportForCompositor() {
	if !b.ChromeConnection.HighFidelity || b.ChromeConnection.WindowWidth <= 0 || b.ChromeConnection.WindowHeight <= 0 {
		return
	}
	var dims []int64
	if err := chromedp.Run(b.Context, chromedp.Evaluate("[window.innerWidth, window.innerHeight]", &dims)); err != nil || len(dims) != 2 || dims[0] <= 0 || dims[1] <= 0 {
		return
	}
	_ = chromedp.Run(b.Context, chromedp.EmulateViewport(dims[0], dims[1], emulateViewportMatchingScreen))
}

func gooseConfigPath(process int) string {
	return fmt.Sprintf("./.biloba-config-%d", process)
}

/*
Call SpinUpChrome(GinkgoT()) to spin up a Chrome browser

Read https://onsi.github.io/biloba/#bootstrapping-biloba for details on how to set up your Ginkgo suite and use SpinUpChrome correctly
*/
func SpinUpChrome(ginkgoT GinkgoTInterface, options ...SpinUpOption) ChromeConnection {
	ginkgoT.Helper()
	cfg := &spinUpConfig{}
	for _, option := range options {
		option(cfg)
	}

	// BILOBA_INTERACTIVE runs a real, visible browser, which is inherently high fidelity
	// (chrome-headless-shell cannot run headful).
	interactive := os.Getenv("BILOBA_INTERACTIVE") != ""
	if interactive {
		cfg.highFidelity = true
	}

	tmp := ginkgoT.TempDir()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(1024, 768),
		chromedp.UserDataDir(tmp),
		// chromedp gives Chrome 20s to print its DevTools websocket URL before giving up with
		// "websocket url timeout reached". A cold full ("new") headless google-chrome on a loaded
		// CI runner intermittently needs longer than that to come up, which flaked the high-fidelity
		// lane at suite bring-up. The lightweight chrome-headless-shell starts well within 20s, so a
		// roomier ceiling only ever buys slow launches headroom - it never slows a fast one.
		chromedp.WSURLReadTimeout(60*time.Second),
	)
	opts = append(opts, cfg.execAllocatorOptions...)
	if interactive {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	if !cfg.highFidelity {
		// Default (pragmatic) mode: drive the lightweight chrome-headless-shell.
		shellPath, err := resolveHeadlessShellPath(ginkgoT, cfg)
		if err != nil {
			ginkgoT.Fatalf("%s", err.Error())
			return ChromeConnection{}
		}
		opts = append(opts, chromedp.ExecPath(shellPath))
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ginkgoT.DeferCleanup(cancel)

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	ginkgoT.DeferCleanup(cancel)

	cc := ChromeConnection{HighFidelity: cfg.highFidelity}

	if cfg.highFidelity {
		// Full ("new") headless renders into a small virtual screen (default 800x600) regardless of
		// --window-size, so an un-emulated tab reports window.innerHeight well below the requested
		// height.  We capture the outer (requested) window dimensions here and have ConnectToChrome
		// EmulateViewport each tab back up to that size (see applyHighFidelityViewport).
		// chrome-headless-shell has no such virtual-screen clamp, so this probe and the EmulateViewport
		// workaround are skipped in the default mode.
		var outerDims []int
		if err := chromedp.Run(browserCtx, chromedp.Evaluate("[window.outerWidth, window.outerHeight]", &outerDims)); err != nil {
			ginkgoT.Fatalf("failed to spin up chrome: %w", err)
			return ChromeConnection{}
		}
		if len(outerDims) == 2 {
			cc.WindowWidth = outerDims[0]
			cc.WindowHeight = outerDims[1]
		}
	} else if err := chromedp.Run(browserCtx, chromedp.Evaluate("1", nil)); err != nil {
		ginkgoT.Fatalf("failed to spin up chrome: %w", err)
		return ChromeConnection{}
	}

	bs, err := os.ReadFile(filepath.Join(tmp, "DevToolsActivePort"))
	if err != nil {
		ginkgoT.Fatalf("failed to spin up chrome: %w", err)
		return ChromeConnection{}
	}
	components := strings.Split(string(bs), "\n")
	cc.WebSocketURL = fmt.Sprintf("ws://127.0.0.1:%s%s", components[0], components[1])

	os.WriteFile(gooseConfigPath(ginkgoT.ParallelProcess()), cc.encode(), 0744)
	ginkgoT.DeferCleanup(func() error {
		chromedp.Cancel(browserCtx)
		// The config file is a throwaway; if something already removed it, that's success, not a
		// teardown failure (a missing file here has shown up intermittently under `ginkgo --repeat`).
		if err := os.Remove(gooseConfigPath(ginkgoT.ParallelProcess())); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})

	return cc
}

/*
BilobaConfigOptions are passed in to [ConnectToChrome] to configure a given connection to Chrome
*/
type BilobaConfigOption func(*Biloba)

// defaultAutomationScreenshotsDir is where failure screenshots are written, by default, when
// Biloba detects it's running under automation (CI or an AI agent) and neither the suite nor
// BILOBA_SCREENSHOTS_DIR specified a directory.  It is workspace-relative so CI can collect it
// (e.g. via actions/upload-artifact).
const defaultAutomationScreenshotsDir = "./biloba-screenshots"

// automationDetected reports whether Biloba is running in a non-interactive context - CI or under
// an AI coding agent - in which case the failure-artifact defaults flip to text-friendly output.
// It is a package var so the suite can pin it for deterministic tests (see export_test.go).
var automationDetected = func() bool {
	return os.Getenv("CI") != "" || agentdetection.IsAgent()
}

// boolArg resolves the variadic argument the boolean BilobaConfig options take: passing no
// argument means true (BilobaConfigFailureOutlines() enables outlines), while an explicit value
// is honored as-is (BilobaConfigFailureOutlines(false) disables them).
func boolArg(args []bool) bool {
	if len(args) > 0 {
		return args[0]
	}
	return true
}

/*
Pass BilobaConfigDebugLogging to [ConnectToChrome] to send all Chrome debug logging to the GinkgoWriter.

Like all the boolean BilobaConfig options it takes an optional bool: BilobaConfigDebugLogging() turns it on, BilobaConfigDebugLogging(false) turns it off.
*/
func BilobaConfigDebugLogging(enabled ...bool) func(*Biloba) {
	return func(b *Biloba) {
		b.debugLogging = boolArg(enabled)
	}
}

/*
Pass BilobaConfigWithChromeConnection to [ConnectToChrome] to provide your own [ChromeConnection] details
*/
func BilobaConfigWithChromeConnection(cc ChromeConnection) func(*Biloba) {
	return func(b *Biloba) {
		b.ChromeConnection = cc
	}
}

/*
Pass BilobaConfigFailureScreenshots to [ConnectToChrome] to control whether Biloba captures a screenshot of every tab on failure.

It is on by default; BilobaConfigFailureScreenshots(false) turns it off.
*/
func BilobaConfigFailureScreenshots(enabled ...bool) func(*Biloba) {
	return func(b *Biloba) {
		b.failureScreenshots = boolArg(enabled)
	}
}

/*
Pass BilobaConfigFailureOutlines to [ConnectToChrome] to control whether Biloba attaches a DOM outline of every tab on failure.

When a human is driving, outlines are off by default (the screenshot is the more useful artifact); under automation (CI or an AI agent) Biloba turns them on automatically.  Set this explicitly to override that default in either direction: BilobaConfigFailureOutlines() forces them on for an interactive run, BilobaConfigFailureOutlines(false) forces them off under automation.

See https://onsi.github.io/biloba/#failure-artifacts for how the human/automation defaults are resolved.
*/
func BilobaConfigFailureOutlines(enabled ...bool) func(*Biloba) {
	return func(b *Biloba) {
		b.failureOutlines = boolArg(enabled)
		b.failureOutlinesSet = true
	}
}

/*
Pass BilobaConfigFailureScreenshotsSize to [ConnectToChrome] to set the size for the screenshots generated on failure
*/
func BilobaConfigFailureScreenshotsSize(width, height int) func(*Biloba) {
	return func(b *Biloba) {
		b.failureScreenshotWidth = width
		b.failureScreenshotHeight = height
	}
}

/*
Pass BilobaConfigProgressReportScreenshots to [ConnectToChrome] to control whether Biloba emits screenshots when Progress Reports are requested.

It is on by default; BilobaConfigProgressReportScreenshots(false) turns it off.
*/
func BilobaConfigProgressReportScreenshots(enabled ...bool) func(*Biloba) {
	return func(b *Biloba) {
		b.progressReportScreenshots = boolArg(enabled)
	}
}

/*
Pass BilobaConfigProgressReportScreenshotSize to [ConnectToChrome] to set the size for the screenshots generated when a progress report is requested
*/
func BilobaConfigProgressReportScreenshotSize(width, height int) func(*Biloba) {
	return func(b *Biloba) {
		b.progressReportScreenshotWidth = width
		b.progressReportScreenshotHeight = height
	}
}

/*
Pass BilobaConfigInlineScreenshots to [ConnectToChrome] to control whether Biloba emits inline-image escape sequences in failure and progress-report output.

It is on by default (subject to terminal support) when a human is driving, and off under automation (CI or an AI agent).  BilobaConfigInlineScreenshots(false) suppresses the inline blob explicitly; BilobaConfigInlineScreenshots() forces it on even under automation.  When inline images are off, Biloba still captures screenshots and writes them to the configured directory (if any); the file path is printed to test output.

The BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel|none environment variable selects (or disables, with "none") the inline-image protocol at runtime.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func BilobaConfigInlineScreenshots(enabled ...bool) func(*Biloba) {
	return func(b *Biloba) {
		b.inlineScreenshots = boolArg(enabled)
		b.inlineScreenshotsSet = true
	}
}

/*
Pass BilobaConfigScreenshotsToDir to [ConnectToChrome] to write failure screenshots to PNG files in the specified directory.
When set, each tab's screenshot is written to <dir>/screenshot-<spec>-<tab>.png on failure, and the absolute path is printed to the test output.
This is complementary to the inline imgcat path: both run when a dir is configured.
The directory is created if it does not already exist.

The BILOBA_SCREENSHOTS_DIR environment variable does the same thing at runtime (this option wins if both are set), and is also the way to point automation's default screenshots directory somewhere specific.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func BilobaConfigScreenshotsToDir(dir string) func(*Biloba) {
	return func(b *Biloba) {
		b.screenshotsDir = dir
	}
}

/*
Call ConnectToChrome(GinkgoT()) to connect to a Chrome browser

Returns a *Biloba struct that you use to interact with the browser

Read https://onsi.github.io/biloba/#bootstrapping-biloba for details on how to set up your Ginkgo suite and use ConnectToChrome correctly
*/
func ConnectToChrome(ginkgoT GinkgoTInterface, options ...BilobaConfigOption) *Biloba {
	ginkgoT.Helper()
	b := newBiloba(ginkgoT)
	b.root = b

	for _, option := range options {
		option(b)
	}

	// Resolve the on-failure artifact policy from the environment, filling in only what the suite
	// left unconfigured (explicit options always win - each artifact knob is one-directional, so a
	// non-zero value means the user set it).  Interactive humans keep the defaults: inline
	// screenshots, no DOM outline.  Under automation (CI or an AI agent) the artifacts flip to
	// text-friendly output: outlines on, inline image blobs off (they're noise in a log), and
	// screenshots written to disk so they can be inspected/uploaded after the run.
	if automationDetected() {
		if !b.failureOutlinesSet {
			b.failureOutlines = true
		}
		if !b.inlineScreenshotsSet {
			b.inlineScreenshots = false
		}
	}
	if b.screenshotsDir == "" {
		if dir := os.Getenv("BILOBA_SCREENSHOTS_DIR"); dir != "" {
			b.screenshotsDir = dir
		} else if automationDetected() {
			b.screenshotsDir = defaultAutomationScreenshotsDir
		}
	}

	if b.ChromeConnection.WebSocketURL == "" {
		var cc ChromeConnection
		configFilePath := gooseConfigPath(ginkgoT.ParallelProcess())
		if _, err := os.Stat(configFilePath); err != nil {
			configFilePath = gooseConfigPath(1)
		}
		data, err := os.ReadFile(configFilePath)
		if err != nil {
			ginkgoT.Fatalf("failed to load ChromeConnection: %w", err)
			return nil
		}
		err = json.Unmarshal(data, &cc)
		if err != nil {
			ginkgoT.Fatalf("failed to decode ChromeConnection: %w", err)
			return nil
		}
		b.ChromeConnection = cc
	}
	allocatorContext, cancel := chromedp.NewRemoteAllocator(context.Background(), b.ChromeConnection.WebSocketURL)
	b.gt.DeferCleanup(cancel)

	// Chrome 149+ rejects Target.createTarget with a browserContextId unless newWindow:true is used,
	// so we can't use chromedp.WithNewBrowserContext() directly. Instead we bootstrap a throwaway
	// default-context tab to initialize the Browser connection, then manually create the isolated
	// browser context and target, and attach via WithTargetID.
	var bootstrapOpts []chromedp.ContextOption
	if b.debugLogging {
		bootstrapOpts = append(bootstrapOpts,
			chromedp.WithDebugf(b.gt.Logf),
			chromedp.WithLogf(b.gt.Logf),
			chromedp.WithErrorf(b.gt.Logf),
		)
	}
	if err := b.bootstrapIsolatedTab(allocatorContext, bootstrapOpts); err != nil {
		ginkgoT.Fatalf("failed to connect to chrome: %w", err)
		return nil
	}

	// Give this root tab the high-fidelity viewport emulation (see applyHighFidelityViewport); a no-op
	// in the default chrome-headless-shell lane.
	if err := b.applyHighFidelityViewport(); err != nil {
		ginkgoT.Fatalf("failed to set initial window size: %w", err)
		return nil
	}

	b.downloadDir = b.gt.TempDir()
	b.setUpListeners()

	b.lock.Lock()
	b.tabs[chromedp.FromContext(b.Context).Target.TargetID] = b
	b.lock.Unlock()

	return b
}

// connectAttempts is the total number of times bootstrapIsolatedTab tries to bring up the root tab
// before giving up (an initial attempt plus retries).  connectBackoffBase is the first retry's
// backoff ceiling; it doubles on each subsequent retry.
const (
	connectAttempts    = 4
	connectBackoffBase = 50 * time.Millisecond
)

// bootstrapIsolatedTab connects to Chrome and brings up this root tab's isolated browser context +
// target.  Each step is a CDP round-trip against the single shared Chrome and can fail transiently
// when many parallel processes connect at once, so we retry with exponential backoff + full jitter -
// the jitter de-correlates the retries of processes that collided together, so they stop colliding
// instead of retrying in lockstep.  On a failed attempt we cancel whatever we created (cancelling the
// bootstrap connection disposes the isolated browser context via WithDisposeOnDetach) so retries
// don't leak contexts or targets.  On success b is wired up and the surviving contexts are kept alive
// for the life of the spec; on exhaustion the last error is returned.
func (b *Biloba) bootstrapIsolatedTab(allocatorContext context.Context, bootstrapOpts []chromedp.ContextOption) error {
	var lastErr error
	for attempt := range connectAttempts {
		if attempt > 0 {
			// full jitter: a random wait in [0, base*2^(attempt-1))
			ceiling := connectBackoffBase << (attempt - 1)
			time.Sleep(time.Duration(rand.Int64N(int64(ceiling))))
		}

		bootstrapCtx, cancelBootstrap := chromedp.NewContext(allocatorContext, bootstrapOpts...)
		if err := chromedp.Run(bootstrapCtx, chromedp.Evaluate("1", nil)); err != nil {
			cancelBootstrap()
			lastErr = err
			continue
		}

		browserContextID, isolatedTargetID, err := newIsolatedBrowserContextAndTarget(bootstrapCtx)
		if err != nil {
			cancelBootstrap()
			lastErr = err
			continue
		}

		tabCtx, cancelTab := chromedp.NewContext(bootstrapCtx, chromedp.WithTargetID(isolatedTargetID))
		b.Context = tabCtx
		if _, err := b.RunErr("1"); err != nil {
			cancelTab()
			cancelBootstrap()
			lastErr = err
			continue
		}

		// success - keep the bootstrap connection and isolated tab alive for the life of the spec
		// (LIFO cleanup: tab detaches first, then the bootstrap connection tears down)
		b.gt.DeferCleanup(cancelBootstrap)
		b.gt.DeferCleanup(cancelTab)
		b.targetID = chromedp.FromContext(b.Context).Target.TargetID
		b.browserContextID = browserContextID
		return nil
	}
	return lastErr
}

/*
Biloba is the main object provided by Biloba for interacting with Chrome.  You get an instance of Biloba when you [ConnectToChrome].  This instance is the reusable root tab and cannot be closed.

Any new tabs created or spawned while your tests run will be represented as different instances of Biloba.

To send commands to a particular tab you use the Biloba instance associated with that tab.

Read https://onsi.github.io/biloba/#parallelization-how-biloba-manages-browsers-and-tabs to build a mental model of how Biloba manages tabs
*/
type Biloba struct {
	//Context is the underlying chromedp context.  Pass this in to chromedp to be take actions on this tab
	Context          context.Context
	gt               GinkgoTInterface
	ChromeConnection ChromeConnection

	targetID         target.ID
	browserContextID cdp.BrowserContextID

	lock  *sync.Mutex
	root  *Biloba
	tabs  map[target.ID]*Biloba
	close context.CancelFunc

	bilobaIsInstalled bool

	// realistic routes DOM interactions (Click/Hover) through real CDP input instead of the
	// fast atomic JS simulations.  Set on the lightweight view returned by Realistic().
	realistic bool

	downloadDir     string
	downloads       map[string]*Download
	downloadHistory map[string]time.Time

	dialogHandlers []*DialogHandler
	dialogs        []*Dialog

	// consoleErrors accumulates rendered console.error / console.assert messages seen on this tab so
	// attachFailureArtifactsIfFailed can replay them at the top of the failure block - the originating
	// error is usually the root cause and is otherwise buried in the streamed timeline.  Reset by Prepare().
	consoleErrors []string

	requests         []*Request
	inflightRequests map[network.RequestID]bool
	requestHandlers  []*requestHandler       // ordered, first-match-wins: stub / abort / modify-request
	responseHandlers []*ResponseModification // ordered, first-match-wins: modify-response (response stage)
	fetchEnabled     bool

	// The boolean failure-artifact knobs are stored positive-sense and default to their human
	// (interactive) values, set in newBiloba; ConnectToChrome adjusts them for automation.
	debugLogging                   bool // default false
	failureScreenshots             bool // default true
	failureOutlines                bool // default false
	failureOutlinesSet             bool // whether the suite set failureOutlines explicitly
	progressReportScreenshots      bool // default true
	inlineScreenshots              bool // default true (subject to terminal support)
	inlineScreenshotsSet           bool // whether the suite set inlineScreenshots explicitly
	failureScreenshotWidth         int
	failureScreenshotHeight        int
	progressReportScreenshotWidth  int
	progressReportScreenshotHeight int
	screenshotsDir                 string
}

// inlineScreenshotsEnabled returns true when inline-image output should be
// emitted.  It respects the per-instance inlineScreenshots flag (cleared by
// BilobaConfigInlineScreenshots(false) or automation) and the package-level
// inlineImagesSupported helper (which checks BILOBA_INLINE_SCREENSHOTS / TERM_PROGRAM).
func (b *Biloba) inlineScreenshotsEnabled() bool {
	if !b.root.inlineScreenshots {
		return false
	}
	return inlineImagesSupported()
}

func (b *Biloba) GomegaString() string {
	s := &strings.Builder{}
	if b.isRootTab() {
		s.WriteString("Root ")
	}
	fmt.Fprintf(s, "Biloba Tab %p: %s (TargetID=%s, BrowserContextID=%s)", b, b.Title(), b.targetID, b.browserContextID)
	return s.String()
}

func newBiloba(ginkgoT GinkgoTInterface) *Biloba {
	b := &Biloba{
		gt:               ginkgoT,
		lock:             &sync.Mutex{},
		downloads:        map[string]*Download{},
		downloadHistory:  map[string]time.Time{},
		tabs:             map[target.ID]*Biloba{},
		inflightRequests: map[network.RequestID]bool{},

		failureScreenshots:        true,
		progressReportScreenshots: true,
		inlineScreenshots:         true,
	}
	return b
}

/*
The Chrome DevTools BrowserContextID() associated with this Biloba tab.

BrowserContextID is an isolation mechanism provided by Chrome DevTools - you may need to pass this in explicitly if you intend to make some low-level calls to chromedp.
*/
func (b *Biloba) BrowserContextID() cdp.BrowserContextID {
	return b.browserContextID
}

/*
Prepare() should be called before every spec.  It prepares the reusable Biloba tab for reuse.

Read https://onsi.github.io/biloba/#bootstrapping-biloba for details on how to set up your Ginkgo suite and use Prepare() correctly

Read https://onsi.github.io/biloba/#parallelization-how-biloba-manages-browsers-and-tabs to build a mental model of how Biloba manages tabs
*/
func (b *Biloba) Prepare() {
	if !b.isRootTab() {
		return
	}
	//close all tabs
	closedTargetIDs := []target.ID{}
	for _, tab := range b.AllTabs() {
		if !tab.isRootTab() {
			tid := chromedp.FromContext(tab.Context).Target.TargetID
			b.root.lock.Lock()
			delete(b.root.tabs, tid)
			b.root.lock.Unlock()
			tab.close()
			closedTargetIDs = append(closedTargetIDs, tid)
		}
	}
	if len(closedTargetIDs) > 0 {
		// Closing is async (see Close): wait until Chrome has truly destroyed these targets so a
		// fast-following spec's AllTabs() can't re-discover a dying tab and wedge attaching to it.
		b.waitUntilTargetsGone(closedTargetIDs)
		//closing all those tabs means we may have nuked our download config, so we reset it
		b.configureDownloadBehavior()
	}

	b.lock.Lock()
	b.downloads = map[string]*Download{}
	b.downloadHistory = map[string]time.Time{}
	b.dialogHandlers = []*DialogHandler{}
	b.dialogs = Dialogs{}
	b.consoleErrors = nil
	b.requests = nil
	b.inflightRequests = map[network.RequestID]bool{}
	b.requestHandlers = nil
	b.responseHandlers = nil
	wasFetchEnabled := b.fetchEnabled
	b.fetchEnabled = false
	b.lock.Unlock()

	// disable request interception if a previous spec stubbed requests, so the catch-all
	// pause doesn't carry into specs that don't stub
	if wasFetchEnabled {
		chromedp.Run(b.Context, fetch.Disable())
	}

	if b.failureScreenshots || b.failureOutlines {
		b.gt.DeferCleanup(b.attachFailureArtifactsIfFailed)
	}
	if b.progressReportScreenshots {
		b.gt.DeferCleanup(b.gt.AttachProgressReporter(b.progressReporter))
	}
	if os.Getenv("BILOBA_INTERACTIVE") != "" {
		b.gt.DeferCleanup(func(ctx context.Context) {
			if b.gt.Failed() {
				fmt.Println(b.gt.F("{{red}}{{bold}}This spec failed and you are running in interactive mode.  Here's a timeline of the spec:{{/}}"))
				fmt.Println(b.gt.Fi(1, b.gt.Name()))
				fmt.Println(b.gt.Fi(1, b.gt.RenderTimeline()))

				fmt.Println(b.gt.F("{{red}}{{bold}}Biloba will now sleep so you can interact with the browser.  Hit ^C when you're done to shut down the suite{{/}}"))
				<-ctx.Done()
			}
		})
	}

	// the root tab is reused between specs, so clear cookies and web storage (which otherwise
	// persist in the browser context / on the origin) to keep specs independent
	b.resetBrowsingState()

	b.Navigate("about:blank")
}

/*
NewTab() creates a new browser tab and returns a Biloba instance pointing to it

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) NewTab() *Biloba {
	_, tabTargetID, err := newIsolatedBrowserContextAndTarget(b.root.Context)
	if err != nil {
		b.gt.Fatalf("failed to create new tab: %s", err.Error())
		return nil
	}
	return b.registerTabFor(chromedp.NewContext(b.root.Context, chromedp.WithTargetID(tabTargetID)))
}

/*
AllTabs() returns all Biloba tabs currently associated with the current spec

Read https://onsi.github.io/biloba/#managing-tabs to learn more about managing tabs
*/
func (b *Biloba) AllTabs() Tabs {
	targets, err := chromedp.Targets(b.root.Context)
	if err != nil {
		b.gt.Fatalf("Failed to list tabs:\n%s", err.Error())
	}
	tabs := Tabs{}

	for _, target := range targets {
		b.root.lock.Lock()
		tab, ok := b.root.tabs[target.TargetID]
		b.root.lock.Unlock()
		if !ok {
			// this may be a new tab we've never seen before - is it ours?
			opener := b.root.tabs[target.OpenerID]
			if opener != nil {
				tab = b.root.registerTabFor(chromedp.NewContext(opener.Context, chromedp.WithTargetID(target.TargetID)))
				if tab == nil {
					continue
				}
			} else {
				continue
			}
		}
		tabs = append(tabs, tab)
	}
	return tabs
}

func (b *Biloba) isRootTab() bool {
	return b.root == b
}

/*
Close() closes a Biloba tab.  It is an error to call Close() on the reusable root tab.

There is one additional edge case in which Close() can return an error.  You can learn about it here: https://onsi.github.io/biloba/#going-the-extra-mile-for-stability

In short - if you have a test that involves both downloading files and tabs spawned by the browser (i.e. tabs that you didn't explicitly Create()) you should call:

	Eventually(tab.Close).Should(Succeed())
*/
func (b *Biloba) Close() error {
	if b.isRootTab() {
		return fmt.Errorf("invalid attempt to close the root tab")
	}

	/*
		any tabs that share this tab's BrowserContextID will fail to download things when this tab is closed that is because we need to configure chrome's download behavior on each tab in order to be able to catch downloads however closing just one tab causes chrome to clear out that download behavior

		so...

		#1 we error if an active download is in place - users must Eventually(b.CloseTab).Should(Succeed())`
	*/
	if b.root.activeDownloadsShouldBlockTabFromClosing(b) {
		return fmt.Errorf("cannot close tab because another tab is actively downloading a file and closing this tab would cause that download to fail, please try again later")
	}
	targetID := chromedp.FromContext(b.Context).Target.TargetID
	b.root.lock.Lock()
	delete(b.root.tabs, targetID)
	b.root.lock.Unlock()
	b.close()
	/*
		Closing a tab is asynchronous: b.close() blocks only until Chrome acks Target.closeTarget
		(sub-millisecond), but Chrome keeps the target visible in Target.getTargets for a few tens of
		milliseconds while it tears down.  During that window AllTabs() would re-discover the dying
		target as a brand-new tab and try to attach to it - an attach that can wedge indefinitely.  So
		we block here until the target is truly gone, keeping Close() honest: once it returns, the tab
		will not resurface.
	*/
	b.root.waitUntilTargetGone(targetID)
	/*
		#2 we must reconfigure the download behavior for all tabs with this tab's browserContextID once this tab is closed
	*/
	b.root.configureDownloadBehaviorForAllTabsWithBrowserContextID(b.browserContextID)
	return nil
}

// targetOpTimeout bounds the two places Biloba can otherwise wedge on a target that is mid-teardown:
// waiting for a closing tab to disappear, and attaching to a target during registration.  It is
// generous - these operations normally complete in tens of milliseconds - and only ever elapses for
// a target that is, in fact, going away.
const targetOpTimeout = 5 * time.Second

// waitUntilTargetGone polls Chrome until targetID is no longer reported by Target.getTargets,
// bounded by a generous deadline.  See Close() for why the post-close teardown window matters.
func (b *Biloba) waitUntilTargetGone(targetID target.ID) {
	b.waitUntilTargetsGone([]target.ID{targetID})
}

// waitUntilTargetsGone polls Chrome until none of targetIDs are reported by Target.getTargets.
// Callers close all the targets first and then wait once, so concurrent teardowns overlap and the
// wait costs ~one destruction window total rather than one per tab.
func (b *Biloba) waitUntilTargetsGone(targetIDs []target.ID) {
	remaining := map[target.ID]bool{}
	for _, id := range targetIDs {
		remaining[id] = true
	}
	deadline := time.Now().Add(targetOpTimeout)
	for len(remaining) > 0 {
		targets, err := chromedp.Targets(b.root.Context)
		if err != nil {
			return // root context is going away (e.g. suite teardown) - nothing left to wait for
		}
		present := map[target.ID]bool{}
		for _, t := range targets {
			present[t.TargetID] = true
		}
		for id := range remaining {
			if !present[id] {
				delete(remaining, id)
			}
		}
		if len(remaining) == 0 || time.Now().After(deadline) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// safeAllTabConsoleErrors gathers the console.error/console.assert messages captured across every tab
// so attachFailureArtifactsIfFailed can replay them at the top of the failure block.
func (b *Biloba) safeAllTabConsoleErrors() []string {
	out := []string{}
	for _, tab := range b.AllTabs() {
		tab.lock.Lock()
		out = append(out, tab.consoleErrors...)
		tab.lock.Unlock()
	}
	return out
}

func (b *Biloba) attachFailureArtifactsIfFailed() {
	if b.gt.Failed() {
		if consoleErrors := b.safeAllTabConsoleErrors(); len(consoleErrors) > 0 {
			b.gt.AddReportEntryVisibilityFailureOrVerbose("Console errors logged before this failure", strings.Join(consoleErrors, "\n"))
		}
		if b.failureOutlines {
			for _, outline := range b.safeAllTabOutlines() {
				if outline.failure != "" {
					b.gt.AddReportEntryVisibilityFailureOrVerbose(outline.failure)
				} else {
					b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("DOM Outline for: '%s'", outline.title), outline.text)
				}
			}
		}
		if !b.failureScreenshots {
			return
		}
		for _, screenshot := range b.safeAllTabScreenshots(b.failureScreenshotWidth, b.failureScreenshotHeight) {
			if screenshot.failure != "" {
				b.gt.AddReportEntryVisibilityFailureOrVerbose(screenshot.failure)
			} else {
				inlineEnabled := b.inlineScreenshotsEnabled()
				if screenshot.filePath != "" {
					b.gt.Printf("Screenshot for '%s' written to: %s\n", screenshot.title, screenshot.filePath)
					if inlineEnabled {
						b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("Screenshot for: '%s'", screenshot.title), fmt.Sprintf("File: %s\n\n%s", screenshot.filePath, screenshot.imgcatScreenshot))
					} else {
						b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("Screenshot for: '%s'", screenshot.title), fmt.Sprintf("File: %s", screenshot.filePath))
					}
				} else if inlineEnabled {
					b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("Screenshot for: '%s'", screenshot.title), screenshot.imgcatScreenshot)
				} else {
					b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("Screenshot for: '%s'", screenshot.title), "(inline screenshots disabled; configure BilobaConfigScreenshotsToDir to save screenshot files)")
				}
			}
		}
	}
}

func (b *Biloba) progressReporter() string {
	out := &strings.Builder{}
	inlineEnabled := b.inlineScreenshotsEnabled()
	for _, screenshot := range b.safeAllTabScreenshots(b.progressReportScreenshotWidth, b.progressReportScreenshotHeight) {
		if screenshot.failure != "" {
			out.WriteString(b.gt.F("{{red}}" + screenshot.failure + "{{/}}\n"))
		} else {
			out.WriteString(b.gt.F("{{bold}}Screenshot for: '%s'{{/}}\n", screenshot.title))
			if inlineEnabled {
				out.WriteString(screenshot.imgcatScreenshot)
				out.WriteByte('\n')
			} else if screenshot.filePath != "" {
				out.WriteString(screenshot.filePath)
				out.WriteByte('\n')
			} else {
				out.WriteString("(inline screenshots disabled; configure BilobaConfigScreenshotsToDir to save screenshot files)\n")
			}
		}
	}
	return out.String()
}

// newIsolatedBrowserContextAndTarget creates a new isolated browser context and a target
// (tab) within it, returning both IDs. Chrome 149+ requires WithNewWindow(true) when
// creating a target in a non-default browser context.
func newIsolatedBrowserContextAndTarget(ctx context.Context) (cdp.BrowserContextID, target.ID, error) {
	var browserContextID cdp.BrowserContextID
	var targetID target.ID
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		be := cdp.WithExecutor(ctx, c.Browser)
		var err error
		browserContextID, err = target.CreateBrowserContext().WithDisposeOnDetach(true).Do(be)
		if err != nil {
			return err
		}
		targetID, err = target.CreateTarget("about:blank").
			WithBrowserContextID(browserContextID).
			WithNewWindow(true).
			Do(be)
		return err
	}))
	return browserContextID, targetID, err
}

func (b *Biloba) registerTabFor(c context.Context, cancel context.CancelFunc) *Biloba {
	b.gt.Helper()
	newG := newBiloba(b.gt)
	newG.Context = c
	newG.ChromeConnection = b.ChromeConnection
	newG.downloadDir = b.downloadDir
	newG.root = b.root
	newG.close = cancel

	//spin up the tab.  This first call is what attaches chromedp to the target, and attaching to a
	//target that is mid-teardown - e.g. a tab the browser is still closing that Target.getTargets
	//momentarily still reports - can wedge forever on Runtime.Enable.  We can't bound it with a
	//context timeout: chromedp binds the target's listener to the first Run's context, so a timeout
	//there would tear the tab (and per chromedp's own docs, potentially the browser) down.  Instead we
	//watchdog it on the tab's real context and, on timeout, cancel() the tab - which unblocks the
	//wedged Run and discards the dying target.  AllTabs() already skips a nil tab.
	probeDone := make(chan error, 1)
	go func() { _, err := newG.RunErr("1"); probeDone <- err }()
	select {
	case err := <-probeDone:
		if err != nil {
			cancel()
			return nil
		}
	case <-time.After(targetOpTimeout):
		cancel()
		return nil
	}
	if ctx := chromedp.FromContext(newG.Context); ctx == nil || ctx.Target == nil {
		cancel()
		return nil
	}
	newG.targetID = chromedp.FromContext(newG.Context).Target.TargetID

	var browserContextID cdp.BrowserContextID
	err := chromedp.Run(c,
		chromedp.ActionFunc(func(ctx context.Context) error {
			info, err := target.GetTargetInfo().Do(ctx)
			browserContextID = info.BrowserContextID
			return err
		}),
	)
	if err != nil {
		b.gt.Fatalf("Failed to register new tab: %s", err.Error())
	}

	newG.browserContextID = browserContextID
	newG.setUpListeners()

	b.root.lock.Lock()
	b.root.tabs[newG.targetID] = newG
	b.root.lock.Unlock()

	return newG
}

//go:embed biloba.js
var bilobaJS string

func (b *Biloba) handleEventFrameNavigated(_ *page.EventFrameNavigated) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.bilobaIsInstalled = false
}

func (b *Biloba) ensureBiloba() {
	b.lock.Lock()
	installed := b.bilobaIsInstalled
	b.lock.Unlock()
	if installed {
		return
	}
	b.reloadBiloba()
}

func (b *Biloba) reloadBiloba() {
	b.Run(bilobaJS)
	b.lock.Lock()
	b.bilobaIsInstalled = true
	b.lock.Unlock()
}
