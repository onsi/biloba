package engine_test

import (
	"regexp"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var emulationMethod = regexp.MustCompile(`"method":"(Emulation\.\w+|Browser\.resetPermissions)"`)

// Prepare runs before every test, and every command it sends Chrome is a round trip each of those tests
// pays.  These count the commands through a debug sink on a second worker attached to the suite's
// Chrome.
var _ = Describe("what a prepare costs", func() {
	It("sends no emulation commands during a prepare when nothing was emulated, and clears only what was", func(ctx SpecContext) {
		var lock sync.Mutex
		sent := []string{}
		worker, err := engine.StartBrowser(ctx, engine.BrowserConfig{WebSocketURL: browser.WebSocketURL(), DebugSink: func(entry engine.DebugEntry) {
			if entry.Direction == engine.DebugSend {
				lock.Lock()
				sent = append(sent, entry.Message)
				lock.Unlock()
			}
		}})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(worker.Close)
		tab, err := worker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(tab.Close)
		Expect(tab.Navigate(ctx, server.URL)).To(Succeed())

		emulationCommands := func(prepare func()) []string {
			// The sink is asynchronous: a marker evaluation brackets the prepare so every command it
			// sent has been delivered once the marker after it has.
			marker := func(name string) {
				_, markerErr := tab.Evaluate(ctx, `"`+name+`"`)
				Expect(markerErr).NotTo(HaveOccurred())
				Eventually(func() bool {
					lock.Lock()
					defer lock.Unlock()
					return len(sent) > 0 && strings.Contains(strings.Join(sent, "\n"), `\"`+name+`\"`)
				}).Should(BeTrue())
			}
			marker("before-prepare")
			lock.Lock()
			start := len(sent)
			lock.Unlock()
			prepare()
			marker("after-prepare")
			lock.Lock()
			defer lock.Unlock()
			out := []string{}
			for _, message := range sent[start:] {
				if method := emulationMethod.FindStringSubmatch(message); method != nil {
					out = append(out, method[1])
				}
			}
			return out
		}

		// applyViewport sets the window size at the end of every prepare; nothing else may appear.
		viewportOnly := []string{"Emulation.setDeviceMetricsOverride", "Emulation.setTouchEmulationEnabled"}
		Expect(emulationCommands(func() { Expect(tab.Prepare(ctx)).To(Succeed()) })).To(Equal(viewportOnly))
		Expect(worker.DebugDropped()).To(BeZero())

		Expect(tab.SetLocale(ctx, "fr-FR")).To(Succeed())
		Expect(tab.SetMedia(ctx, engine.Media{ColorScheme: "dark"})).To(Succeed())
		Expect(emulationCommands(func() { Expect(tab.Prepare(ctx)).To(Succeed()) })).To(Equal(append([]string{"Emulation.setLocaleOverride", "Emulation.setEmulatedMedia"}, viewportOnly...)))

		Expect(tab.SetPermissions(ctx, server.URL, map[engine.Permission]engine.PermissionState{engine.PermissionGeolocation: engine.PermissionGranted})).To(Succeed())
		Expect(emulationCommands(func() { Expect(tab.Prepare(ctx)).To(Succeed()) })).To(Equal(append([]string{"Browser.resetPermissions"}, viewportOnly...)))

		Expect(tab.SetTimezone(ctx, "America/New_York")).To(Succeed())
		Expect(tab.ClearTimezone(ctx)).To(Succeed())
		Expect(emulationCommands(func() { Expect(tab.Prepare(ctx)).To(Succeed()) })).To(Equal(viewportOnly), "a cleared override is no longer outstanding")
		Expect(worker.DebugDropped()).To(BeZero())
	})
})
