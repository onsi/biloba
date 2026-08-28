package engine_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("eventful engine state", func() {
	It("handles dialogs newest-first, records history, removes handlers, and resets on prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		oldText, newText := "old", "Ada"
		_, err = session.RegisterDialogHandler(ctx, engine.DialogHandlerOptions{Type: engine.DialogPrompt, Accept: true, PromptText: &oldText})
		Expect(err).NotTo(HaveOccurred())
		newest, err := session.RegisterDialogHandler(ctx, engine.DialogHandlerOptions{
			Type: engine.DialogPrompt, Message: &engine.Expectation{Kind: engine.ExpectContains, Expected: "name"}, Accept: true, PromptText: &newText,
		})
		Expect(err).NotTo(HaveOccurred())

		value, err := session.Evaluate(ctx, `window.prompt("your name", "default")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("Ada"))
		Eventually(session.Dialogs).Should(ConsistOf(SatisfyAll(
			HaveField("Type", Equal(engine.DialogPrompt)),
			HaveField("Message", Equal("your name")),
			HaveField("DefaultPrompt", Equal("default")),
			HaveField("Accepted", BeTrue()),
			HaveField("PromptText", Equal("Ada")),
			HaveField("AutoHandled", BeFalse()),
		)))

		Expect(session.RemoveDialogHandler(ctx, newest.ID)).To(Succeed())
		value, err = session.Evaluate(ctx, `window.prompt("your name", "default")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("old"))
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Dialogs()).To(BeEmpty())
	})

	It("applies dialog defaults, preserves empty prompt text, and rejects cross-session handles", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Close)
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())

		value, err := first.Evaluate(ctx, `window.confirm("unhandled confirm")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeFalse())
		value, err = first.Evaluate(ctx, `window.prompt("unhandled prompt", "default")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeNil())
		empty := ""
		firstHandler, err := first.RegisterDialogHandler(ctx, engine.DialogHandlerOptions{Type: engine.DialogPrompt, Accept: true, PromptText: &empty})
		Expect(err).NotTo(HaveOccurred())
		secondHandler, err := second.RegisterDialogHandler(ctx, engine.DialogHandlerOptions{Type: engine.DialogPrompt, Accept: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(firstHandler.ID).NotTo(Equal(secondHandler.ID))
		Expect(second.RemoveDialogHandler(ctx, firstHandler.ID)).To(MatchError(ContainSubstring("not found")))

		value, err = first.Evaluate(ctx, `window.prompt("empty prompt", "default")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(""))
		Expect(second.Dialogs()).To(BeEmpty())
		Expect(first.Dialogs()).To(HaveLen(3))
		recent, found := first.MostRecentDialog(engine.DialogQuery{Message: &engine.Expectation{Kind: engine.ExpectContains, Expected: "empty"}})
		Expect(found).To(BeTrue())
		Expect(recent.PromptText).To(Equal(""))
	})

	It("emits structured warnings for auto-handled dialogs and resets them per session lifecycle", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())

		value, err := first.Evaluate(ctx, `window.confirm("unhandled warning")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeFalse())
		Eventually(first.Warnings).Should(ConsistOf(SatisfyAll(
			HaveField("Code", Equal(engine.WarningDialogAutoHandled)),
			HaveField("Message", ContainSubstring("unhandled warning")),
			HaveField("Dialog", SatisfyAll(
				HaveField("Type", Equal(engine.DialogConfirm)),
				HaveField("Message", Equal("unhandled warning")),
				HaveField("AutoHandled", BeTrue()),
			)),
		)))
		Expect(first.Dialogs()).To(ConsistOf(HaveField("AutoHandled", BeTrue())))
		Expect(second.Warnings()).To(BeEmpty())

		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(first.Warnings()).To(BeEmpty())
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		_, err = first.Evaluate(ctx, `window.alert("close warning")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(first.Warnings).Should(HaveLen(1))
		Expect(first.Close()).To(Succeed())
		Expect(first.Warnings()).To(BeEmpty())
	})

	It("tracks completed downloads with bounded content and clears artifacts on prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/download"; a.download=""; document.body.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())

		download, err := session.WaitForDownload(ctx, engine.DownloadQuery{
			State:    engine.DownloadComplete,
			Filename: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "report.txt"},
		}, engine.PollPolicy{Timeout: 2 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(download.URL).To(HaveSuffix("/download"))
		Expect(download.ReceivedBytes).To(BeNumerically(">", 0))
		content, err := session.DownloadContent(ctx, download.ID, 1024)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("downloaded report"))
		Expect(session.Downloads(engine.DownloadQuery{Content: &engine.Expectation{Kind: engine.ExpectContains, Expected: "downloaded report"}})).To(HaveLen(1))
		_, err = session.DownloadContent(ctx, download.ID, 4)
		Expect(err).To(MatchError(ContainSubstring("exceeds")))
		_, err = session.DownloadContent(ctx, "../"+download.ID, 1024)
		Expect(err).To(MatchError(ContainSubstring("basename")))

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Downloads(engine.DownloadQuery{})).To(BeEmpty())
	})

	It("cancels active downloads, protects close, and isolates download state", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		other, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/slow-download"; a.download=""; document.documentElement.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		download, err := session.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadActive}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(session.Close()).To(MatchError(ContainSubstring("active downloads")))
		Expect(other.Downloads(engine.DownloadQuery{})).To(BeEmpty())

		Expect(session.CancelDownload(ctx, download.ID)).To(Succeed())
		cancelled, err := session.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadCancelled}, engine.PollPolicy{Timeout: 2 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(cancelled.ID).To(Equal(download.ID))
		_, err = session.DownloadContent(ctx, download.ID, 1024)
		Expect(err).To(MatchError(ContainSubstring("not complete")))
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Downloads(engine.DownloadQuery{})).To(BeEmpty())
		Expect(session.Close()).To(Succeed())
	})

	It("cancels active transfers and fences late download events during prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		before := cancelledDownloadHTTP.Load()
		_, err = session.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/slow-download?prepare"; a.download=""; document.documentElement.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadActive}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Downloads(engine.DownloadQuery{})).To(BeEmpty())
		Eventually(cancelledDownloadHTTP.Load).Should(BeNumerically(">", before))
		Consistently(session.Downloads, 200*time.Millisecond).WithArguments(engine.DownloadQuery{}).Should(BeEmpty())
	})

	It("attributes sibling-tab downloads only to the initiating tab", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		child, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(child.Close)
		Expect(child.Navigate(ctx, server.URL)).To(Succeed())
		_, err = child.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/download"; a.download=""; document.body.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		_, err = child.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadComplete}, engine.PollPolicy{Timeout: 2 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Downloads(engine.DownloadQuery{})).To(BeEmpty())
	})

	It("initializes eventful state and shared download ownership for discovered popups", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `void window.open("/destination", "_blank")`)
		Expect(err).NotTo(HaveOccurred())
		popup, err := root.WaitForTab(ctx, engine.TabQuery{
			SpawnedOnly: true,
			URL:         &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"},
		}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())

		_, err = popup.Evaluate(ctx, `console.log("popup event")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(popup.ConsoleMessages, 300*time.Millisecond).Should(ContainElement(HaveField("Text", Equal("popup event"))))
		value, err := popup.Evaluate(ctx, `window.confirm("popup dialog")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeFalse())
		Eventually(popup.Warnings, 300*time.Millisecond).Should(ContainElement(HaveField("Code", Equal(engine.WarningDialogAutoHandled))))
		_, err = popup.EvaluateAsync(ctx, `fetch("/popup-request").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(popup.Requests, 300*time.Millisecond).Should(ContainElement(HaveField("URL", HaveSuffix("/popup-request"))))

		_, err = popup.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/download?popup"; a.download=""; document.body.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		download, err := popup.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadComplete}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		content, err := popup.DownloadContent(ctx, download.ID, 1024)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("downloaded report"))
		Expect(root.Downloads(engine.DownloadQuery{})).To(BeEmpty())

		Expect(popup.Close()).To(Succeed())
		_, err = root.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/download?root-after-popup"; a.download=""; document.body.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		rootDownload, err := root.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadComplete}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		rootContent, err := root.DownloadContent(ctx, rootDownload.ID, 1024)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(rootContent)).To(Equal("downloaded report"))
	})

	It("throttles DOM handlers while Chrome's download completion window is saturated", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `for(let i=0;i<10;i++){const a=document.createElement("a");a.href="/download?i="+i;a.download="";document.documentElement.append(a);a.click()}`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() int {
			return len(session.Downloads(engine.DownloadQuery{State: engine.DownloadComplete}))
		}, 2*time.Second).Should(Equal(10))

		done := make(chan error, 1)
		go func() {
			_, handlerErr := session.Exists(ctx, engine.TestID("name"))
			done <- handlerErr
		}()
		Consistently(done, 50*time.Millisecond).ShouldNot(Receive())
		Eventually(done, 2*time.Second).Should(Receive(Succeed()))
	})

	It("fulfills, aborts, and modifies requests and responses with ordered handler stats", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		stub, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:     engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/stubbed"},
			Fulfill: &engine.ResponseOverride{Status: intPtr(202), Headers: map[string]string{"Content-Type": "text/plain"}, Body: bytesPtr([]byte("stubbed body"))},
		})
		Expect(err).NotTo(HaveOccurred())
		abort, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/aborted"}, Abort: true})
		Expect(err).NotTo(HaveOccurred())
		requestHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:     engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/echo"},
			Request: &engine.RequestOverride{Method: stringPtr("PUT"), Headers: map[string]string{"X-Changed": "yes"}, Body: bytesPtr([]byte("changed"))},
		})
		Expect(err).NotTo(HaveOccurred())
		responseHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:      engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/modifiable"},
			Response: &engine.ResponseOverride{Status: intPtr(206), Headers: map[string]string{"X-Modified": "yes"}, Body: bytesPtr([]byte("modified body"))},
		})
		Expect(err).NotTo(HaveOccurred())
		transformed := make(chan engine.InterceptedResponse, 1)
		transformHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/transformed"},
			Transform: func(_ context.Context, response engine.InterceptedResponse) (engine.ResponseOverride, error) {
				transformed <- response
				body := append([]byte("transformed:"), response.Body...)
				return engine.ResponseOverride{Status: intPtr(207), Body: &body}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		timeoutHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:              engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/transform-timeout"},
			TransformTimeout: 25 * time.Millisecond,
			Transform: func(transformCtx context.Context, _ engine.InterceptedResponse) (engine.ResponseOverride, error) {
				<-transformCtx.Done()
				return engine.ResponseOverride{}, transformCtx.Err()
			},
		})
		Expect(err).NotTo(HaveOccurred())

		result, err := session.EvaluateAsync(ctx, `fetch("/stubbed").then(async r => [r.status, await r.text()])`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal([]any{float64(202), "stubbed body"}))
		result, err = session.EvaluateAsync(ctx, `fetch("/aborted").then(() => false, () => true)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
		result, err = session.EvaluateAsync(ctx, `fetch("/echo", {method:"POST", body:"original"}).then(async r => [r.headers.get("x-echo-method"), r.headers.get("x-echo-header"), await r.text()])`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal([]any{"PUT", "yes", "PUT:changed"}))
		result, err = session.EvaluateAsync(ctx, `fetch("/modifiable").then(async r => [r.status, r.headers.get("x-modified"), await r.text()])`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal([]any{float64(206), "yes", "modified body"}))
		result, err = session.EvaluateAsync(ctx, `fetch("/transformed").then(async r => [r.status, await r.text()])`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(2))
		Expect(result.([]any)[0]).To(Equal(float64(207)))
		Expect(result.([]any)[1]).To(ContainSubstring("transformed:<!doctype html>"))
		var original engine.InterceptedResponse
		Expect(transformed).To(Receive(&original))
		Expect(original.Status).To(Equal(200))
		Expect(string(original.Body)).To(ContainSubstring("<!doctype html>"))
		result, err = session.EvaluateAsync(ctx, `fetch("/transform-timeout").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(ContainSubstring("<!doctype html>"))
		timeoutStats, err := session.NetworkHandlerStats(timeoutHandler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(timeoutStats.LastError).To(ContainSubstring("deadline exceeded"))
		for _, handler := range []engine.NetworkHandler{stub, abort, requestHandler, responseHandler, transformHandler} {
			stats, statsErr := session.NetworkHandlerStats(handler.ID)
			Expect(statsErr).NotTo(HaveOccurred())
			Expect(stats.Count).To(Equal(1))
		}
		Expect(len(session.Requests())).To(BeNumerically(">=", 4))
		post := session.RequestsMatching(engine.RequestQuery{Method: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "POST"}})
		Expect(post).NotTo(BeEmpty())
		post[0].Headers["Mutated"] = "outside"
		Expect(session.Requests()[0].Headers).NotTo(HaveKey("Mutated"))
		Expect(session.Responses(engine.ResponseQuery{})).NotTo(BeEmpty())

		first, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/ordered"}, Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("first"))},
		})
		Expect(err).NotTo(HaveOccurred())
		second, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/ordered"}, Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("second"))},
		})
		Expect(err).NotTo(HaveOccurred())
		result, err = session.EvaluateAsync(ctx, `fetch("/ordered").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("first"))
		secondStats, err := session.NetworkHandlerStats(second.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondStats.Shadowed).To(Equal(1))
		Expect(session.RemoveNetworkHandler(ctx, first.ID)).To(Succeed())
		firstStats, err := session.NetworkHandlerStats(first.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstStats.Count).To(Equal(1))
		result, err = session.EvaluateAsync(ctx, `fetch("/ordered").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("second"))
	})

	It("records first-match network shadows with client provenance and lifecycle isolation", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())

		winner, err := first.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/shadowed"}, Callsite: "catalog/winner.ts:10",
			Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("winner"))},
		})
		Expect(err).NotTo(HaveOccurred())
		shadowed, err := first.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/shadowed"}, Callsite: "catalog/shadowed.ts:20",
			Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("shadowed"))},
		})
		Expect(err).NotTo(HaveOccurred())
		for range 2 {
			value, evaluateErr := first.EvaluateAsync(ctx, `fetch("/shadowed").then(r => r.text())`)
			Expect(evaluateErr).NotTo(HaveOccurred())
			Expect(value).To(Equal("winner"))
		}

		winnerStats, err := first.NetworkHandlerStats(winner.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(winnerStats).To(SatisfyAll(
			HaveField("ID", Equal(winner.ID)),
			HaveField("Callsite", Equal("catalog/winner.ts:10")),
			HaveField("Count", Equal(2)),
		))
		shadowedStats, err := first.NetworkHandlerStats(shadowed.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(shadowedStats).To(SatisfyAll(
			HaveField("ID", Equal(shadowed.ID)),
			HaveField("Callsite", Equal("catalog/shadowed.ts:20")),
			HaveField("Shadowed", Equal(2)),
		))
		Expect(first.NetworkShadowDiagnostics()).To(HaveLen(2))
		latest := first.NetworkShadowDiagnostics()[1]
		Expect(latest).To(SatisfyAll(
			HaveField("URL", HaveSuffix("/shadowed")),
			HaveField("Stage", Equal(engine.NetworkStageRequest)),
			HaveField("Winner", Equal(engine.NetworkOwnerProvenance{
				Kind: engine.NetworkOwnerHandler, ID: winner.ID, Callsite: "catalog/winner.ts:10", Count: 2,
			})),
			HaveField("Shadowed", Equal([]engine.NetworkOwnerProvenance{{
				Kind: engine.NetworkOwnerHandler, ID: shadowed.ID, Callsite: "catalog/shadowed.ts:20", Shadowed: 2,
			}})),
		))
		Expect(second.NetworkShadowDiagnostics()).To(BeEmpty())

		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(first.NetworkShadowDiagnostics()).To(BeEmpty())
		_, err = first.NetworkHandlerStats(winner.ID)
		Expect(err).To(MatchError(ContainSubstring("not found")))

		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		winner, err = first.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/close-shadow"}, Callsite: "catalog/close-winner.ts:1",
			Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("winner"))},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = first.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/close-shadow"}, Callsite: "catalog/close-shadowed.ts:2",
			Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("shadowed"))},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = first.EvaluateAsync(ctx, `fetch("/close-shadow").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.NetworkShadowDiagnostics()).To(HaveLen(1))
		Expect(first.Close()).To(Succeed())
		Expect(first.NetworkShadowDiagnostics()).To(BeEmpty())
		_, err = first.NetworkHandlerStats(winner.ID)
		Expect(err).To(MatchError(ContainSubstring("not found")))
	})

	It("validates typed network connection state and resets it without crossing sessions", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)

		invalid := engine.NetworkState{ConnectionType: engine.NetworkConnectionType("satellite")}
		Expect(first.SetNetworkState(ctx, invalid)).To(MatchError(ContainSubstring("unsupported connection type")))
		Expect(first.CurrentNetworkState()).To(Equal(engine.NetworkState{}))

		configured := engine.NetworkState{
			Latency: 25 * time.Millisecond, DownloadThroughput: 4096, UploadThroughput: 2048,
			ConnectionType: engine.NetworkConnectionCellular4G,
		}
		Expect(first.SetNetworkState(ctx, configured)).To(Succeed())
		Expect(first.CurrentNetworkState()).To(Equal(configured))
		Expect(second.CurrentNetworkState()).To(Equal(engine.NetworkState{}))
		Expect(first.ResetNetworkState(ctx)).To(Succeed())
		Expect(first.CurrentNetworkState()).To(Equal(engine.NetworkState{}))

		configured.ConnectionType = engine.NetworkConnectionWiFi
		Expect(first.SetNetworkState(ctx, configured)).To(Succeed())
		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(first.CurrentNetworkState()).To(Equal(engine.NetworkState{}))
		Expect(first.SetNetworkState(ctx, configured)).To(Succeed())
		Expect(first.Close()).To(Succeed())
		Expect(first.CurrentNetworkState()).To(Equal(engine.NetworkState{}))
	})

	It("tracks HTTP in-flight state, restores interception/cache/network state, and isolates sessions", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Close)
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())
		_, err = first.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL:     engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/isolated"},
			Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("first only"))},
		})
		Expect(err).NotTo(HaveOccurred())
		firstResult, err := first.EvaluateAsync(ctx, `fetch("/isolated").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstResult).To(Equal("first only"))
		secondResult, err := second.EvaluateAsync(ctx, `fetch("/isolated").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondResult).NotTo(Equal("first only"))

		_, err = first.Evaluate(ctx, `void fetch("/slow")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(first.InflightRequestCount).Should(BeNumerically(">", 0))
		_, err = first.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(second.InflightRequestCount()).To(Equal(0))
		redirected, err := first.EvaluateAsync(ctx, `fetch("/redirect").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected).To(Equal("slow"))
		_, err = first.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		failed, err := first.EvaluateAsync(ctx, `fetch("http://127.0.0.1:1/unreachable").then(() => false, () => true)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(failed).To(BeTrue())
		_, err = first.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())

		wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/socket"
		opened, err := first.EvaluateAsync(ctx, fmt.Sprintf(`new Promise((resolve,reject)=>{window.testSocket=new WebSocket(%q);testSocket.onopen=()=>resolve(true);testSocket.onerror=reject})`, wsURL))
		Expect(err).NotTo(HaveOccurred())
		Expect(opened).To(BeTrue())
		_, err = first.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(first.InflightRequestCount()).To(Equal(0))
		_, err = first.Evaluate(ctx, `testSocket.close()`)
		Expect(err).NotTo(HaveOccurred())

		_, err = first.Evaluate(ctx, `void fetch("/slow?prepare")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(first.InflightRequestCount).Should(BeNumerically(">", 0))
		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(first.InflightRequestCount()).To(Equal(0))
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())

		Expect(first.SetNetworkState(ctx, engine.NetworkState{Offline: true})).To(Succeed())
		Expect(first.SetCacheEnabled(ctx, false)).To(Succeed())
		Expect(first.Prepare(ctx)).To(Succeed())
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		value, err := first.EvaluateAsync(ctx, `fetch("/destination").then(r => r.ok)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeTrue())
	})

	It("disables cache during interception and restores the explicit cache preference", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		cacheURL := fmt.Sprintf("/cacheable?session=%s", session.TargetID())
		before := cacheRequests.Load()
		_, err = session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, cacheURL))
		Expect(err).NotTo(HaveOccurred())
		_, err = session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, cacheURL))
		Expect(err).NotTo(HaveOccurred())
		Expect(cacheRequests.Load() - before).To(Equal(int64(1)))

		handler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectContains, Expected: cacheURL}, Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("intercepted"))},
		})
		Expect(err).NotTo(HaveOccurred())
		for range 2 {
			value, evaluateErr := session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, cacheURL))
			Expect(evaluateErr).NotTo(HaveOccurred())
			Expect(value).To(Equal("intercepted"))
		}
		stats, err := session.NetworkHandlerStats(handler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Count).To(Equal(2))
		Expect(session.RemoveNetworkHandler(ctx, handler.ID)).To(Succeed())
		_, err = session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, cacheURL))
		Expect(err).NotTo(HaveOccurred())
		Expect(cacheRequests.Load() - before).To(Equal(int64(1)))

		holdURL := cacheURL + "&hold=1"
		holdBefore := cacheRequests.Load()
		_, err = session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, holdURL))
		Expect(err).NotTo(HaveOccurred())
		holdID, err := session.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectContains, Expected: holdURL})
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, fmt.Sprintf(`window.holdDone=false;void fetch(%q).then(()=>holdDone=true)`, holdURL))
		Expect(err).NotTo(HaveOccurred())
		_, err = session.AwaitResponseHold(ctx, holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(session.ReleaseResponseHold(ctx, holdID)).To(Succeed())
		Eventually(func() any {
			value, evaluateErr := session.Evaluate(ctx, `holdDone`)
			Expect(evaluateErr).NotTo(HaveOccurred())
			return value
		}).Should(BeTrue())
		_, err = session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, holdURL))
		Expect(err).NotTo(HaveOccurred())
		Expect(cacheRequests.Load() - holdBefore).To(Equal(int64(2)))
		holdStats, err := session.ResponseHoldStats(holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(holdStats.Count).To(Equal(1))

		uncachedURL := cacheURL + "&uncached=1"
		Expect(session.SetCacheEnabled(ctx, false)).To(Succeed())
		handler, err = session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectContains, Expected: uncachedURL}, Fulfill: &engine.ResponseOverride{Body: bytesPtr([]byte("intercepted"))},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(session.RemoveNetworkHandler(ctx, handler.ID)).To(Succeed())
		uncachedBefore := cacheRequests.Load()
		for range 2 {
			_, evaluateErr := session.EvaluateAsync(ctx, fmt.Sprintf(`fetch(%q).then(r => r.text())`, uncachedURL))
			Expect(evaluateErr).NotTo(HaveOccurred())
		}
		Expect(cacheRequests.Load() - uncachedBefore).To(Equal(int64(2)))
	})

	It("holds full responses with limits, stable identities, sequencing, and counters", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		holdID, err := session.HoldResponseWithOptions(ctx,
			engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/held"},
			engine.ResponseHoldOptions{Limit: 1},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = session.Evaluate(ctx, `window.holdResults=[]; void fetch("/held").then(r => r.text()).then(v => holdResults.push(v))`)
		Expect(err).NotTo(HaveOccurred())
		first, err := session.AwaitResponseHold(ctx, holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.ID).NotTo(BeEmpty())
		Expect(first.URL).To(HaveSuffix("/held"))
		Expect(first.Headers).NotTo(BeEmpty())
		Expect(string(first.Body)).To(ContainSubstring("<!doctype html>"))

		_, err = session.Evaluate(ctx, `void fetch("/held").then(r => r.text()).then(v => holdResults.push(v))`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() int {
			stats, statsErr := session.ResponseHoldStats(holdID)
			Expect(statsErr).NotTo(HaveOccurred())
			return stats.PassedThrough
		}).Should(Equal(1))
		stats, err := session.ResponseHoldStats(holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats).To(Equal(engine.ResponseHoldStats{Count: 2, Held: 1, PassedThrough: 1, Holding: 1}))

		Expect(session.ReleaseHeldResponse(ctx, holdID, first.ID)).To(Succeed())
		Eventually(func() int {
			value, evaluateErr := session.Evaluate(ctx, `holdResults.length`)
			Expect(evaluateErr).NotTo(HaveOccurred())
			return int(value.(float64))
		}).Should(Equal(2))
		Expect(session.ReleaseNextResponseHold(ctx, holdID)).To(MatchError(ContainSubstring("not holding")))
		Expect(session.ReleaseResponseHold(ctx, holdID)).To(Succeed())
		Expect(session.ReleaseResponseHold(ctx, holdID)).To(Succeed())
		result, err := session.EvaluateAsync(ctx, `fetch("/held").then(r => r.ok)`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
		stats, err = session.ResponseHoldStats(holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Count).To(Equal(2))
		Expect(stats.PassedThrough).To(Equal(1))

		body := []byte("handler won")
		handler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/handler-before-hold"}, Callsite: "catalog/handler-winner.ts:1",
			Response: &engine.ResponseOverride{Body: &body},
		})
		Expect(err).NotTo(HaveOccurred())
		laterHold, err := session.HoldResponseWithOptions(ctx,
			engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/handler-before-hold"},
			engine.ResponseHoldOptions{Callsite: "catalog/hold-shadowed.ts:2"},
		)
		Expect(err).NotTo(HaveOccurred())
		result, err = session.EvaluateAsync(ctx, `fetch("/handler-before-hold").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("handler won"))
		laterStats, err := session.ResponseHoldStats(laterHold)
		Expect(err).NotTo(HaveOccurred())
		Expect(laterStats.Count).To(Equal(0))
		handlerStats, err := session.NetworkHandlerStats(handler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(handlerStats.Count).To(Equal(1))
		handlerWins := session.NetworkShadowDiagnostics()[len(session.NetworkShadowDiagnostics())-1]
		Expect(handlerWins).To(SatisfyAll(
			HaveField("Stage", Equal(engine.NetworkStageResponse)),
			HaveField("Winner", Equal(engine.NetworkOwnerProvenance{
				Kind: engine.NetworkOwnerHandler, ID: handler.ID, Callsite: "catalog/handler-winner.ts:1", Count: 1,
			})),
			HaveField("Shadowed", Equal([]engine.NetworkOwnerProvenance{{
				Kind: engine.NetworkOwnerHold, ID: laterHold, Callsite: "catalog/hold-shadowed.ts:2", Shadowed: 1,
			}})),
		))
		Expect(session.ReleaseResponseHold(ctx, laterHold)).To(Succeed())
		Expect(session.RemoveNetworkHandler(ctx, handler.ID)).To(Succeed())

		earlierHold, err := session.HoldResponseWithOptions(ctx,
			engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/hold-before-handler"},
			engine.ResponseHoldOptions{Callsite: "catalog/hold-winner.ts:3"},
		)
		Expect(err).NotTo(HaveOccurred())
		laterHandler, err := session.RegisterNetworkHandler(ctx, engine.NetworkHandlerOptions{
			URL: engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/hold-before-handler"}, Callsite: "catalog/handler-shadowed.ts:4",
			Response: &engine.ResponseOverride{Body: &body},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `void fetch("/hold-before-handler")`)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.AwaitResponseHold(ctx, earlierHold)
		Expect(err).NotTo(HaveOccurred())
		laterHandlerStats, err := session.NetworkHandlerStats(laterHandler.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(laterHandlerStats.Shadowed).To(Equal(1))
		earlierStats, err := session.ResponseHoldStats(earlierHold)
		Expect(err).NotTo(HaveOccurred())
		Expect(earlierStats.Count).To(Equal(1))
		holdWins := session.NetworkShadowDiagnostics()[len(session.NetworkShadowDiagnostics())-1]
		Expect(holdWins).To(SatisfyAll(
			HaveField("Stage", Equal(engine.NetworkStageResponse)),
			HaveField("Winner", Equal(engine.NetworkOwnerProvenance{
				Kind: engine.NetworkOwnerHold, ID: earlierHold, Callsite: "catalog/hold-winner.ts:3", Count: 1,
			})),
			HaveField("Shadowed", Equal([]engine.NetworkOwnerProvenance{{
				Kind: engine.NetworkOwnerHandler, ID: laterHandler.ID, Callsite: "catalog/handler-shadowed.ts:4", Shadowed: 1,
			}})),
		))
		Expect(session.ReleaseResponseHold(ctx, earlierHold)).To(Succeed())

		boundedHold, err := session.HoldResponseWithOptions(ctx,
			engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/bounded-hold"},
			engine.ResponseHoldOptions{MaxBodyBytes: 4},
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `void fetch("/bounded-hold")`)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.AwaitResponseHold(ctx, boundedHold)
		Expect(err).To(MatchError(ContainSubstring("exceeds limit")))
		Expect(session.ReleaseResponseHold(ctx, boundedHold)).To(Succeed())

		Expect(session.Close()).To(Succeed())
		Expect(session.NetworkShadowDiagnostics()).To(BeEmpty())
		_, err = session.ResponseHoldStats(earlierHold)
		Expect(err).To(MatchError(ContainSubstring("not found")))
	})

	It("releases an in-flight response hold before closing the session", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		holdID, err := session.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/held"})
		Expect(err).NotTo(HaveOccurred())
		done := make(chan error, 1)
		go func() {
			_, evaluateErr := session.EvaluateAsync(ctx, `fetch("/held").then(r => r.text())`)
			done <- evaluateErr
		}()
		_, err = session.AwaitResponseHold(ctx, holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(session.Close()).To(Succeed())
		Eventually(done).Should(Receive())
	})

	It("releases holds and marks active downloads cancelled when the page crashes", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		holdID, err := session.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/held"})
		Expect(err).NotTo(HaveOccurred())
		done := make(chan error, 1)
		go func() {
			_, evaluateErr := session.EvaluateAsync(ctx, `fetch("/held").then(r => r.text())`)
			done <- evaluateErr
		}()
		_, err = session.AwaitResponseHold(ctx, holdID)
		Expect(err).NotTo(HaveOccurred())
		engine.MarkSessionCrashedForTest(session)
		Eventually(done).Should(Receive())

		// Prepare recovers the synthetic crash before exercising the download side of the same hook.
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Evaluate(ctx, `(() => { const a=document.createElement("a"); a.href="/slow-download"; a.download=""; document.documentElement.append(a); a.click(); })()`)
		Expect(err).NotTo(HaveOccurred())
		download, err := session.WaitForDownload(ctx, engine.DownloadQuery{State: engine.DownloadActive}, engine.PollPolicy{Timeout: time.Second})
		Expect(err).NotTo(HaveOccurred())
		engine.MarkSessionCrashedForTest(session)
		Eventually(session.Downloads).WithArguments(engine.DownloadQuery{State: engine.DownloadCancelled}).Should(ContainElement(HaveField("ID", download.ID)))
	})
})

func stringPtr(value string) *string { return &value }
func intPtr(value int) *int          { return &value }
func bytesPtr(value []byte) *[]byte  { return &value }
