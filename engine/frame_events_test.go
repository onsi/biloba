package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onsi/biloba/engine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("frame document event lifetimes", func() {
	for _, scenario := range []struct {
		name, host   string
		sharesTarget bool
	}{
		{name: "in the parent renderer", host: "127.0.0.1", sharesTarget: true},
		{name: "in a separate renderer", host: "localhost"},
	} {
		It("keeps replacement events out of the old document "+scenario.name, func(ctx SpecContext) {
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/release" {
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<title>event child</title><p>ready</p>"))
			}))
			DeferCleanup(child.Close)
			isolated, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath(), Arguments: []string{"--site-per-process"}})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(isolated.Close)
			root, err := isolated.OpenSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(root.Close)
			Expect(root.Navigate(ctx, server.URL)).To(Succeed())
			childURL := strings.Replace(child.URL, "127.0.0.1", scenario.host, 1)
			_, err = root.Evaluate(ctx, `document.body.innerHTML='<iframe id="child"></iframe>'; document.querySelector('#child').src=`+strconv.Quote(childURL))
			Expect(err).NotTo(HaveOccurred())
			old, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "event child"}}, engine.PollPolicy{Timeout: 3 * time.Second})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(old.Close)
			DeferCleanup(unblock)
			Expect(old.TargetID() == root.TargetID()).To(Equal(scenario.sharesTarget))
			// An event wait must not wait for the session's pending evaluation to finish.
			pending := make(chan error, 1)
			go func() {
				_, evaluateErr := old.EvaluateAsync(ctx, `fetch('/original').then(r=>r.text()).then(()=>{console.log('original'); return fetch('/release').then(r=>r.text())})`)
				pending <- evaluateErr
			}()
			Eventually(old.ConsoleMessages).Should(HaveLen(1))
			query := engine.RequestQuery{URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/original"}}
			_, err = old.WaitForRequest(ctx, query, engine.PollPolicy{Timeout: time.Second})
			Expect(err).NotTo(HaveOccurred())
			unblock()
			Eventually(pending).Should(Receive(BeNil()))
			history := old.Requests()
			responses := old.Responses(engine.ResponseQuery{})
			_, err = root.EvaluateAsync(ctx, `new Promise(resolve=>{const f=document.querySelector('#child'); f.onload=()=>resolve(true); f.src=`+strconv.Quote(childURL+"/replacement")+`})`)
			Expect(err).NotTo(HaveOccurred())
			fresh, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/replacement"}}, engine.PollPolicy{Timeout: 3 * time.Second})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(fresh.Close)
			_, err = fresh.EvaluateAsync(ctx, `fetch('/new-document').then(r=>r.text()).then(()=>console.log('replacement'))`)
			Expect(err).NotTo(HaveOccurred())
			Eventually(fresh.ConsoleMessages).Should(HaveLen(1))
			Expect(old.Requests()).To(Equal(history))
			Expect(old.Responses(engine.ResponseQuery{})).To(Equal(responses))
			Expect(old.ConsoleMessages()).To(HaveLen(1))
			Expect(old.ConsoleMessages()[0].Text).To(Equal("original"))
			replacementQuery := engine.RequestQuery{URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/new-document"}}
			_, err = fresh.WaitForRequest(ctx, replacementQuery, engine.PollPolicy{Timeout: time.Second})
			Expect(err).NotTo(HaveOccurred())
			var stale *engine.Error
			_, err = old.WaitForRequest(ctx, replacementQuery, engine.PollPolicy{Timeout: time.Second})
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Code).To(Equal(engine.CodeFrameDetached))
			// Retained history must not make a stale wait succeed either.
			_, err = old.WaitForRequest(ctx, query, engine.PollPolicy{Timeout: time.Second})
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Code).To(Equal(engine.CodeFrameDetached))
			_, err = old.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: time.Second})
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Code).To(Equal(engine.CodeFrameDetached))
			Expect(fresh.Close()).To(Succeed())
			_, err = fresh.WaitForRequest(context.Background(), replacementQuery, engine.PollPolicy{Timeout: time.Second})
			Expect(errors.As(err, &stale)).To(BeTrue())
			Expect(stale.Code).To(Equal(engine.CodeSessionClosed))
		})
	}
})
