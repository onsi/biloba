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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const BILOBA_VERSION = "0.1.6"

/*
GinkgoTInterface is the interface by which Biloba receives GinkgoT()
*/
type GinkgoTInterface interface {
	Name() string
	Helper()
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	TempDir() string
	Logf(format string, args ...any)
	Failed() bool

	GinkgoRecover()
	DeferCleanup(args ...any)
	Print(args ...any)
	Printf(format string, args ...any)
	Println(a ...interface{})
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
	WebSocketURL  string
	WindowWidth   int
	WindowHeight  int
}

func (gc ChromeConnection) encode() []byte {
	data, _ := json.Marshal(gc)
	return data
}

/*
Pass StartingWindowSize into [SpinUpChrome] to set the default window size for all tabs
*/
func StartingWindowSize(x int, y int) chromedp.ExecAllocatorOption {
	return chromedp.WindowSize(x, y)
}

func gooseConfigPath(process int) string {
	return fmt.Sprintf("./.biloba-config-%d", process)
}

/*
Call SpinUpChrome(GinkgoT()) to spin up a Chrome browser

Read https://onsi.github.io/biloba/#bootstrapping-biloba for details on how to set up your Ginkgo suite and use SpinUpChrome correctly
*/
func SpinUpChrome(ginkgoT GinkgoTInterface, options ...chromedp.ExecAllocatorOption) ChromeConnection {
	ginkgoT.Helper()
	tmp := ginkgoT.TempDir()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		StartingWindowSize(1024, 768),
		chromedp.UserDataDir(tmp),
	)
	opts = append(opts, options...)
	if os.Getenv("BILOBA_INTERACTIVE") != "" {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ginkgoT.DeferCleanup(cancel)

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	ginkgoT.DeferCleanup(cancel)

	// Read the outer window dimensions — these reflect --window-size and give us the
	// correct target viewport for all isolated tabs (window.innerHeight < outerHeight
	// in Chrome 149+ headless mode due to virtual browser chrome).
	var outerDims []int
	err := chromedp.Run(browserCtx,
		chromedp.Evaluate("[window.outerWidth, window.outerHeight]", &outerDims),
	)
	if err != nil {
		ginkgoT.Fatalf("failed to spin up chrome: %w", err)
		return ChromeConnection{}
	}
	bs, err := os.ReadFile(filepath.Join(tmp, "DevToolsActivePort"))
	if err != nil {
		ginkgoT.Fatalf("failed to spin up chrome: %w", err)
		return ChromeConnection{}
	}

	components := strings.Split(string(bs), "\n")

	cc := ChromeConnection{
		WebSocketURL: fmt.Sprintf("ws://127.0.0.1:%s%s", components[0], components[1]),
	}
	if len(outerDims) == 2 {
		cc.WindowWidth = outerDims[0]
		cc.WindowHeight = outerDims[1]
	}

	os.WriteFile(gooseConfigPath(ginkgoT.ParallelProcess()), cc.encode(), 0744)
	ginkgoT.DeferCleanup(func() error {
		chromedp.Cancel(browserCtx)
		return os.Remove(gooseConfigPath(ginkgoT.ParallelProcess()))
	})

	return cc
}

/*
BilobaConfigOptions are passed in to [ConnectToChrome] to configure a given connection to Chrome
*/
type BilobaConfigOption func(*Biloba)

