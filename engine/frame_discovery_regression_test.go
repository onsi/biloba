package engine_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame discovery regressions", func() {
	It("discovers a same-server sandboxed frame whose opaque origin blocks parent DOM access", func(ctx SpecContext) {
		sandboxServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/sandbox-child" {
				_, _ = response.Write([]byte(`<!doctype html><div data-testid="destination">arrived</div><script>const parent = {document: {body: "decoy"}}</script>`))
				return
			}
			_, _ = response.Write([]byte(`<!doctype html><title>sandbox parent</title>`))
		}))
		DeferCleanup(sandboxServer.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, sandboxServer.URL)).To(Succeed())

		loaded, err := root.EvaluateAsync(ctx, `new Promise((resolve, reject) => {
			const frame = document.createElement('iframe');
			frame.id = 'sandboxed';
			frame.sandbox = 'allow-scripts';
			frame.onload = () => resolve(true);
			frame.onerror = () => reject(new Error('sandboxed frame failed to load'));
			frame.src = `+strconv.Quote(sandboxServer.URL+"/sandbox-child")+`;
			document.body.append(frame);
		})`)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(BeTrue())

		blocked, err := root.Evaluate(ctx, `(() => {
			try {
				void document.querySelector('#sandboxed').contentWindow.document.body;
				return false;
			} catch (error) {
				return error.name === 'SecurityError';
			}
		})()`)
		Expect(err).NotTo(HaveOccurred())
		Expect(blocked).To(BeTrue(), "sandbox without allow-same-origin must prevent parent DOM access")

		// A sandbox that preserves the document's origin must still use ordinary DOM piercing.
		_, err = root.EvaluateAsync(ctx, `new Promise(resolve => {
			const frame = document.createElement('iframe');
			frame.id = 'same-origin';
			frame.sandbox = 'allow-same-origin';
			frame.onload = () => resolve(true);
			frame.src = `+strconv.Quote(sandboxServer.URL+"/sandbox-child")+`;
			document.body.append(frame);
		})`)
		Expect(err).NotTo(HaveOccurred())
		accessible, err := root.Evaluate(ctx, `document.querySelector('#same-origin').contentDocument.body !== null`)
		Expect(err).NotTo(HaveOccurred())
		Expect(accessible).To(BeTrue())

		frames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, frame := range frames {
				_ = frame.Close()
			}
		})
		Expect(frames).To(HaveLen(1))
		Expect(frames[0].URL()).To(Equal(sandboxServer.URL + "/sandbox-child"))
		shadowedParent, err := frames[0].Evaluate(ctx, `parent.document.body`)
		Expect(err).NotTo(HaveOccurred())
		Expect(shadowedParent).To(Equal("decoy"), "the fixture must shadow the page world's parent binding")
		text, err := frames[0].Text(ctx, engine.TestID("destination"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("arrived"))
	})

	DescribeTable("does not let an unrelated busy OOPIF block waiting for a healthy frame", func(ctx SpecContext, healthyOOPIF, busySandbox bool) {
		busy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path != "/busy" {
				_, _ = response.Write([]byte(`<!doctype html><title>parent</title>`))
				return
			}
			_, _ = response.Write([]byte(`<title>busy child</title><script>parent.postMessage('busy-child-starting', '*'); setTimeout(() => { while (true) {} }, 0)</script>`))
		}))
		DeferCleanup(busy.Close)
		healthy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><title>healthy child</title><div id="healthy">ready</div>`))
		}))
		DeferCleanup(healthy.Close)

		isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Arguments:      []string{"--site-per-process", "--host-resolver-rules=MAP healthy.test 127.0.0.1", "--no-proxy-server"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolatedBrowser.Close)
		root, err := isolatedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		rootURL := server.URL
		if busySandbox {
			rootURL = busy.URL
		}
		Expect(root.Navigate(ctx, rootURL)).To(Succeed())

		busyURL := strings.Replace(busy.URL, "127.0.0.1", "localhost", 1) + "/busy"
		if busySandbox {
			busyURL = busy.URL + "/busy"
		}
		healthyURL := healthy.URL
		if healthyOOPIF {
			healthyURL = strings.Replace(healthyURL, "127.0.0.1", "healthy.test", 1)
		}
		_, err = root.Evaluate(ctx, `window.busyChildStarted = false;
			window.healthyChildLoaded = false;
			addEventListener('message', event => { if (event.data === 'busy-child-starting') window.busyChildStarted = true });
			const busyFrame = document.createElement('iframe');
			if (`+strconv.FormatBool(busySandbox)+`) busyFrame.sandbox = 'allow-scripts';
			busyFrame.src = `+strconv.Quote(busyURL)+`;
			document.body.append(busyFrame);
			const healthyFrame = document.createElement('iframe');
			healthyFrame.onload = () => { window.healthyChildLoaded = true };
			healthyFrame.src = `+strconv.Quote(healthyURL)+`;
			document.body.append(healthyFrame)`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() any {
			ready, evaluateErr := root.Evaluate(ctx, `window.busyChildStarted && window.healthyChildLoaded`)
			Expect(evaluateErr).NotTo(HaveOccurred())
			return ready
		}).WithTimeout(2 * time.Second).Should(BeTrue())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{
			URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: healthyURL + "/"},
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		if healthyOOPIF {
			Expect(frame.TargetID()).NotTo(Equal(root.TargetID()))
		} else {
			Expect(frame.TargetID()).To(Equal(root.TargetID()))
		}
		text, err := frame.Text(ctx, engine.CSS("#healthy"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("ready"))
	},
		Entry("in the parent renderer", false, false),
		Entry("in another renderer", true, false),
		Entry("beside a sandboxed renderer with the same reported origin", true, true),
	)
})
