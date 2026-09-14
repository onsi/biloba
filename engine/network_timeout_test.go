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
