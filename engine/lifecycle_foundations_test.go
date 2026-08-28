package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("lifecycle engine foundations", func() {
	It("exposes ownership metadata and closes every context descendant during root prepare", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(root.ContextID()).NotTo(BeEmpty())
		Expect(root.TargetID()).NotTo(BeEmpty())
		Expect(root.OwnsContext()).To(BeTrue())
		Expect(sibling.ContextID()).To(Equal(root.ContextID()))
		Expect(sibling.OwnsContext()).To(BeFalse())
		Expect(sessionTargetIDs(browser.Sessions())).To(ContainElements(string(root.TargetID()), string(sibling.TargetID())))

		Expect(root.Prepare(ctx)).To(Succeed())
		_, err = sibling.Evaluate(ctx, "1")
		Expect(err).To(MatchError(ContainSubstring("session is closed")))
		Expect(sessionTargetIDs(browser.Sessions())).To(ConsistOf(string(root.TargetID())))
	})

	It("invalidates descendant handles when their context owner closes", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(root.Close()).To(Succeed())
		_, err = sibling.Evaluate(ctx, "1")
		Expect(err).To(MatchError(ContainSubstring("session is closed")))
		Expect(sessionTargetIDs(browser.Sessions())).NotTo(ContainElements(string(root.TargetID()), string(sibling.TargetID())))
	})

	It("discovers a spawned tab and can wait for it by typed query", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())

		_, err = root.Evaluate(ctx, `void window.open("/destination", "_blank")`)
		Expect(err).NotTo(HaveOccurred())
		tab, err := root.WaitForTab(ctx, engine.TabQuery{
			SpawnedOnly: true,
			URL:         &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"},
			HasElement:  selectorPtr(engine.TestID("destination")),
		}, engine.PollPolicy{Timeout: time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		Expect(tab).NotTo(BeNil())
		Expect(tab.ContextID()).To(Equal(root.ContextID()))
	})

	It("reconciles a popup that closes itself and keeps prepare idempotent", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())

		_, err = root.Evaluate(ctx, `void window.open("/destination", "_blank")`)
		Expect(err).NotTo(HaveOccurred())
		popup, err := root.WaitForTab(ctx, engine.TabQuery{
			SpawnedOnly: true,
			URL:         &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"},
		}, engine.PollPolicy{Timeout: time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())

		_, err = popup.Evaluate(ctx, `window.close()`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() []string { return sessionTargetIDs(browser.Sessions()) }).ShouldNot(ContainElement(string(popup.TargetID())))
		Expect(popup.Close()).To(Succeed())
		Expect(root.Prepare(ctx)).To(Succeed())
		Expect(sessionTargetIDs(browser.Sessions())).To(ConsistOf(string(root.TargetID())))
	})

	It("reports descendant discovery failure instead of continuing prepare", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)

		canceled, cancel := context.WithCancel(ctx)
		cancel()
		err = root.Prepare(canceled)
		Expect(err).To(MatchError(ContainSubstring("list tabs")))
	})

	It("closes a popup spawned by a descendant while prepare is closing it", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(sibling.Navigate(ctx, server.URL)).To(Succeed())

		started := make(chan struct{})
		spawned := make(chan error, 1)
		go func() {
			close(started)
			_, evaluateErr := sibling.Evaluate(ctx, `(() => {
				const until = performance.now() + 150
				while (performance.now() < until) {}
				window.open("/destination", "_blank")
			})()`)
			spawned <- evaluateErr
		}()
		<-started
		time.Sleep(25 * time.Millisecond)

		Expect(root.Prepare(ctx)).To(Succeed())
		Expect(<-spawned).NotTo(HaveOccurred())
		tabs, err := root.Tabs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(sessionTargetIDs(tabs)).To(ConsistOf(string(root.TargetID())))
		Expect(sessionTargetIDs(browser.Sessions())).To(ConsistOf(string(root.TargetID())))
	})

	It("provides typed cookie, storage, page, and defined-JavaScript operations", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		local := session.Storage(engine.StorageLocal)
		Expect(local.Set(ctx, "count", 3)).To(Succeed())
		Expect(local.Set(ctx, "raw", "value")).To(Succeed())
		value, found, err := local.Get(ctx, "count")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(value).To(BeNumerically("==", 3))
		length, err := local.Length(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(length).To(Equal(2))
		all, err := local.GetAll(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(Equal(map[string]any{"count": float64(3), "raw": "value"}))
		Expect(local.Remove(ctx, "raw")).To(Succeed())
		Expect(local.Clear(ctx)).To(Succeed())

		_, err = session.Evaluate(ctx, `setTimeout(() => window.__readyValue = {ready: true}, 30)`)
		Expect(err).NotTo(HaveOccurred())
		defined, err := session.WaitForDefined(ctx, `window.__readyValue`, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(defined.Final.Value).To(Equal(map[string]any{"ready": true}))

		_, err = session.Evaluate(ctx, `document.title = "Lifecycle"`)
		Expect(err).NotTo(HaveOccurred())
		title, err := session.Title(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(title).To(Equal("Lifecycle"))
		width, height, err := session.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(width).To(BeNumerically(">", 0))
		Expect(height).To(BeNumerically(">", 0))
		outline, err := session.Outline(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(outline).To(ContainSubstring(`<button aria-label="Save">`))
		a11y, err := session.AccessibilityOutline(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(a11y).To(ContainSubstring(`button "Save"`))

		Expect(session.SetCookies(ctx, []engine.Cookie{{Name: "clear-me", Value: "yes", Domain: "127.0.0.1", Path: "/"}})).To(Succeed())
		Expect(session.ClearCookies(ctx)).To(Succeed())
		cookies, err := session.GetCookies(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(cookies).To(BeEmpty())
	})

	It("records console events and clears their history during prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `console.warn("careful", {count: 2})`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(session.ConsoleMessages).Should(ContainElement(SatisfyAll(
			HaveField("Type", Equal("warning")),
			HaveField("Text", ContainSubstring("careful")),
		)))
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.ConsoleMessages()).To(BeEmpty())
	})

	It("bounds console history, reports drops, and preserves safe previews and stack metadata", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		_, err = session.Evaluate(ctx, `(() => {
			for (let i = 0; i < 1005; i++) console.log("bounded-" + i)
			console.error("stacked", "x".repeat(10000))
		})()`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() int { return len(session.ConsoleSnapshot().Messages) }).Should(Equal(engine.DefaultEventHistoryLimit))
		snapshot := session.ConsoleSnapshot()
		Expect(snapshot.Dropped).To(BeNumerically(">=", 6))
		stacked := snapshot.Messages[len(snapshot.Messages)-1]
		Expect(stacked.Text).To(HavePrefix("stacked "))
		Expect(len(stacked.Text)).To(BeNumerically("<=", engine.DefaultConsolePreviewBytes*2))
		Expect(stacked.Args[1]).To(And(BeAssignableToTypeOf(""), HaveSuffix("… [truncated]")))
		Expect(stacked.Stack).NotTo(BeEmpty())
		Expect(stacked.Stack[0].Line).To(BeNumerically(">=", 0))
		Expect(stacked.Stack[0].Column).To(BeNumerically(">=", 0))
	})

	It("delivers console subscriptions without blocking Chrome and resets generations", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		subscription, err := session.SubscribeConsole(1)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(subscription.Close)

		started := time.Now()
		_, err = session.Evaluate(ctx, `for (let i = 0; i < 200; i++) console.log("queued-" + i)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(time.Since(started)).To(BeNumerically("<", time.Second))
		Eventually(subscription.Dropped).Should(BeNumerically(">", 0))
		var before engine.ConsoleMessage
		Eventually(subscription.Events()).Should(Receive(&before))

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `console.log("after-prepare")`)
		Expect(err).NotTo(HaveOccurred())
		var after engine.ConsoleMessage
		Eventually(subscription.Events()).Should(Receive(&after))
		Expect(after.Text).To(Equal("after-prepare"))
		Expect(after.Generation).To(BeNumerically(">", before.Generation))
		Expect(session.ConsoleSnapshot().Messages).To(ConsistOf(HaveField("Text", "after-prepare")))
		Expect(session.ConsoleSnapshot().Dropped).To(BeZero())
	})

	It("isolates console subscriptions and closes them with the session", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())
		subscription, err := first.SubscribeConsole(4)
		Expect(err).NotTo(HaveOccurred())

		_, err = second.Evaluate(ctx, `console.log("other-session")`)
		Expect(err).NotTo(HaveOccurred())
		Consistently(subscription.Events(), 100*time.Millisecond).ShouldNot(Receive())
		Expect(first.Close()).To(Succeed())
		Eventually(subscription.Events()).Should(BeClosed())
		Expect(subscription.Close()).To(Succeed())
	})

	It("silences event subscriptions across a crash and resumes them on recovery", func(ctx SpecContext) {
		// A renderer crash must not end a subscription: Go's console stream is bound to the tab
		// context, which a crash does not tear down, so "navigate again to recover it" restores
		// eventing too.  Ending it here left a listener registered before the crash silently receiving
		// nothing for the rest of the session - and re-registering did not help, because the client
		// only re-subscribes when it has no subscription at all.
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		console, err := session.SubscribeConsole(4)
		Expect(err).NotTo(HaveOccurred())
		warnings, err := session.SubscribeWarnings(4)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `console.log("before")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(console.Events()).Should(Receive())

		engine.MarkSessionCrashedForTest(session)
		Consistently(console.Events()).ShouldNot(BeClosed())
		Consistently(warnings.Events()).ShouldNot(BeClosed())

		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `console.log("after recovery")`)
		Expect(err).NotTo(HaveOccurred())
		var message engine.ConsoleMessage
		Eventually(console.Events()).Should(Receive(&message))
		Expect(message.Text).To(ContainSubstring("after recovery"))

		engine.EmitWarningForTest(session, engine.Warning{Message: "post-crash warning"})
		Eventually(warnings.Events()).Should(Receive())
	})

	It("removes registered init scripts during prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.AddInitScript(ctx, `window.__leakedInit = true`)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		value, err := session.Evaluate(ctx, `window.__leakedInit`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeTrue())

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		value, err = session.Evaluate(ctx, `typeof window.__leakedInit`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("undefined"))
	})

	It("applies typed emulation and restores every override during prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		Expect(session.SetDeviceMetrics(ctx, engine.DeviceMetrics{Width: 390, Height: 844, DeviceScaleFactor: 2, Mobile: true})).To(Succeed())
		Expect(session.SetLocale(ctx, "fr-FR")).To(Succeed())
		Expect(session.SetTimezone(ctx, "America/New_York")).To(Succeed())
		Expect(session.SetMedia(ctx, engine.Media{ColorScheme: "dark", ReducedMotion: "reduce"})).To(Succeed())
		state, err := session.Evaluate(ctx, `[screen.width, screen.height, devicePixelRatio, matchMedia("(prefers-color-scheme: dark)").matches, matchMedia("(prefers-reduced-motion: reduce)").matches, Intl.DateTimeFormat().resolvedOptions().timeZone]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal([]any{float64(390), float64(844), float64(2), true, true, "America/New_York"}))

		Expect(session.SetPermissions(ctx, server.URL, map[engine.Permission]engine.PermissionState{engine.PermissionGeolocation: engine.PermissionGranted})).To(Succeed())
		Expect(session.SetGeolocation(ctx, engine.Geolocation{Latitude: 40.7, Longitude: -74, Accuracy: 10})).To(Succeed())

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		reset, err := session.Evaluate(ctx, `[innerWidth === 390, devicePixelRatio === 2, matchMedia("(prefers-color-scheme: dark)").matches, matchMedia("(prefers-reduced-motion: reduce)").matches, Intl.DateTimeFormat().resolvedOptions().timeZone === "America/New_York"]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(reset).To(Equal([]any{false, false, false, false, false}))
	})

	It("honors runner-neutral Chrome mode, arguments, and starting size", func(ctx SpecContext) {
		launched, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Mode:           engine.ChromeModeHeadless,
			Arguments:      []string{"--user-agent=BilobaLifecycleTest"},
			WindowWidth:    640,
			WindowHeight:   480,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(launched.Close)
		session, err := launched.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		width, height, err := session.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(width).To(Equal(640))
		Expect(height).To(Equal(480))
		userAgent, err := session.Evaluate(ctx, `navigator.userAgent`)
		Expect(err).NotTo(HaveOccurred())
		Expect(userAgent).To(Equal("BilobaLifecycleTest"))
	})

	It("uses Go's 1024 by 768 starting size when none is configured", func(ctx SpecContext) {
		launched, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath()})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(launched.Close)
		session, err := launched.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)

		width, height, err := session.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(width).To(Equal(1024))
		Expect(height).To(Equal(768))

		metadata := launched.LaunchMetadata()
		Expect(metadata.Mode).To(Equal(engine.ChromeModeHeadlessShell))
		Expect(metadata.ExecutablePath).To(Equal(chromePath()))
		Expect(metadata.WindowWidth).To(Equal(1024))
		Expect(metadata.WindowHeight).To(Equal(768))
		Expect(metadata.Attached).To(BeFalse())
		metadata.Arguments = append(metadata.Arguments, "--mutated")
		Expect(launched.LaunchMetadata().Arguments).To(BeEmpty(), "metadata snapshots must not mutate browser state")
	})

	It("rejects malformed raw Chrome arguments before launching", func(ctx SpecContext) {
		_, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Arguments:      []string{"--bad name=value"},
		})
		Expect(err).To(MatchError(ContainSubstring("Chrome argument name")))
	})

	It("accepts headful mode as a full-Chrome launch mode", func(ctx SpecContext) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := engine.StartBrowser(canceled, engine.BrowserConfig{
			ExecutablePath: "/usr/bin/google-chrome",
			Mode:           engine.ChromeMode("headful"),
			Arguments:      []string{"--no-sandbox"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("mode must"))
	})

	It("resolves an installed full Chrome for high-fidelity mode", func(ctx SpecContext) {
		launched, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			Mode:      engine.ChromeModeHeadless,
			Arguments: []string{"--no-sandbox"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(launched.Close)
		Expect(launched.LaunchMetadata().ExecutablePath).NotTo(BeEmpty())
	})

	It("does not invent launch metadata for an attached browser", func(ctx SpecContext) {
		attached, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			WebSocketURL: browser.WebSocketURL(),
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(attached.Close)

		metadata := attached.LaunchMetadata()
		Expect(metadata.Attached).To(BeTrue())
		Expect(metadata.Mode).To(BeEmpty())
		Expect(metadata.ExecutablePath).To(BeEmpty())
		Expect(metadata.WindowWidth).To(BeZero())
		Expect(metadata.WindowHeight).To(BeZero())

		session, err := attached.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		width, height, err := session.WindowSize(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(width).To(Equal(1024))
		Expect(height).To(Equal(768))
	})

	It("keeps the default mode compatible with an explicitly supplied full Chrome", func(ctx SpecContext) {
		launched, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: engine.LocateFullChrome(""),
			Arguments:      []string{"--no-sandbox"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(launched.Close)
		Expect(launched.LaunchMetadata().Mode).To(Equal(engine.ChromeModeHeadlessShell))
		session, err := launched.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		value, err := session.Evaluate(ctx, `1 + 1`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeNumerically("==", 2))
	})

	It("never installs the headless shell unless explicitly opted in", func(ctx SpecContext) {
		GinkgoT().Setenv(engine.ChromeEnvVar, "")
		GinkgoT().Setenv("PATH", "")
		GinkgoT().Setenv("HOME", GinkgoT().TempDir())
		GinkgoT().Setenv("XDG_CACHE_HOME", GinkgoT().TempDir())
		installedPath := filepath.Join(GinkgoT().TempDir(), engine.ChromeBinaryName())
		Expect(os.WriteFile(installedPath, []byte("binary"), 0o755)).To(Succeed())
		calls := 0
		restore := engine.SetHeadlessShellInstallerForTest(func(context.Context) (string, error) {
			calls++
			return installedPath, nil
		})
		DeferCleanup(restore)

		_, _, err := engine.ResolveHeadlessShell(ctx, "", false)
		Expect(err).To(MatchError(ContainSubstring("could not find chrome-headless-shell")))
		Expect(calls).To(BeZero())

		path, autoInstalled, err := engine.ResolveHeadlessShell(ctx, "", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(Equal(installedPath))
		Expect(autoInstalled).To(BeTrue())
		Expect(calls).To(Equal(1))
	})

	It("reasserts the high-fidelity viewport after navigation and restores it during prepare", func(ctx SpecContext) {
		launched, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: "/usr/bin/google-chrome",
			Mode:           engine.ChromeModeHeadless,
			Arguments:      []string{"--no-sandbox"},
			WindowWidth:    640,
			WindowHeight:   480,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(launched.Close)
		session, err := launched.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)

		Expect(session.SetWindowSize(ctx, 700, 650)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		state, err := session.Evaluate(ctx, `[innerWidth, innerHeight, screen.width, screen.height]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal([]any{float64(700), float64(650), float64(700), float64(650)}))

		_, err = session.Evaluate(ctx, `void window.open("about:blank", "_blank")`)
		Expect(err).NotTo(HaveOccurred())
		spawned, err := session.WaitForTab(ctx, engine.TabQuery{SpawnedOnly: true}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		spawnedState, err := spawned.Evaluate(ctx, `[innerWidth, innerHeight, screen.width, screen.height]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(spawnedState).To(Equal([]any{float64(640), float64(480), float64(640), float64(480)}))

		Expect(session.Prepare(ctx)).To(Succeed())
		state, err = session.Evaluate(ctx, `[innerWidth, innerHeight, screen.width, screen.height]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal([]any{float64(640), float64(480), float64(640), float64(480)}))
	})

	It("emits structured bounded CDP debug entries through the tabless allocator", func(ctx SpecContext) {
		entries := make(chan engine.DebugEntry, engine.DefaultDebugQueueSize*2)
		debugBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			DebugSink:      func(entry engine.DebugEntry) { entries <- entry },
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(debugBrowser.Close)
		session, err := debugBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		_, err = session.Evaluate(ctx, `"`+strings.Repeat("x", engine.DefaultDebugEntryBytes*2)+`"`)
		Expect(err).NotTo(HaveOccurred())

		var sent, received engine.DebugEntry
		Eventually(entries).Should(Receive(&sent, HaveField("Direction", engine.DebugSend)))
		Eventually(entries).Should(Receive(&received, HaveField("Direction", engine.DebugReceive)))
		Expect(len(sent.Message)).To(BeNumerically("<=", engine.DefaultDebugEntryBytes+len("… [truncated]")))
		Expect(sent.Timestamp).NotTo(BeZero())
		Expect(received.Timestamp).NotTo(BeZero())
	})

	It("never lets a slow or panicking debug sink block Chrome operations", func(ctx SpecContext) {
		release := make(chan struct{})
		blocked := make(chan struct{}, 1)
		debugBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			DebugSink: func(engine.DebugEntry) {
				select {
				case blocked <- struct{}{}:
				default:
				}
				<-release
			},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { close(release); _ = debugBrowser.Close() })
		Eventually(blocked).Should(Receive())
		session, err := debugBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		started := time.Now()
		for i := 0; i < engine.DefaultDebugQueueSize*2; i++ {
			_, err = session.Evaluate(ctx, `1`)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
		Expect(debugBrowser.DebugDropped()).To(BeNumerically(">", 0))

		panicBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(), DebugSink: func(engine.DebugEntry) { panic("sink") },
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(panicBrowser.Close)
		panicSession, err := panicBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = panicSession.Evaluate(ctx, `2`)
		Expect(err).NotTo(HaveOccurred())
	})
})

func selectorPtr(selector engine.Selector) *engine.Selector { return &selector }

func sessionTargetIDs(sessions []*engine.Session) []string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, string(session.TargetID()))
	}
	return ids
}
