package biloba_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Emulation", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("SetMedia", func() {
		// The full-chrome lane follows the OS colour scheme, so the specs read the ambient scheme and
		// emulate the other one: a hardcoded "dark" would be a no-op on a machine already in dark mode.
		var ambient, other string
		BeforeEach(func() {
			ambient, other = "light", "dark"
			if b.Run(`matchMedia("(prefers-color-scheme: dark)").matches`) == true {
				ambient, other = "dark", "light"
			}
		})

		prefers := func(feature string) string {
			return `matchMedia("(` + feature + `)").matches`
		}

		Describe("emulating and resetting", Ordered, func() {
			It("emulates the colour scheme, reduced motion, and media type, across navigations", func() {
				b.SetMedia(biloba.Media{ColorScheme: other, ReducedMotion: "reduce", Type: "print"})
				Ω(prefers("prefers-color-scheme: " + other)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
				Ω(`matchMedia("print").matches`).Should(b.EvaluateTo(true))

				b.Navigate(fixtureServer + "/dom.html")
				Eventually("#hello").Should(b.Exist())
				Ω(prefers("prefers-color-scheme: " + other)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
			})

			It("resets it for the next spec", func() {
				Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(false))
				Ω(`matchMedia("print").matches`).Should(b.EvaluateTo(false))
			})
		})

		It("replaces the previous media rather than merging into it", func() {
			b.SetMedia(biloba.Media{ColorScheme: other, ReducedMotion: "reduce"})
			b.SetMedia(biloba.Media{ReducedMotion: "reduce"})
			Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
			Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
		})

		It("is per tab", func() {
			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())
			tab.SetMedia(biloba.Media{ColorScheme: other})
			Ω(prefers("prefers-color-scheme: " + other)).Should(tab.EvaluateTo(true))
			Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
		})

		It("rejects values Chrome would silently ignore", func() {
			b.SetMedia(biloba.Media{ColorScheme: "midnight"})
			b.SetMedia(biloba.Media{ReducedMotion: "less"})
			b.SetMedia(biloba.Media{Type: "tv"})
			ExpectFailures(
				`SetMedia: Media.ColorScheme must be one of ["light" "dark"], got "midnight"`,
				`SetMedia: Media.ReducedMotion must be one of ["reduce" "no-preference"], got "less"`,
				`SetMedia: Media.Type must be one of ["screen" "print"], got "tv"`,
			)
		})

		It("is a one-shot mutation, so it rejects the poll knobs", func() {
			b.Immediate().SetMedia(biloba.Media{ColorScheme: other})
			ExpectFailures(ContainSubstring("SetMedia does not support Immediate"))
		})
	})

	Describe("SetUserAgent", func() {
		const agent = "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) BilobaTest/1.0"

		Describe("overriding and restoring", Ordered, func() {
			It("overrides navigator.userAgent and the User-Agent header for the documents the tab loads", func() {
				b.SetUserAgent(agent)
				b.Navigate(fixtureServer + "/dom.html?ua")
				Eventually("#hello").Should(b.Exist())
				Ω(`navigator.userAgent`).Should(b.EvaluateTo(agent))
				request := b.AllRequests().Find(b.RequestMatching(fixtureServer + "/dom.html?ua"))
				Ω(request).ShouldNot(BeNil())
				Ω(request.Headers).Should(HaveKeyWithValue("User-Agent", agent))
			})

			It("restores the browser's own user agent for the next spec", func() {
				Ω(`navigator.userAgent`).ShouldNot(b.EvaluateTo(agent))
				Ω(`navigator.userAgent`).Should(b.EvaluateTo(ContainSubstring("Chrome")))
			})
		})

		It("unwinds nested overrides in order", func() {
			b.SetUserAgent("first")
			DeferCleanup(func() {
				b.Navigate(fixtureServer + "/dom.html")
				Ω(`navigator.userAgent`).Should(b.EvaluateTo("first"), "the second override's cleanup restores the first")
			})
			b.SetUserAgent("second")
			b.Navigate(fixtureServer + "/dom.html")
			Ω(`navigator.userAgent`).Should(b.EvaluateTo("second"))
		})
	})

	permissionState := func(tab *biloba.Biloba, name string) string {
		return tab.RunAsync(`return (await navigator.permissions.query({name: "` + name + `"})).state`).(string)
	}

	Describe("SetGeolocation", func() {
		// resolves with [latitude, longitude, accuracy], or with the error message
		position := func() any {
			return b.RunAsync(`return await new Promise(resolve => navigator.geolocation.getCurrentPosition(
				p => resolve([p.coords.latitude, p.coords.longitude, p.coords.accuracy]),
				e => resolve(e.message || "error " + e.code),
				{timeout: 200}))`)
		}
		grant := func() {
			b.SetPermissions(fixtureServer, map[biloba.Permission]biloba.PermissionState{biloba.PermissionGeolocation: biloba.PermissionGranted})
		}

		Describe("emulating and restoring", Ordered, func() {
			var ambientPermission string
			It("reports the emulated position across navigations", func() {
				ambientPermission = permissionState(b, "geolocation")
				grant()
				b.SetGeolocation(biloba.Geolocation{Latitude: 48.8584, Longitude: 2.2945, Accuracy: 10})
				Ω(position()).Should(Equal([]any{48.8584, 2.2945, 10.0}))
				b.Navigate(fixtureServer + "/dom.html")
				Eventually("#hello").Should(b.Exist())
				Ω(position()).Should(Equal([]any{48.8584, 2.2945, 10.0}))
			})

			It("restores the position and the permission for the next spec", func() {
				Ω(permissionState(b, "geolocation")).Should(Equal(ambientPermission))
				grant()
				Ω(position()).ShouldNot(Equal([]any{48.8584, 2.2945, 10.0}))
			})
		})

		It("reports a position at zero, and ClearGeolocation removes it", func() {
			grant()
			b.SetGeolocation(biloba.Geolocation{})
			Ω(position()).Should(Equal([]any{0.0, 0.0, 0.0}))
			b.ClearGeolocation()
			Ω(position()).Should(BeAssignableToTypeOf(""))
		})

		It("rejects a position no device could report", func() {
			b.SetGeolocation(biloba.Geolocation{Latitude: 91})
			b.SetGeolocation(biloba.Geolocation{Longitude: -181})
			b.SetGeolocation(biloba.Geolocation{Accuracy: -1})
			ExpectFailures(
				"SetGeolocation: latitude, longitude, or accuracy is outside its valid range, got {Latitude:91 Longitude:0 Accuracy:0}",
				"SetGeolocation: latitude, longitude, or accuracy is outside its valid range, got {Latitude:0 Longitude:-181 Accuracy:0}",
				"SetGeolocation: latitude, longitude, or accuracy is outside its valid range, got {Latitude:0 Longitude:0 Accuracy:-1}",
			)
		})
	})

	Describe("SetPermissions", func() {
		permissions := func(entries ...any) map[biloba.Permission]biloba.PermissionState {
			out := map[biloba.Permission]biloba.PermissionState{}
			for i := 0; i < len(entries); i += 2 {
				out[entries[i].(biloba.Permission)] = entries[i+1].(biloba.PermissionState)
			}
			return out
		}

		Describe("setting and restoring", Ordered, func() {
			var ambient string
			It("sets permissions for an origin", func() {
				ambient = permissionState(b, "notifications")
				b.SetPermissions(fixtureServer, permissions(biloba.PermissionNotifications, biloba.PermissionDenied, biloba.PermissionCamera, biloba.PermissionGranted))
				Ω(permissionState(b, "notifications")).Should(Equal("denied"))
				Ω(permissionState(b, "camera")).Should(Equal("granted"))
				b.SetPermissions(fixtureServer, permissions(biloba.PermissionNotifications, biloba.PermissionGranted))
				Ω(permissionState(b, "notifications")).Should(Equal("granted"), "a later call adds to the earlier ones")
				Ω(permissionState(b, "camera")).Should(Equal("granted"))
			})

			It("restores them for the next spec", func() {
				Ω(permissionState(b, "notifications")).Should(Equal(ambient))
			})
		})

		It("applies to the origin it names, or to every origin for an empty one", func() {
			b.SetPermissions(crossOriginFixtureServer, permissions(biloba.PermissionMicrophone, biloba.PermissionGranted))
			Ω(permissionState(b, "microphone")).ShouldNot(Equal("granted"))
			b.SetPermissions("", permissions(biloba.PermissionMicrophone, biloba.PermissionDenied))
			Ω(permissionState(b, "microphone")).Should(Equal("denied"))
		})

		It("unwinds ResetPermissions and nested calls in order", func() {
			b.SetPermissions(fixtureServer, permissions(biloba.PermissionNotifications, biloba.PermissionGranted))
			DeferCleanup(func() {
				Ω(permissionState(b, "notifications")).Should(Equal("granted"), "ResetPermissions' cleanup restores the earlier grant")
			})
			b.ResetPermissions()
			Ω(permissionState(b, "notifications")).ShouldNot(Equal("granted"))
		})

		It("is shared with the tabs a page opens, but not with a new tab", func() {
			b.SetPermissions("", permissions(biloba.PermissionNotifications, biloba.PermissionDenied))
			b.Navigate(fixtureServer + "/auto-open.html")
			Eventually(b).Should(b.HaveSpawnedTab().WithURL(ContainSubstring("dom.html")))
			spawned := b.AllSpawnedTabs().Find(b.TabMatching().WithURL(ContainSubstring("dom.html")))
			Eventually("#hello").Should(spawned.Exist())
			Ω(permissionState(spawned, "notifications")).Should(Equal("denied"))

			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())
			Ω(permissionState(tab, "notifications")).ShouldNot(Equal("denied"))
			tab.SetPermissions("", permissions(biloba.PermissionNotifications, biloba.PermissionGranted))
			Ω(permissionState(tab, "notifications")).Should(Equal("granted"))
			Ω(permissionState(b, "notifications")).Should(Equal("denied"))
		})

		It("rejects a state Chrome does not know", func() {
			b.SetPermissions(fixtureServer, permissions(biloba.PermissionCamera, biloba.PermissionState("maybe")))
			ExpectFailures(`SetPermissions: permission state must be granted, denied, or prompt, got "maybe" for "camera"`)
		})
	})

	Describe("SetLocale", func() {
		locale := `Intl.DateTimeFormat().resolvedOptions().locale`

		Describe("emulating and restoring", Ordered, func() {
			var ambient string
			It("sets the page's default locale across navigations", func() {
				b.Run(locale, &ambient)
				b.SetLocale("fr-FR")
				Ω(locale).Should(b.EvaluateTo("fr-FR"))
				Ω(`(1234.5).toLocaleString()`).Should(b.EvaluateTo(MatchRegexp(`^1.234,5$`)))
				b.Navigate(fixtureServer + "/dom.html")
				Eventually("#hello").Should(b.Exist())
				Ω(locale).Should(b.EvaluateTo("fr-FR"))
			})

			It("restores it for the next spec", func() {
				Ω(locale).Should(b.EvaluateTo(ambient))
			})
		})

		It("fails on a locale Chrome does not know", func() {
			b.SetLocale("not a locale!")
			ExpectFailures(HavePrefix("SetLocale failed: "))
		})
	})

	Describe("SetTimezone", func() {
		timezone := `Intl.DateTimeFormat().resolvedOptions().timeZone`

		Describe("emulating and restoring", Ordered, func() {
			var ambient string
			It("sets the page's time zone across navigations", func() {
				b.Run(timezone, &ambient)
				b.SetTimezone("Asia/Tokyo")
				Ω(timezone).Should(b.EvaluateTo("Asia/Tokyo"))
				Ω(`new Date(0).getTimezoneOffset()`).Should(b.EvaluateTo(-540.0))
				b.Navigate(fixtureServer + "/dom.html")
				Eventually("#hello").Should(b.Exist())
				Ω(timezone).Should(b.EvaluateTo("Asia/Tokyo"))
			})

			It("restores it for the next spec", func() {
				Ω(timezone).Should(b.EvaluateTo(ambient))
			})
		})

		It("is per tab", func() {
			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())
			tab.SetTimezone("Pacific/Auckland")
			Ω(timezone).Should(tab.EvaluateTo("Pacific/Auckland"))
			Ω(timezone).ShouldNot(b.EvaluateTo("Pacific/Auckland"))
		})

		It("fails on a time zone Chrome does not know", func() {
			b.SetTimezone("Mars/Olympus_Mons")
			ExpectFailures(HavePrefix("SetTimezone failed: "))
		})
	})

	Describe("SetNetworkState", func() {
		fetchAPI := func(tab *biloba.Biloba) string {
			return tab.RunAsync(`try { await fetch("/api/ping"); return "ok" } catch (e) { return "failed" }`).(string)
		}

		Describe("going offline and back", Ordered, func() {
			It("takes the tab offline", func() {
				b.SetOffline(true)
				Ω(`navigator.onLine`).Should(b.EvaluateTo(false))
				Ω(fetchAPI(b)).Should(Equal("failed"))
				Ω(b.NetworkState()).Should(Equal(biloba.NetworkState{Offline: true}))

				b.SetOffline(false)
				Ω(`navigator.onLine`).Should(b.EvaluateTo(true))
				Ω(fetchAPI(b)).Should(Equal("ok"))
				b.SetOffline(true)
			})

			It("restores the network for the next spec", func() {
				Ω(b.NetworkState()).Should(BeZero())
				Ω(`navigator.onLine`).Should(b.EvaluateTo(true))
				Ω(fetchAPI(b)).Should(Equal("ok"))
			})
		})

		It("adds latency, and SetOffline keeps it", func() {
			b.SetNetworkState(biloba.NetworkState{Latency: 300 * time.Millisecond, ConnectionType: biloba.NetworkConnectionWiFi})
			elapsed := b.RunAsync(`const start = performance.now(); await fetch("/api/latency"); return performance.now() - start`)
			Ω(elapsed).Should(BeNumerically(">=", 300))
			b.SetOffline(true)
			Ω(b.NetworkState()).Should(Equal(biloba.NetworkState{Offline: true, Latency: 300 * time.Millisecond, ConnectionType: biloba.NetworkConnectionWiFi}))
		})

		It("is per tab", func() {
			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())
			tab.SetOffline(true)
			Ω(`navigator.onLine`).Should(tab.EvaluateTo(false))
			Ω(`navigator.onLine`).Should(b.EvaluateTo(true))
		})

		It("rejects a state Chrome would silently accept", func() {
			b.SetNetworkState(biloba.NetworkState{Latency: -time.Second})
			b.SetNetworkState(biloba.NetworkState{ConnectionType: "carrier-pigeon"})
			ExpectFailures(
				"SetNetworkState: latency and throughput must not be negative, got {Offline:false Latency:-1s DownloadThroughput:0 UploadThroughput:0 ConnectionType:}",
				"SetNetworkState: unsupported connection type, got {Offline:false Latency:0s DownloadThroughput:0 UploadThroughput:0 ConnectionType:carrier-pigeon}",
			)
		})
	})

	Describe("SetCacheEnabled", func() {
		var path string
		BeforeEach(func() {
			path = fmt.Sprintf("/cached/%d-%d", GinkgoParallelProcess(), time.Now().UnixNano())
		})
		fetchTwice := func() []any {
			return b.RunAsync(`return [await (await fetch("` + path + `")).text(), await (await fetch("` + path + `")).text()]`).([]any)
		}
		served := func(bodies []any) bool { return bodies[0] != bodies[1] }

		Describe("disabling and restoring", Ordered, func() {
			It("serves every request from the network while the cache is off", func() {
				Ω(served(fetchTwice())).Should(BeFalse(), "the cache is on by default")
				b.SetCacheEnabled(false)
				Ω(served(fetchTwice())).Should(BeTrue())
			})

			It("turns the cache back on for the next spec", func() {
				Ω(served(fetchTwice())).Should(BeFalse())
			})
		})

		It("leaves the cache off while network interception is on", func() {
			b.StubRequest(ContainSubstring("/never-requested"), biloba.StubResponse{Body: "stubbed"})
			b.SetCacheEnabled(true)
			Ω(served(fetchTwice())).Should(BeTrue())
		})
	})

	It("clears what a spec leaked on the next Prepare", func() {
		timezone := `Intl.DateTimeFormat().resolvedOptions().timeZone`
		var ambientTimezone string
		b.Run(timezone, &ambientTimezone)
		ambientPermission := permissionState(b, "notifications")
		leaked := "Asia/Tokyo"
		if ambientTimezone == leaked {
			leaked = "Europe/Paris"
		}
		Ω(b.LeakEmulationForTest(leaked, fixtureServer)).Should(Succeed())
		Ω(`navigator.onLine`).Should(b.EvaluateTo(false))
		Ω(permissionState(b, "notifications")).Should(Equal("granted"))

		b.Prepare()
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
		Ω(timezone).Should(b.EvaluateTo(ambientTimezone))
		Ω(`navigator.onLine`).Should(b.EvaluateTo(true))
		Ω(permissionState(b, "notifications")).Should(Equal(ambientPermission))
		Ω(b.NetworkState()).Should(BeZero())
	})

	It("is a one-shot mutation, so it rejects the poll knobs", func() {
		b.Immediate().SetLocale("fr-FR")
		b.WithTimeout(time.Second).SetPermissions("", nil)
		b.WithPolling(time.Second).SetOffline(false)
		b.Immediate().NetworkState()
		ExpectFailures(
			ContainSubstring("SetLocale does not support Immediate"),
			ContainSubstring("SetPermissions does not support WithTimeout"),
			ContainSubstring("SetOffline does not support WithPolling"),
			ContainSubstring("NetworkState does not support Immediate"),
		)
	})
})
