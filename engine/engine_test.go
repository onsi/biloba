package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

func TestEngine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Engine Suite")
}

var _ = Describe("runner-neutral engine primitives", func() {
	It("embeds the canonical Biloba browser runtime without drift", func() {
		canonical, err := os.ReadFile("../biloba.js")
		Expect(err).NotTo(HaveOccurred())
		embedded, err := os.ReadFile("biloba.js")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(embedded)).To(Equal(string(canonical)))
	})

	It("encodes the pilot locator forms for biloba.js", func() {
		Expect(engine.CSS("main > button").Encoded()).To(Equal("smain > button"))
		Expect(engine.TestID("save").Encoded()).To(HavePrefix("a"))
		Expect(engine.Text("Saved", engine.Contains).First().Encoded()).To(ContainSubstring(`"nth":0`))
		Expect(engine.Role("button", "Submit", engine.Exact).Encoded()).To(ContainSubstring(`"nameMode":"exact"`))
		Expect(engine.Role("button", "Submit", engine.Exact).First().Description()).To(Equal(`getByRole("button", name="Submit", exact).first()`))
	})

	It("polls entirely in Go and returns the attempt trajectory", func() {
		attempt := 0
		result, err := engine.Poll(context.Background(), engine.PollPolicy{
			Timeout:  250 * time.Millisecond,
			Interval: time.Millisecond,
		}, func(context.Context) (engine.Observation, bool, error) {
			attempt++
			observation := engine.Observation{Value: attempt}
			return observation, attempt == 3, nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Final.Value).To(Equal(3))
		Expect(result.AttemptCount).To(Equal(3))
		Expect(result.Attempts).To(HaveLen(3))
	})

	It("stops polling the moment an attempt reports something retrying cannot fix", func() {
		// The engine's answer to gomega.StopTrying.  Without it a poll spends its whole budget on an
		// unfixable failure and then reports a timeout, which blames the page for something that was
		// never about the page - see the browser-gone spec below.
		attempts := 0
		started := time.Now()
		_, err := engine.Poll(context.Background(), engine.PollPolicy{Timeout: time.Minute, Interval: time.Millisecond},
			func(context.Context) (engine.Observation, bool, error) {
				attempts++
				return engine.Observation{Value: "gone"}, false, engine.Fatal(errors.New("the browser exited"))
			})

		Expect(attempts).To(Equal(1), "a fatal attempt must not be retried")
		Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second), "the poll must not wait out its timeout")
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Message).To(ContainSubstring("the browser exited"))
		Expect(engineErr.Attempts).To(HaveLen(1), "the attempts made so far still travel with the failure")
	})

	It("keeps the failing operation's own code when it stops early", func() {
		_, err := engine.Poll(context.Background(), engine.PollPolicy{Timeout: time.Minute, Interval: time.Millisecond},
			func(context.Context) (engine.Observation, bool, error) {
				return engine.Observation{}, false, &engine.Error{Code: engine.CodeBrowserGone, Operation: "evaluate", Message: "the browser is gone"}
			})

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeBrowserGone), "a timeout code here would misdescribe the failure")
		Expect(engineErr.Operation).To(Equal("evaluate"))
	})

	It("retries an ordinary failure and treats an unfixable one as fatal", func() {
		Expect(engine.IsFatal(errors.New("not ready yet"))).To(BeFalse())
		Expect(engine.IsFatal(&engine.Error{Code: engine.CodeNotFound})).To(BeFalse(), "an element can still appear")
		Expect(engine.IsFatal(&engine.Error{Code: engine.CodeBrowserGone})).To(BeTrue())
		Expect(engine.IsFatal(&engine.Error{Code: engine.CodeSessionClosed})).To(BeTrue())
		Expect(engine.IsFatal(&engine.Error{Code: engine.CodeInvalidSelector})).To(BeTrue(), "no amount of polling parses a bad selector")
	})

	It("preserves the final observation and typed cancellation failure", func() {
		ctx, cancel := context.WithCancel(context.Background())
		attempt := 0
		_, err := engine.Poll(ctx, engine.PollPolicy{Interval: time.Millisecond}, func(context.Context) (engine.Observation, bool, error) {
			attempt++
			cancel()
			return engine.Observation{Value: "still loading"}, false, errors.New("transient read")
		})

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeCanceled))
		Expect(engineErr.Observed).To(Equal("still loading"))
		Expect(engineErr.AttemptCount).To(Equal(1))
	})

	It("does not discard an attempt error merely because the attempt also reports a match", func() {
		attempts := 0
		_, err := engine.Poll(context.Background(), engine.PollPolicy{Timeout: 20 * time.Millisecond, Interval: time.Millisecond},
			func(context.Context) (engine.Observation, bool, error) {
				attempts++
				return engine.Observation{Value: "stale match"}, true, engine.Fatal(errors.New("observation failed"))
			})

		Expect(err).To(MatchError(ContainSubstring("observation failed")))
		Expect(attempts).To(Equal(1))
	})

	It("encodes raw JavaScript arguments identically wherever a call is generated", func() {
		encoded, err := engine.EncodeArgs(15, "literal", rawJSArg{placeholder: `"__placeholder__"`, expression: "app.numRecords + 1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(encoded).To(Equal(`[15,"literal",app.numRecords + 1]`))
	})

	It("returns an error - never a panic - when a cookie command is handed a context that is not a chromedp context", func() {
		Expect(func() {
			Expect(engine.ClearCookiesContext(context.Background(), "")).To(MatchError(chromedp.ErrInvalidContext))
			Expect(engine.SetCookiesContext(context.Background(), "", "http://example.com", []engine.Cookie{{Name: "a", Value: "b"}})).To(MatchError(chromedp.ErrInvalidContext))
		}).NotTo(Panic())
	})
})