/*
Pass BilobaConfigEnableDebugLogging to [ConnectToChrome] to send all Chrome debug logging to the GinkgoWriter
*/
func BilobaConfigEnableDebugLogging() func(*Biloba) {
	return func(b *Biloba) {
		b.enableDebugLogging = true
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
Pass BilobaConfigWithChromeConnection to [ConnectToChrome] to disable screenshots on failure
*/
func BilobaConfigDisableFailureScreenshots() func(*Biloba) {
	return func(b *Biloba) {
		b.disableFailureScreenshots = true
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
Pass BilobaConfigDisableProgressReportScreenshots to [ConnectToChrome] to disable screenshots when Progress Reports are emitted
*/
func BilobaConfigDisableProgressReportScreenshots() func(*Biloba) {
	return func(b *Biloba) {
		b.disableProgressReportScreenshots = true
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
	if b.enableDebugLogging {
		bootstrapOpts = append(bootstrapOpts,
			chromedp.WithDebugf(b.gt.Logf),
			chromedp.WithLogf(b.gt.Logf),
			chromedp.WithErrorf(b.gt.Logf),
		)
	}
	bootstrapCtx, cancelBootstrap := chromedp.NewContext(allocatorContext, bootstrapOpts...)
	b.gt.DeferCleanup(cancelBootstrap)

	if err := chromedp.Run(bootstrapCtx, chromedp.Evaluate("1", nil)); err != nil {
		ginkgoT.Fatalf("failed to connect to chrome: %w", err)
		return nil
	}

	browserContextID, isolatedTargetID, err := newIsolatedBrowserContextAndTarget(bootstrapCtx)
	if err != nil {
		ginkgoT.Fatalf("failed to create isolated chrome context: %w", err)
		return nil
	}

	b.Context, cancel = chromedp.NewContext(bootstrapCtx, chromedp.WithTargetID(isolatedTargetID))
	b.gt.DeferCleanup(cancel)
	_, err = b.RunErr("1")

	if err != nil {
		ginkgoT.Fatalf("failed to connect to chrome: %w", err)
		return nil
	}

	b.targetID = chromedp.FromContext(b.Context).Target.TargetID
	b.browserContextID = browserContextID

	// Chrome 149+ headless mode reports window.innerHeight smaller than --window-size due to
	// virtual browser chrome. Apply EmulateViewport using the outer window dimensions stored
	// in ChromeConnection (set by SpinUpChrome) to give each isolated tab the correct viewport.
	if b.ChromeConnection.WindowWidth > 0 && b.ChromeConnection.WindowHeight > 0 {
		if err := chromedp.Run(b.Context, chromedp.EmulateViewport(
			int64(b.ChromeConnection.WindowWidth),
			int64(b.ChromeConnection.WindowHeight),
		)); err != nil {
			ginkgoT.Fatalf("failed to set initial window size: %w", err)
			return nil
		}
	}

	b.downloadDir = b.gt.TempDir()
	b.setUpListeners()

	b.lock.Lock()
	b.tabs[chromedp.FromContext(b.Context).Target.TargetID] = b
	b.lock.Unlock()

	return b
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

	downloadDir     string
	downloads       map[string]*Download
	downloadHistory map[string]time.Time

	dialogHandlers []*DialogHandler
	dialogs        []*Dialog

	enableDebugLogging               bool
	disableFailureScreenshots        bool
	disableProgressReportScreenshots bool
	failureScreenshotWidth           int
	failureScreenshotHeight          int
	progressReportScreenshotWidth    int
	progressReportScreenshotHeight   int
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
		gt:              ginkgoT,
		lock:            &sync.Mutex{},
		downloads:       map[string]*Download{},
		downloadHistory: map[string]time.Time{},
		tabs:            map[target.ID]*Biloba{},
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
	closedTabs := false
	for _, tab := range b.AllTabs() {
		if !tab.isRootTab() {
			b.root.lock.Lock()
			delete(b.root.tabs, chromedp.FromContext(tab.Context).Target.TargetID)
			b.root.lock.Unlock()
			tab.close()
			closedTabs = true
		}
	}
	//closing all those tabs means we may have nuked our download config, so we reset it
	if closedTabs {
		b.configureDownloadBehavior()
	}

	b.lock.Lock()
	b.downloads = map[string]*Download{}
	b.downloadHistory = map[string]time.Time{}
	b.dialogHandlers = []*DialogHandler{}
	b.dialogs = Dialogs{}
	b.lock.Unlock()

	if !b.disableFailureScreenshots {
		b.gt.DeferCleanup(b.attachScreenshotsIfFailed)
	}
	if !b.disableProgressReportScreenshots {
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
	b.root.lock.Lock()
	delete(b.root.tabs, chromedp.FromContext(b.Context).Target.TargetID)
	b.root.lock.Unlock()
	b.close()
	/*
		#2 we must reconfigure the download behavior for all tabs with this tab's browserContextID once this tab is closed
	*/
	b.root.configureDownloadBehaviorForAllTabsWithBrowserContextID(b.browserContextID)
	return nil
}

func (b *Biloba) attachScreenshotsIfFailed() {
	if b.gt.Failed() {
		for _, outline := range b.safeAllTabOutlines() {
			if outline.failure != "" {
				b.gt.AddReportEntryVisibilityFailureOrVerbose(outline.failure)
			} else {
				b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("DOM Outline for: '%s'", outline.title), outline.text)
			}
		}
		for _, screenshot := range b.safeAllTabScreenshots(b.failureScreenshotWidth, b.failureScreenshotHeight) {
			if screenshot.failure != "" {
				b.gt.AddReportEntryVisibilityFailureOrVerbose(screenshot.failure)
			} else {
				b.gt.AddReportEntryVisibilityFailureOrVerbose(fmt.Sprintf("Screenshot for: '%s'", screenshot.title), screenshot.imgcatScreenshot)
			}
		}
	}
}

func (b *Biloba) progressReporter() string {
	out := ""
	for _, screenshot := range b.safeAllTabScreenshots(b.progressReportScreenshotWidth, b.progressReportScreenshotHeight) {
		if screenshot.failure != "" {
			out += b.gt.F("{{red}}" + screenshot.failure + "{{/}}\n")
		} else {
			out += b.gt.F("{{bold}}Screenshot for: '%s'{{/}}\n", screenshot.title)
			out += screenshot.imgcatScreenshot + "\n"
		}
	}
	return out
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

	//spin up the tab
	if _, err := newG.RunErr("1"); err != nil {
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

func (b *Biloba) handleEventFrameNavigated(ev *page.EventFrameNavigated) {
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
