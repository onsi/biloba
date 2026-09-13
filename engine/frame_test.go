package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("cross-origin frame targets", func() {
	It("routes trusted input and uploads to an offset transformed same-process frame", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><style>button{position:absolute;left:20px;top:20px;width:120px;height:50px}</style><button id="child-button" onclick="document.querySelector('#status').textContent='child-clicked'">Child</button><div id="status">idle</div><input id="child-upload" type="file">`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<button id="parent-button" style="position:fixed;left:0;top:0;width:250px;height:150px" onclick="this.dataset.clicked=\'yes\'">Parent</button><input id="parent-upload" type="file"><iframe id="child" style="position:fixed;left:400px;top:280px;width:320px;height:220px;border:10px solid black;transform:rotate(2deg) scale(.9);transform-origin:top left"></iframe>'; document.querySelector('#child').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.RealisticClick(ctx, engine.CSS("#child-button"))).To(Succeed())
		status, err := frame.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Value).To(Equal("child-clicked"))
		parentClicked, err := root.Evaluate(ctx, `document.querySelector('#parent-button').dataset.clicked || ''`)
		Expect(err).NotTo(HaveOccurred())
		Expect(parentClicked).To(Equal(""))

		path := GinkgoT().TempDir() + "/frame-upload.txt"
		Expect(os.WriteFile(path, []byte("frame"), 0o600)).To(Succeed())
		upload, err := frame.SetUpload(ctx, engine.CSS("#child-upload"), []string{path})
		Expect(err).NotTo(HaveOccurred())
		Expect(upload.Found).NotTo(BeNil())
		Expect(*upload.Found).To(BeTrue())
		childFiles, err := frame.Evaluate(ctx, `document.querySelector('#child-upload').files.length`)
		Expect(err).NotTo(HaveOccurred())
		Expect(childFiles).To(BeEquivalentTo(1))
		parentFiles, err := root.Evaluate(ctx, `document.querySelector('#parent-upload').files.length`)
		Expect(err).NotTo(HaveOccurred())
		Expect(parentFiles).To(BeEquivalentTo(0))
	})

	It("does not let a timing-out waiter close an independently discovered handle", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>present</title><div id="alive">alive</div>`))
		}))
		DeferCleanup(child.Close)
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe></iframe>'; document.querySelector('iframe').src = `+strconv.Quote(child.URL))
		Expect(err).NotTo(HaveOccurred())
		ready, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		Expect(ready.Close()).To(Succeed())

		waitDone := make(chan error, 1)
		go func() {
			_, waitErr := root.WaitForFrame(context.Background(), engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "never"}}, engine.PollPolicy{Timeout: 120 * time.Millisecond, Interval: 5 * time.Millisecond})
			waitDone <- waitErr
		}()
		time.Sleep(20 * time.Millisecond)
		frames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(frames).To(HaveLen(1))
		DeferCleanup(frames[0].Close)
		Expect(<-waitDone).To(HaveOccurred())
		text, err := frames[0].Text(ctx, engine.CSS("#alive"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("alive"))
	})

	It("filters sibling OOPIFs from frame-local discovery", func(ctx SpecContext) {
		sameProcess := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>same-process</title><div id="same">same</div>`))
		}))
		DeferCleanup(sameProcess.Close)
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		oopifURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/destination"
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe id="same"></iframe><iframe id="oopif"></iframe>'; document.querySelector('#same').src = `+strconv.Quote(sameProcess.URL)+`; document.querySelector('#oopif').src = `+strconv.Quote(oopifURL))
		Expect(err).NotTo(HaveOccurred())
		same, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "same-process"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(same.Close)
		oopif, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(oopif.Close)
		children, err := same.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(children).To(BeEmpty())
	})

	It("discovers mixed-process descendants and keeps nested handles independent", func(ctx SpecContext) {
		nested := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>nested</title><div id="nested">nested-alive</div>`))
		}))
		DeferCleanup(nested.Close)
		nestedURL := strings.Replace(nested.URL, "127.0.0.1", "localhost", 1)
		outer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte(`<!doctype html><title>outer</title><div id="outer">outer-alive</div><iframe src="` + nestedURL + `"></iframe>`))
		}))
		DeferCleanup(outer.Close)
		outerURL := strings.Replace(outer.URL, "127.0.0.1", "localhost", 1)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe></iframe>'; document.querySelector('iframe').src = `+strconv.Quote(outerURL))
		Expect(err).NotTo(HaveOccurred())
		nestedFromRoot, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "nested"}}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nestedFromRoot.Close)
		rootFrames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, frame := range rootFrames {
				_ = frame.Close()
			}
		})
		Expect(rootFrames).To(HaveLen(2))

		outerFrame, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "outer"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		nestedFrame, err := outerFrame.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "nested"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nestedFrame.Close)
		Expect(outerFrame.Close()).To(Succeed())
		text, err := nestedFrame.Text(ctx, engine.CSS("#nested"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("nested-alive"))

		reacquiredOuter, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "outer"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(reacquiredOuter.Close)
		reacquiredNested, err := reacquiredOuter.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "nested"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(reacquiredNested.Close)
		text, err = reacquiredNested.Text(ctx, engine.CSS("#nested"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("nested-alive"))
	})

	It("discovers and drives a same-site cross-origin frame", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><title>` + request.URL.Query().Get("id") + `</title><form onsubmit="event.preventDefault(); success.hidden = false; success.textContent = document.querySelector('input').value"><input name="email"><button type="submit">Submit</button></form><div id="success" hidden></div>`))
		}))
		DeferCleanup(child.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `(() => { for (const id of ["one", "two"]) { const frame = document.createElement("iframe"); frame.id = id; frame.src = `+strconv.Quote(child.URL+"/child-form")+` + "?id=" + id; document.body.append(frame) } })()`)
		Expect(err).NotTo(HaveOccurred())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectContains, Expected: "id=one"},
			HasElement: selectorPtr(engine.CSS(`input[name="email"]`)),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.SetValue(ctx, engine.CSS(`input[name="email"]`), "ada@example.com")).To(Succeed())
		Expect(frame.Click(ctx, engine.CSS(`button[type="submit"]`))).To(Succeed())
		visible, err := frame.Visible(ctx, engine.CSS("#success"))
		Expect(err).NotTo(HaveOccurred())
		Expect(visible.Value).To(BeTrue())
		text, err := frame.Text(ctx, engine.CSS("#success"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("ada@example.com"))

		frames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(frames).To(HaveLen(2))
		Expect(frames[0].FrameID()).NotTo(Equal(frames[1].FrameID()))
		two, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "two"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(two.Close)
		Expect(two.SetValue(ctx, engine.CSS(`input[name="email"]`), "grace@example.com")).To(Succeed())
		value, err := frame.Value(ctx, engine.CSS(`input[name="email"]`))
		Expect(err).NotTo(HaveOccurred())
		Expect(value.Value).To(Equal("ada@example.com"))

		_, err = root.Evaluate(ctx, `document.querySelector("#one").remove()`)
		Expect(err).NotTo(HaveOccurred())
		_, err = frame.Text(ctx, engine.CSS("#success"))
		var detached *engine.Error
		Expect(errors.As(err, &detached)).To(BeTrue())
		Expect(detached.Code).To(Equal(engine.CodeFrameDetached))

		_, err = root.Evaluate(ctx, `document.querySelector("#two").src = `+strconv.Quote(child.URL+"/child-form?id=replaced"))
		Expect(err).NotTo(HaveOccurred())
		replacement, err := root.WaitForFrame(ctx, engine.FrameQuery{URL: &engine.Expectation{Kind: engine.ExpectContains, Expected: "id=replaced"}}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(replacement.Close)
		_, err = two.Value(ctx, engine.CSS(`input[name="email"]`))
		Expect(errors.As(err, &detached)).To(BeTrue())
		Expect(detached.Code).To(Equal(engine.CodeFrameDetached))

		Expect(root.Navigate(ctx, server.URL+"/destination")).To(Succeed())
		_, err = replacement.Value(ctx, engine.CSS(`input[name="email"]`))
		Expect(errors.As(err, &detached)).To(BeTrue())
		Expect(detached.Code).To(Equal(engine.CodeFrameDetached))
	})

	It("discovers an isolated frame and drives its DOM through a scoped handle", func(ctx SpecContext) {
		isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
			ExecutablePath: chromePath(),
			Arguments:      []string{"--site-per-process"},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(isolatedBrowser.Close)
		root, err := isolatedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())

		frameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/destination"
		_, err = root.Evaluate(ctx, `(() => { const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(frameURL)+`; document.body.append(frame) })()`)
		Expect(err).NotTo(HaveOccurred())

		frame, err := root.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/destination"},
			HasElement: selectorPtr(engine.TestID("destination")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)

		parsed, err := url.Parse(frame.URL())
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Hostname()).To(Equal("localhost"))
		text, err := frame.Text(ctx, engine.TestID("destination"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("arrived"))
		Expect(sessionTargetIDs(isolatedBrowser.Sessions())).To(ContainElement(string(frame.TargetID())))
		Expect(frame.Close()).To(Succeed())
		Expect(frame.Close()).To(Succeed())
		Expect(sessionTargetIDs(isolatedBrowser.Sessions())).NotTo(ContainElement(string(frame.TargetID())))
		stillPresent, err := root.Evaluate(ctx, `document.querySelector("iframe") !== null`)
		Expect(err).NotTo(HaveOccurred())
		Expect(stillPresent).To(BeTrue())
		_, err = frame.Text(ctx, engine.TestID("destination"))
		Expect(err).To(MatchError(ContainSubstring("session is closed")))

		sibling, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(sibling.Close)
		Expect(sibling.Navigate(ctx, server.URL)).To(Succeed())
		siblingFrameURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/dom-surface"
		_, err = sibling.Evaluate(ctx, `(() => { const frame = document.createElement("iframe"); frame.src = `+strconv.Quote(siblingFrameURL)+`; document.body.append(frame) })()`)
		Expect(err).NotTo(HaveOccurred())
		siblingFrame, err := sibling.WaitForFrame(ctx, engine.FrameQuery{
			URL: &engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/dom-surface"},
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		Expect(siblingFrame.Close()).To(Succeed())

		rootFrames, err := root.Frames(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, discovered := range rootFrames {
				_ = discovered.Close()
			}
		})
		Expect(rootFrames).To(HaveLen(1), "a sibling tab's frame must not leak into this session")
		Expect(rootFrames[0].URL()).To(HaveSuffix("/destination"))
		Expect(root.Prepare(ctx)).To(Succeed())
		_, err = rootFrames[0].Text(ctx, engine.TestID("destination"))
		Expect(err).To(MatchError(ContainSubstring("session is closed")))
	})

	It("tracks page worlds that already exist when a popup tab is discovered", func(ctx SpecContext) {
		child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><div id="ready">ready</div><script>
				window.popupFrameWindow = "page-window";
				const popupFrameLexical = "page-lexical";
			</script>`))
		}))
		DeferCleanup(child.Close)

		popupSite := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/popup" {
				_, _ = response.Write([]byte(`<!doctype html><iframe src=` + strconv.Quote(child.URL) + ` onload="opener.postMessage('popup-frame-ready', '*')"></iframe>`))
				return
			}
			_, _ = response.Write([]byte(`<!doctype html><title>opener</title>`))
		}))
		DeferCleanup(popupSite.Close)

		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, popupSite.URL)).To(Succeed())
		ready, err := root.EvaluateAsync(ctx, `new Promise((resolve, reject) => {
			const listener = event => {
				if (event.data !== "popup-frame-ready") return;
				removeEventListener("message", listener);
				resolve(true);
			};
			addEventListener("message", listener);
			if (!window.open("/popup", "_blank")) reject(new Error("popup was blocked"));
		})`)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready).To(BeTrue(), "the popup's frame must finish loading before Biloba attaches to its tab")

		tabs, err := root.Tabs(ctx)
		Expect(err).NotTo(HaveOccurred())
		var popup *engine.Session
		for _, tab := range tabs {
			if tab.OpenerID() == root.TargetID() {
				popup = tab
				break
			}
		}
		Expect(popup).NotTo(BeNil())
		DeferCleanup(popup.Close)

		frame, err := popup.WaitForFrame(ctx, engine.FrameQuery{
			URL:        &engine.Expectation{Kind: engine.ExpectEqual, Expected: child.URL + "/"},
			HasElement: selectorPtr(engine.CSS("#ready")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(frame.Close)
		Expect(frame.TargetID()).To(Equal(popup.TargetID()), "the fixture must exercise a frame in the popup's renderer")
		values, err := frame.Evaluate(ctx, `[window.popupFrameWindow, popupFrameLexical]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(values).To(Equal([]any{"page-window", "page-lexical"}))
	})

	DescribeTable("evaluates in the frame's page world",
		func(ctx SpecContext, forceOOPIF bool) {
			child := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/html")
				id := request.URL.Query().Get("id")
				_, _ = response.Write([]byte(`<!doctype html>
					<div id="observed">idle</div>
					<script>
						window.frameWindowValue = ` + strconv.Quote(id+"-window") + `;
						const frameLexicalState = {value: ` + strconv.Quote(id+"-lexical") + `};
						addEventListener("biloba-sync", () => {
							document.querySelector("#observed").textContent = window.frameWindowValue + "|" + frameLexicalState.value;
						});
					</script>`))
			}))
			DeferCleanup(child.Close)

			frameBaseURL := child.URL
			activeBrowser := browser
			if forceOOPIF {
				frameBaseURL = strings.Replace(frameBaseURL, "127.0.0.1", "localhost", 1)
				isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
					ExecutablePath: chromePath(),
					Arguments:      []string{"--site-per-process"},
				})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(isolatedBrowser.Close)
				activeBrowser = isolatedBrowser
			}

			parent := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/html")
				_, _ = response.Write([]byte(`<!doctype html>
					<iframe id="selected" src=` + strconv.Quote(frameBaseURL+"/frame?id=selected") + `></iframe>
					<iframe id="sibling" src=` + strconv.Quote(frameBaseURL+"/frame?id=sibling") + `></iframe>
					<script>
						window.frameWindowValue = "parent-window";
						const frameLexicalState = {value: "parent-lexical"};
						window.pendingFrameEvaluationStarted = false;
						addEventListener("message", event => {
							if (event.data === "pending-frame-evaluation-started") window.pendingFrameEvaluationStarted = true;
						});
					</script>`))
			}))
			DeferCleanup(parent.Close)

			root, err := activeBrowser.OpenSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(root.Close)
			Expect(root.Navigate(ctx, parent.URL)).To(Succeed())

			selected, err := root.WaitForFrame(ctx, engine.FrameQuery{
				URL:        &engine.Expectation{Kind: engine.ExpectContains, Expected: "id=selected"},
				HasElement: selectorPtr(engine.CSS("#observed")),
			}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(selected.Close)
			sibling, err := root.WaitForFrame(ctx, engine.FrameQuery{
				URL:        &engine.Expectation{Kind: engine.ExpectContains, Expected: "id=sibling"},
				HasElement: selectorPtr(engine.CSS("#observed")),
			}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sibling.Close)

			Expect(selected.FrameID()).NotTo(Equal(sibling.FrameID()))
			Expect(selected.TargetID() != root.TargetID()).To(Equal(forceOOPIF), "the fixture must use the requested renderer process model")

			value, err := selected.Evaluate(ctx, `[window.frameWindowValue, frameLexicalState.value]`)
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal([]any{"selected-window", "selected-lexical"}))

			// An extension or another tool can create isolated worlds in the same frame. Their
			// context-created events must not replace the page's default world in Biloba's registry.
			targetCtx := engine.SessionContextForTest(selected.Session)
			err = chromedp.Run(targetCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
				_, createErr := page.CreateIsolatedWorld(selected.FrameID()).
					WithWorldName("biloba-main-world-regression").Do(runCtx)
				return createErr
			}))
			Expect(err).NotTo(HaveOccurred())
			value, err = selected.Evaluate(ctx, `[window.frameWindowValue, frameLexicalState.value]`)
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal([]any{"selected-window", "selected-lexical"}))

			_, err = selected.Evaluate(ctx, `window.frameWindowValue = "updated-window"; frameLexicalState.value = "updated-lexical"; dispatchEvent(new Event("biloba-sync"))`)
			Expect(err).NotTo(HaveOccurred())
			observed, err := selected.Text(ctx, engine.CSS("#observed"))
			Expect(err).NotTo(HaveOccurred())
			Expect(observed.Value).To(Equal("updated-window|updated-lexical"), "a handler installed by the page must observe the updates")

			parentValues, err := root.Evaluate(ctx, `[window.frameWindowValue, frameLexicalState.value]`)
			Expect(err).NotTo(HaveOccurred())
			Expect(parentValues).To(Equal([]any{"parent-window", "parent-lexical"}))
			siblingValues, err := sibling.Evaluate(ctx, `[window.frameWindowValue, frameLexicalState.value]`)
			Expect(err).NotTo(HaveOccurred())
			Expect(siblingValues).To(Equal([]any{"sibling-window", "sibling-lexical"}))

			parentAccess, err := selected.Evaluate(ctx, `(() => { try { void parent.document.body; return "allowed" } catch (error) { return error.name } })()`)
			Expect(err).NotTo(HaveOccurred())
			Expect(parentAccess).To(Equal("SecurityError"), "evaluation must still obey the browser's same-origin policy")
			_, err = selected.Evaluate(ctx, `throw new Error("Inspected target navigated or closed")`)
			var pageError *engine.Error
			Expect(errors.As(err, &pageError)).To(BeTrue())
			Expect(pageError.Code).To(Equal(engine.CodeJavaScript), "page-thrown text must not be mistaken for a protocol navigation error")

			oldDocumentID := selected.FrameDocumentID()
			pending := make(chan error, 1)
			evaluationCtx, cancelEvaluation := context.WithTimeout(ctx, 3*time.Second)
			DeferCleanup(cancelEvaluation)
			go func() {
				_, evaluateErr := selected.EvaluateAsync(evaluationCtx, `new Promise(() => parent.postMessage("pending-frame-evaluation-started", "*"))`)
				pending <- evaluateErr
			}()
			Eventually(func() bool {
				started, evaluateErr := root.Evaluate(ctx, `window.pendingFrameEvaluationStarted`)
				return evaluateErr == nil && started == true
			}).WithTimeout(2 * time.Second).WithPolling(5 * time.Millisecond).Should(BeTrue())

			_, err = root.Evaluate(ctx, `document.querySelector("#selected").src = `+strconv.Quote(frameBaseURL+"/frame?id=replaced"))
			Expect(err).NotTo(HaveOccurred())
			var pendingErr error
			Eventually(pending).WithTimeout(2 * time.Second).Should(Receive(&pendingErr))
			var pendingDetached *engine.Error
			Expect(errors.As(pendingErr, &pendingDetached)).To(BeTrue())
			Expect(pendingDetached.Code).To(Equal(engine.CodeFrameDetached), "pending evaluation failed: %v (session context: %v)", pendingErr, engine.SessionContextForTest(selected.Session).Err())

			replacement, err := root.WaitForFrame(ctx, engine.FrameQuery{
				URL:        &engine.Expectation{Kind: engine.ExpectContains, Expected: "id=replaced"},
				HasElement: selectorPtr(engine.CSS("#observed")),
			}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(replacement.Close)
			Expect(replacement.FrameDocumentID()).NotTo(Equal(oldDocumentID))

			_, err = selected.Evaluate(ctx, `window.frameWindowValue`)
			var detached *engine.Error
			Expect(errors.As(err, &detached)).To(BeTrue())
			Expect(detached.Code).To(Equal(engine.CodeFrameDetached))

			replacementValues, err := replacement.Evaluate(ctx, `[window.frameWindowValue, frameLexicalState.value]`)
			Expect(err).NotTo(HaveOccurred())
			Expect(replacementValues).To(Equal([]any{"replaced-window", "replaced-lexical"}))
		},
		Entry("for a same-process cross-origin frame", false),
		Entry("for an out-of-process iframe", true),
	)
})
