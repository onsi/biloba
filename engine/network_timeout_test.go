package engine_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("response transform deadlines", func() {
	It("continues a redirect when Chrome refuses to transfer its response body", func(ctx SpecContext) {
		responseServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/redirect-response":
				http.Redirect(response, request, "/redirect-final", http.StatusFound)
			case "/redirect-final":
				_, _ = io.WriteString(response, "redirect final")
			default:
				_, _ = io.WriteString(response, "<!doctype html>")
			}
		}))
		DeferCleanup(responseServer.Close)

		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, responseServer.URL)).To(Succeed())

		invoked := make(chan struct{}, 1)
		handler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/redirect-response"},
			Transform: func(context.Context, engine.InterceptedResponse) (engine.ResponseOverride, error) {
				invoked <- struct{}{}
				return engine.ResponseOverride{}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())

		value, err := session.EvaluateAsync(ctx, `fetch("/redirect-response").then(response => response.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("redirect final"))
		Consistently(invoked, 50*time.Millisecond).ShouldNot(Receive())
		stats, err := session.NetworkHandlerStats(handler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.LastError).To(ContainSubstring("response body"))
	})

	It("extends the body read to a transform timeout longer than the minimum", func(ctx SpecContext) {
		// The production minimum is five seconds; shorten it so the spec need not wait that long.
		DeferCleanup(engine.SetMinResponseBodyTimeoutForTest(100 * time.Millisecond))
		responseServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/long-body" && request.URL.Path != "/default-body" {
				_, _ = io.WriteString(response, "<!doctype html>")
				return
			}
			response.WriteHeader(http.StatusOK)
			response.(http.Flusher).Flush()
			time.Sleep(400 * time.Millisecond)
			_, _ = io.WriteString(response, "long body")
		}))
		DeferCleanup(responseServer.Close)

		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, responseServer.URL)).To(Succeed())

		transform := func(_ context.Context, intercepted engine.InterceptedResponse) (engine.ResponseOverride, error) {
			body := append([]byte("transformed:"), intercepted.Body...)
			return engine.ResponseOverride{Body: &body}, nil
		}
		_, err = session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:              engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/long-body"},
			TransformTimeout: time.Second,
			Transform:        transform,
		})
		Expect(err).NotTo(HaveOccurred())
		// Without a longer TransformTimeout the minimum applies, and the same slow body misses it.
		defaultHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:       engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/default-body"},
			Transform: transform,
		})
		Expect(err).NotTo(HaveOccurred())

		value, err := session.EvaluateAsync(ctx, `fetch("/long-body").then(response => response.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("transformed:long body"))

		_, err = session.EvaluateAsync(ctx, `fetch("/default-body").then(response => response.text())`)
		Expect(err).To(HaveOccurred())
		stats, err := session.NetworkHandlerStats(defaultHandler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.LastError).To(ContainSubstring("deadline exceeded"))
	})

	It("starts the transform deadline after the intercepted response body is available", func(ctx SpecContext) {
		fastStarted := make(chan struct{})
		fastRelease := make(chan struct{})
		timeoutStarted := make(chan struct{})
		timeoutRelease := make(chan struct{})
		var releaseFast, releaseTimeout sync.Once
		responseServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/slow-fast":
				response.WriteHeader(http.StatusOK)
				response.(http.Flusher).Flush()
				close(fastStarted)
				<-fastRelease
				_, _ = io.WriteString(response, "slow original")
			case "/slow-timeout":
				response.WriteHeader(http.StatusOK)
				response.(http.Flusher).Flush()
				close(timeoutStarted)
				<-timeoutRelease
				_, _ = io.WriteString(response, "timeout original")
			default:
				_, _ = io.WriteString(response, "<!doctype html>")
			}
		}))
		DeferCleanup(responseServer.Close)

		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		// Release server handlers before closing the session if an assertion aborts the test early.
		DeferCleanup(func() {
			releaseFast.Do(func() { close(fastRelease) })
			releaseTimeout.Do(func() { close(timeoutRelease) })
		})
		Expect(session.Navigate(ctx, responseServer.URL)).To(Succeed())

		fastInvoked := make(chan struct{}, 1)
		_, err = session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:              engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/slow-fast"},
			TransformTimeout: 25 * time.Millisecond,
			Transform: func(_ context.Context, intercepted engine.InterceptedResponse) (engine.ResponseOverride, error) {
				fastInvoked <- struct{}{}
				body := append([]byte("transformed:"), intercepted.Body...)
				return engine.ResponseOverride{Body: &body}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		type evaluation struct {
			value any
			err   error
		}
		fastResult := make(chan evaluation, 1)
		go func() {
			value, evaluateErr := session.EvaluateAsync(ctx, `fetch("/slow-fast").then(response => response.text())`)
			fastResult <- evaluation{value: value, err: evaluateErr}
		}()
		Eventually(fastStarted, 2*time.Second).Should(BeClosed())
		Consistently(fastInvoked, 75*time.Millisecond).ShouldNot(Receive())
		releaseFast.Do(func() { close(fastRelease) })
		Eventually(fastInvoked, 2*time.Second).Should(Receive())
		var fast evaluation
		Eventually(fastResult, 2*time.Second).Should(Receive(&fast))
		Expect(fast.err).NotTo(HaveOccurred())
		Expect(fast.value).To(Equal("transformed:slow original"))

		timeoutInvoked := make(chan struct{}, 1)
		timeoutHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:              engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/slow-timeout"},
			TransformTimeout: 25 * time.Millisecond,
			Transform: func(transformCtx context.Context, _ engine.InterceptedResponse) (engine.ResponseOverride, error) {
				timeoutInvoked <- struct{}{}
				<-transformCtx.Done()
				return engine.ResponseOverride{}, transformCtx.Err()
			},
		})
		Expect(err).NotTo(HaveOccurred())
		timeoutResult := make(chan evaluation, 1)
		go func() {
			value, evaluateErr := session.EvaluateAsync(ctx, `fetch("/slow-timeout").then(response => response.text())`)
			timeoutResult <- evaluation{value: value, err: evaluateErr}
		}()
		Eventually(timeoutStarted, 2*time.Second).Should(BeClosed())
		Consistently(timeoutInvoked, 75*time.Millisecond).ShouldNot(Receive())
		releaseTimeout.Do(func() { close(timeoutRelease) })
		Eventually(timeoutInvoked, 2*time.Second).Should(Receive())
		var timedOut evaluation
		Eventually(timeoutResult, 2*time.Second).Should(Receive(&timedOut))
		Expect(timedOut.err).NotTo(HaveOccurred())
		Expect(timedOut.value).To(Equal("timeout original"))
		stats, err := session.NetworkHandlerStats(timeoutHandler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.LastError).To(ContainSubstring("deadline exceeded"))
	})
})