var (
	browser *engine.Browser
	server  *httptest.Server
)

// chromePath resolves Chrome the same way the Go suite and bilobad do.  It fails the suite rather
// than skipping it: this suite is the only guard on engine/biloba.js matching the canonical
// biloba.js, and a suite that quietly skips is a suite that stops protecting anything.
func chromePath() string {
	GinkgoHelper()
	path := engine.LocateChrome("")
	Expect(path).NotTo(BeEmpty(),
		"The engine suite needs a chrome-headless-shell binary.\n"+
			"Install one with `make update-chrome` (or `npx @puppeteer/browsers install chrome-headless-shell@stable`),\n"+
			"put it on your PATH, or set %s=/path/to/chrome-headless-shell.", engine.ChromeEnvVar)
	return path
}

var _ = BeforeSuite(func() {
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/not-found" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(response, `<!doctype html><button aria-label="Save">Save</button><input data-testid="name"><div id="status">loading</div><script>document.querySelector('button').onclick=()=>{document.querySelector('#status').textContent='saved'};setTimeout(()=>{if(document.querySelector('#status').textContent==='loading')document.querySelector('#status').textContent='ready'},30)</script>`)
	}))
	var err error
	browser, err = engine.StartBrowser(context.Background(), engine.BrowserConfig{
		ExecutablePath: chromePath(),
		ArtifactDir:    GinkgoT().TempDir(),
	})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if browser != nil {
		Expect(browser.Close()).To(Succeed())
	}
	if server != nil {
		server.Close()
	}
})

