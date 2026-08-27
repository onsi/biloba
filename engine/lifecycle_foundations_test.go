package engine_test

import (
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
		Expect(browser.Sessions()).To(ContainElements(root, sibling))

		Expect(root.Prepare(ctx)).To(Succeed())
		_, err = sibling.Evaluate(ctx, "1")
		Expect(err).To(MatchError(ContainSubstring("session is closed")))
		Expect(browser.Sessions()).To(ConsistOf(root))
	})

	It("invalidates descendant handles when their context owner closes", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(root.Close()).To(Succeed())
		_, err = sibling.Evaluate(ctx, "1")
		Expect(err).To(MatchError(ContainSubstring("session is closed")))
		Expect(browser.Sessions()).NotTo(ContainElements(root, sibling))
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
})

func selectorPtr(selector engine.Selector) *engine.Selector { return &selector }
