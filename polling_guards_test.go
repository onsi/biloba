package biloba_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs exercise the four-bucket config enforcement: snapshot reads and one-shot mutations
// reject every poll-config knob, while the waiting commands (Navigate, screenshots) accept
// WithTimeout/WithContext (and honor them) but reject WithPolling/Immediate.  Each guard fails the
// spec via gt - which the test harness captures - so we assert with ExpectFailures.
var _ = Describe("Bucket-guard config enforcement", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("snapshot reads (Cat 3) reject all config", func() {
		It("rejects every knob on a 3a primitive (HasElement), naming the method and knob", func() {
			b.WithTimeout(time.Second).HasElement("#hello")
			ExpectFailures(ContainSubstring("HasElement does not support WithTimeout"))

			b.WithPolling(time.Second).HasElement("#hello")
			ExpectFailures(ContainSubstring("HasElement does not support WithPolling"))

			b.WithContext(context.Background()).HasElement("#hello")
			ExpectFailures(ContainSubstring("HasElement does not support WithContext"))

			b.Immediate().HasElement("#hello")
			ExpectFailures(ContainSubstring("HasElement does not support Immediate"))
		})

		It("rejects config on a 3b snapshot getter (CurrentPropertyForEach)", func() {
			b.Immediate().CurrentPropertyForEach("#hello", "tagName")
			ExpectFailures(ContainSubstring("CurrentPropertyForEach does not support Immediate"))
		})

		It("rejects config on a 3a storage snapshot (GetAll)", func() {
			b.WithTimeout(time.Second).LocalStorage().GetAll()
			ExpectFailures(ContainSubstring("localStorage.GetAll does not support WithTimeout"))
		})
	})

	Describe("snapshot actions (Cat 4) reject all config", func() {
		It("rejects config on a *Immediately action (ClickEachImmediately)", func() {
			b.WithPolling(time.Second).ClickEachImmediately("#non-existing")
			ExpectFailures(ContainSubstring("ClickEachImmediately does not support WithPolling"))
		})
	})

	Describe("one-shot mutations (Cat 5b) reject all config", func() {
		It("rejects config on a pure mutation (SetWindowSize)", func() {
			b.WithTimeout(time.Second).SetWindowSize(640, 480)
			ExpectFailures(ContainSubstring("SetWindowSize does not support WithTimeout"))
		})

		It("rejects config on a storage mutation (Set)", func() {
			b.Immediate().LocalStorage().Set("k", "v")
			ExpectFailures(ContainSubstring("localStorage.Set does not support Immediate"))
		})
	})

	Describe("Run/RunAsync sit outside the polling model and reject all config", func() {
		It("rejects config on Run", func() {
			b.Immediate().Run("1+1")
			ExpectFailures(ContainSubstring("Run does not support Immediate"))
		})

		It("rejects config on RunAsync", func() {
			b.WithPolling(time.Second).RunAsync("return 1+1")
			ExpectFailures(ContainSubstring("RunAsync does not support WithPolling"))
		})
	})

	Describe("waiting commands (Cat 5a) honor WithTimeout/WithContext and reject WithPolling/Immediate", func() {
		It("rejects WithPolling and Immediate on Navigate but allows WithTimeout/WithContext", func() {
			// WithPolling/Immediate are hard errors.  (After the captured guard failure the navigation
			// still proceeds against a healthy fixture, which succeeds and adds no further failure.)
			b.WithPolling(time.Second).Navigate(fixtureServer + "/nav-a.html")
			ExpectFailures(ContainSubstring("Navigate does not support WithPolling"))

			b.Immediate().Navigate(fixtureServer + "/nav-a.html")
			ExpectFailures(ContainSubstring("Navigate does not support Immediate"))

			// WithTimeout/WithContext are allowed and must NOT raise a guard failure.
			b.WithTimeout(10 * time.Second).WithContext(context.Background()).Navigate(fixtureServer + "/nav-a.html")
			Eventually("body a").Should(b.Exist())
		})

		It("rejects WithPolling/Immediate on a screenshot command", func() {
			b.WithPolling(time.Second).CaptureScreenshot()
			ExpectFailures(ContainSubstring("CaptureScreenshot does not support WithPolling"))

			b.Immediate().CaptureScreenshot()
			ExpectFailures(ContainSubstring("CaptureScreenshot does not support Immediate"))
		})

		It("actually honors a short WithTimeout, bounding a wedged navigation", func() {
			// A server that accepts the connection but never responds wedges chromedp.Navigate on the load
			// event.  WithTimeout must override the generous navigationTimeout default so the call fails
			// promptly with the overridden deadline in the message.
			block := make(chan struct{})
			hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-block:
				case <-r.Context().Done():
				}
			}))
			DeferCleanup(func() {
				close(block)
				hang.Close()
			})

			start := time.Now()
			b.WithTimeout(300 * time.Millisecond).Navigate(hang.URL)
			Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
			ExpectFailures(ContainSubstring("timed out after 300ms navigating to " + hang.URL))
		})

		It("actually honors WithContext, aborting a navigation when the context is cancelled", func() {
			block := make(chan struct{})
			hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-block:
				case <-r.Context().Done():
				}
			}))
			DeferCleanup(func() {
				close(block)
				hang.Close()
			})

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			start := time.Now()
			b.WithContext(ctx).Navigate(hang.URL)
			Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
			ExpectFailures(ContainSubstring("failed to navigate to " + hang.URL))
		})
	})
})

var _ = Describe("Bucket-guard regression: clean calls are unaffected", func() {
	// A guard with no knob set is a no-op, so the ordinary (config-free) call paths keep working.
	It("leaves a config-free snapshot, mutation, and waiting command working", func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
		Expect(b.HasElement("#hello")).To(BeTrue())
		b.LocalStorage().Set("k", "v")
		Expect(b.LocalStorage().GetAll()).To(HaveKeyWithValue("k", "v"))
		Expect(b.CaptureScreenshot()).NotTo(BeEmpty())
	})
})