var _ = Describe("browser engine", func() {
	It("lets independent worker engines share one Chrome process", func(ctx SpecContext) {
		Expect(browser.WebSocketURL()).To(HavePrefix("ws://"))
		firstWorker, err := engine.StartBrowser(ctx, engine.BrowserConfig{WebSocketURL: browser.WebSocketURL()})
		Expect(err).NotTo(HaveOccurred())
		secondWorker, err := engine.StartBrowser(ctx, engine.BrowserConfig{WebSocketURL: browser.WebSocketURL()})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(secondWorker.Close)

		firstSession, err := firstWorker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		secondSession, err := secondWorker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(secondSession.Close)
		Expect(firstSession.Navigate(ctx, server.URL)).To(Succeed())
		Expect(secondSession.Navigate(ctx, server.URL)).To(Succeed())
		Expect(firstWorker.Close()).To(Succeed())

		value, err := secondSession.Evaluate(ctx, "document.title = 'still connected'; document.title")
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("still connected"))
	})

	It("drives atomic actions and one-attempt reads in an isolated session", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		Expect(session.SetValue(ctx, engine.TestID("name"), "Biloba")).To(Succeed())
		value, err := session.Value(ctx, engine.TestID("name"))
		Expect(err).NotTo(HaveOccurred())
		Expect(value.Value).To(Equal("Biloba"))
		Expect(session.Click(ctx, engine.Role("button", "Save", engine.Exact))).To(Succeed())
		text, err := session.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("saved"))

		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err = session.Evaluate(canceled, "1")
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeCanceled))
		result, err := session.Evaluate(ctx, "2")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeNumerically("==", 2))
	})

	It("isolates cookies and storage while allowing sessions to run concurrently", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Close)
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())
		Expect(first.SetCookies(ctx, []engine.Cookie{{Name: "owner", Value: "first", Domain: "127.0.0.1", Path: "/"}})).To(Succeed())
		_, err = first.Evaluate(ctx, `localStorage.setItem("owner", "first")`)
		Expect(err).NotTo(HaveOccurred())

		var wait sync.WaitGroup
		values := make([]any, 2)
		errs := make([]error, 2)
		for index, session := range []*engine.Session{first, second} {
			wait.Add(1)
			go func(index int, session *engine.Session) {
				defer GinkgoRecover()
				defer wait.Done()
				values[index], errs[index] = session.Evaluate(ctx, `({storage: localStorage.getItem("owner"), cookies: document.cookie})`)
			}(index, session)
		}
		wait.Wait()
		Expect(errs).To(ConsistOf(BeNil(), BeNil()))
		Expect(values[0]).To(Equal(map[string]any{"storage": "first", "cookies": "owner=first"}))
		Expect(values[1]).To(Equal(map[string]any{"storage": nil, "cookies": ""}))
	})

	It("reads back the cookies it set, session and persistent alike", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		expires := time.Now().Add(time.Hour).Truncate(time.Second)
		Expect(session.SetCookies(ctx, []engine.Cookie{
			{Name: "sessionish", Value: "no-expiry", Domain: "127.0.0.1", Path: "/"},
			{Name: "persistent", Value: "expires", Domain: "127.0.0.1", Path: "/", Expires: expires},
		})).To(Succeed())

		cookies, err := session.GetCookies(ctx)
		Expect(err).NotTo(HaveOccurred())

		byName := map[string]engine.Cookie{}
		for _, cookie := range cookies {
			byName[cookie.Name] = cookie
		}
		Expect(byName).To(HaveKey("sessionish"))
		Expect(byName["sessionish"].Value).To(Equal("no-expiry"))
		Expect(byName["sessionish"].Session).To(BeTrue(), "a cookie set without an expiry is a session cookie")
		Expect(byName["sessionish"].Expires).To(BeZero())
		Expect(byName).To(HaveKey("persistent"))
		Expect(byName["persistent"].Session).To(BeFalse())
		Expect(byName["persistent"].Expires).To(BeTemporally("~", expires, time.Second))
	})

	It("reads cookies only from its own browser context", func(ctx SpecContext) {
		first, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Close)
		second, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(second.Close)
		Expect(first.Navigate(ctx, server.URL)).To(Succeed())
		Expect(second.Navigate(ctx, server.URL)).To(Succeed())
		Expect(first.SetCookies(ctx, []engine.Cookie{{Name: "mine", Value: "only", Domain: "127.0.0.1", Path: "/"}})).To(Succeed())

		mine, err := first.GetCookies(ctx)
		Expect(err).NotTo(HaveOccurred())
		theirs, err := second.GetCookies(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(namesOf(mine)).To(ContainElement("mine"))
		Expect(namesOf(theirs)).NotTo(ContainElement("mine"), "a read must be scoped to its own browser context, like the write is")
	})

	It("prepare clears cookies and web storage before returning to about:blank", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		Expect(session.SetCookies(ctx, []engine.Cookie{{Name: "session", Value: "present", Domain: "127.0.0.1", Path: "/"}})).To(Succeed())
		_, err = session.Evaluate(ctx, `localStorage.setItem("session", "present")`)
		Expect(err).NotTo(HaveOccurred())

		Expect(session.Prepare(ctx)).To(Succeed())
		location, err := session.URL(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(location.Value).To(Equal("about:blank"))
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		state, err := session.Evaluate(ctx, `({storage: localStorage.getItem("session"), cookies: document.cookie})`)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal(map[string]any{"storage": nil, "cookies": ""}))
	})

	It("prepare clears a recorded page crash after its recovery navigation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		engine.MarkSessionCrashedForTest(session)

		Expect(session.Prepare(ctx)).To(Succeed())
		value, err := session.Evaluate(ctx, "1 + 1")

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeNumerically("==", 2))
	})

	It("returns a typed navigation failure for a non-200 document", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)

		err = session.Navigate(ctx, server.URL+"/not-found")

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeNavigation))
		Expect(engineErr.Observed).To(BeNumerically("==", http.StatusNotFound))
	})

	It("classifies an HTTP loading failure from the error alone, independently of any status it observed", func() {
		// the two observations are independent decisions: a 4xx/5xx whose document response we
		// never saw (listener registration race, target swapped mid-navigation) is still an HTTP
		// failure, and must not be folded into "this destination has no HTTP response at all".
		Expect(engine.HTTPStatusFailureForTest(errors.New("page load error net::ERR_HTTP_RESPONSE_CODE_FAILURE"))).To(BeTrue())
		Expect(engine.HTTPStatusFailureForTest(errors.New("page load error net::ERR_NAME_NOT_RESOLVED"))).To(BeFalse())
		Expect(engine.HTTPStatusFailureForTest(nil)).To(BeFalse())
	})

	It("reports the document status and the loading failure it saw on one navigation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		tab := engine.SessionContextForTest(session)

		result, err := engine.NavigateContext(tab, server.URL+"/not-found")

		Expect(result.Status).To(Equal(http.StatusNotFound))
		Expect(result.HTTPFailure).To(Equal(engine.HTTPStatusFailureForTest(err)))
	})

	It("attaches a worker to a shared Chrome without leaving an idle tab behind", func(ctx SpecContext) {
		before := pageTargetCount(browser.WebSocketURL())

		worker, err := engine.StartBrowser(ctx, engine.BrowserConfig{WebSocketURL: browser.WebSocketURL()})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(worker.Close)

		Expect(pageTargetCount(browser.WebSocketURL())).To(Equal(before), "attaching to a shared Chrome must not open a tab")

		session, err := worker.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		Expect(pageTargetCount(browser.WebSocketURL())).To(Equal(before+1), "the session's tab is the only tab a worker adds")
	})

	It("closes owned sessions when the browser closes", func(ctx SpecContext) {
		ownedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath()})
		Expect(err).NotTo(HaveOccurred())
		session, err := ownedBrowser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(ownedBrowser.Close()).To(Succeed())
		_, err = session.Evaluate(ctx, "1")

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeSessionClosed))
	})

	It("reinstalls the DOM runtime after the page replaces its JavaScript world", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, "globalThis._biloba = undefined")
		Expect(err).NotTo(HaveOccurred())

		text, err := session.Text(ctx, engine.CSS("#status"))

		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Or(Equal("loading"), Equal("ready")))
	})

	It("distinguishes a script that will never parse from one whose page has not caught up", func(ctx SpecContext) {
		// The line Biloba has always walked: a ReferenceError or a TypeError is the ordinary shape of
		// "not there yet" - biloba.js not installed, an element not rendered - and is worth retrying.
		// A SyntaxError is not; the expression cannot start parsing later.
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		_, syntaxErr := session.Evaluate(ctx, "syntax ~~ error")
		var engineErr *engine.Error
		Expect(errors.As(syntaxErr, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeInvalidScript))
		Expect(engine.IsFatal(syntaxErr)).To(BeTrue())

		for _, retryable := range []string{"notDefinedYet.value", "document.querySelector('#missing').value"} {
			_, runtimeErr := session.Evaluate(ctx, retryable)
			Expect(errors.As(runtimeErr, &engineErr)).To(BeTrue(), retryable)
			Expect(engineErr.Code).To(Equal(engine.CodeJavaScript), retryable)
			Expect(engine.IsFatal(runtimeErr)).To(BeFalse(), "%s must stay retryable - this is how a page that has not settled looks", retryable)
		}
	})

	It("keeps delayed assertion retries in Go and emits diagnostics", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		// Warm the bridge before timing anything.  Navigate clears s.installed, so the first handler
		// call is the one that evaluates all of biloba.js - leaving it cold would make the
		// attempt-count assertion below a race between that evaluate and the page's timer, which a
		// loaded machine loses.  This spec is about Go-side retries, not installation cost.
		_, err = session.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		// Drive the transition with values the fixture's own script will not touch.  That script
		// arms a 30ms timer flipping 'loading' to 'ready', so a spec that reuses those two words is
		// really being timed by the fixture - 30ms, barely one CDP round-trip - no matter what
		// delay it sets for itself.  'pending'/'settled' and 300ms make this spec's own timer the
		// one that counts: ~55 attempts at the 5ms poll interval, instead of the 4-5 this had
		// before - and it was those 4-5 that could collapse to 1 on a loaded machine.
		_, err = session.Evaluate(ctx, `document.querySelector('#status').textContent='pending';setTimeout(()=>document.querySelector('#status').textContent='settled',300)`)
		Expect(err).NotTo(HaveOccurred())
		result, err := engine.Poll(ctx, engine.PollPolicy{Timeout: time.Second, Interval: 5 * time.Millisecond}, func(attemptCtx context.Context) (engine.Observation, bool, error) {
			observed, readErr := session.Text(attemptCtx, engine.CSS("#status"))
			return observed, observed.Value == "settled", readErr
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.AttemptCount).To(BeNumerically(">", 1))
		diagnostics, err := session.CaptureDiagnostics(ctx, "engine-test")
		Expect(err).NotTo(HaveOccurred())
		Expect(diagnostics.DOMOutline).To(ContainSubstring(`<div id="status">`))
		Expect(diagnostics.ScreenshotPath).To(BeAnExistingFile())
	})
})

// pageTargetCount asks Chrome's HTTP DevTools endpoint how many page targets are open.  It is
// deliberately not a CDP connection: connecting one to count tabs is exactly the thing under
// test.
func pageTargetCount(webSocketURL string) int {
	parsed, err := url.Parse(webSocketURL)
	Expect(err).NotTo(HaveOccurred())
	response, err := http.Get("http://" + parsed.Host + "/json/list")
	Expect(err).NotTo(HaveOccurred())
	defer response.Body.Close()
	var targets []struct {
		Type string `json:"type"`
	}
	Expect(json.NewDecoder(response.Body).Decode(&targets)).To(Succeed())
	count := 0
	for _, target := range targets {
		if target.Type == "page" {
			count++
		}
	}
	return count
}

// rawJSArg is a stand-in for the runner's JSVar: the engine deliberately knows nothing about the
// biloba package, only about the RawJSArg interface an argument implements.
type rawJSArg struct {
	placeholder string
	expression  string
}

func (r rawJSArg) MarshalJSON() ([]byte, error) { return []byte(r.placeholder), nil }
func (r rawJSArg) RawJSPlaceholder() string     { return r.placeholder }
func (r rawJSArg) RawJSExpression() string      { return r.expression }

func namesOf(cookies []engine.Cookie) []string {
	names := make([]string, len(cookies))
	for index, cookie := range cookies {
		names[index] = cookie.Name
	}
	return names
}
