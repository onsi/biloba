package engine_test

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
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

var (
	cacheRequests         atomic.Int64
	cancelledDownloadHTTP atomic.Int64
)

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
		Expect(engine.XPath("//main//button").Encoded()).To(Equal("x//main//button"))
		Expect(engine.TestID("save").Encoded()).To(HavePrefix("a"))
		Expect(engine.Text("Saved", engine.Contains).First().Encoded()).To(ContainSubstring(`"nth":0`))
		Expect(engine.Role("button", "Submit", engine.Exact).Encoded()).To(ContainSubstring(`"nameMode":"exact"`))
		Expect(engine.Role("button", "Submit", engine.Exact).First().Description()).To(Equal(`getByRole("button", name="Submit", exact).first()`))
	})

	It("composes XPath with runner-neutral locators", func() {
		selector := engine.XPath("//article").Containing(engine.XPath(".//h2[text()=\"Ada\"]"))
		Expect(selector.Encoded()).To(ContainSubstring(`"selector":"x.//h2[text()=\"Ada\"]"`))
		Expect(selector.Description()).To(Equal(`xpath("//article").containing(xpath(".//h2[text()=\"Ada\"]"))`))
	})

	It("encodes the locator composition used by the consumer suite", func() {
		selector := engine.CSS(".row").
			ContainingText("Ada").
			Within(engine.TestID("results")).
			Nth(1)
		Expect(selector.Encoded()).To(And(
			HavePrefix("a"),
			ContainSubstring(`"within":"a{\"attr\":\"data-testid\"`),
			ContainSubstring(`"kind":"containsText"`),
			ContainSubstring(`"nth":1`),
		))
		Expect(selector.Description()).To(Equal(`locator(".row").containingText("Ada").within(getByTestId("results")).nth(1)`))

		union := engine.TestID("sent").Or(engine.TestID("delivered")).Last()
		Expect(union.Encoded()).To(And(ContainSubstring(`"by":"or"`), ContainSubstring(`"nth":-1`)))
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

	It("supports immediate and consistency polling without a runner", func() {
		immediateAttempts := 0
		immediate, err := engine.Poll(context.Background(), engine.PollPolicy{
			Mode: engine.PollImmediate,
		}, func(context.Context) (engine.Observation, bool, error) {
			immediateAttempts++
			return engine.Observation{Value: "loading"}, false, nil
		})
		Expect(err).To(HaveOccurred())
		Expect(immediate.AttemptCount).To(Equal(1))
		Expect(immediateAttempts).To(Equal(1))

		consistentAttempts := 0
		consistent, err := engine.Poll(context.Background(), engine.PollPolicy{
			Mode:     engine.PollConsistently,
			Timeout:  20 * time.Millisecond,
			Interval: 2 * time.Millisecond,
		}, func(context.Context) (engine.Observation, bool, error) {
			consistentAttempts++
			return engine.Observation{Value: "steady"}, true, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(consistent.AttemptCount).To(BeNumerically(">", 1))
		Expect(consistentAttempts).To(Equal(consistent.AttemptCount))

		terminalDeadlineAttempts := 0
		terminalDeadline, err := engine.Poll(context.Background(), engine.PollPolicy{
			Mode:     engine.PollConsistently,
			Timeout:  20 * time.Millisecond,
			Interval: time.Millisecond,
		}, func(ctx context.Context) (engine.Observation, bool, error) {
			terminalDeadlineAttempts++
			if terminalDeadlineAttempts == 1 {
				return engine.Observation{Value: "steady"}, true, nil
			}
			<-ctx.Done()
			return engine.Observation{}, false, ctx.Err()
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(terminalDeadline.Final.Value).To(Equal("steady"))
		Expect(terminalDeadline.AttemptCount).To(Equal(1))
		Expect(terminalDeadlineAttempts).To(Equal(2))

		terminalCanceledAttempts := 0
		terminalCanceled, err := engine.Poll(context.Background(), engine.PollPolicy{
			Mode:     engine.PollConsistently,
			Timeout:  20 * time.Millisecond,
			Interval: time.Millisecond,
		}, func(ctx context.Context) (engine.Observation, bool, error) {
			terminalCanceledAttempts++
			if terminalCanceledAttempts == 1 {
				return engine.Observation{Value: "steady"}, true, nil
			}
			<-ctx.Done()
			return engine.Observation{}, false, fmt.Errorf("browser operation: %w", context.Canceled)
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(terminalCanceled.Final.Value).To(Equal("steady"))
		Expect(terminalCanceled.AttemptCount).To(Equal(1))
		Expect(terminalCanceledAttempts).To(Equal(2))

		broken, err := engine.Poll(context.Background(), engine.PollPolicy{
			Mode:     engine.PollConsistently,
			Timeout:  time.Second,
			Interval: time.Millisecond,
		}, func(context.Context) (engine.Observation, bool, error) {
			return engine.Observation{Value: "changed"}, false, nil
		})
		Expect(err).To(HaveOccurred())
		Expect(broken.AttemptCount).To(Equal(1))
	})

	It("matches the typed expectation algebra used by runner adapters", func() {
		matches := func(actual any, expectation engine.Expectation) bool {
			matched, err := engine.MatchExpectation(actual, expectation)
			Expect(err).NotTo(HaveOccurred())
			return matched
		}

		Expect(matches("Ada Lovelace", engine.Expectation{Kind: engine.ExpectContains, Expected: "Lovelace"})).To(BeTrue())
		Expect(matches("Ada Lovelace", engine.Expectation{Kind: engine.ExpectRegexp, Expected: `^Ada\s`})).To(BeTrue())
		Expect(matches("Ada Lovelace", engine.Expectation{Kind: engine.ExpectPrefix, Expected: "Ada"})).To(BeTrue())
		Expect(matches("Ada Lovelace", engine.Expectation{Kind: engine.ExpectSuffix, Expected: "Lovelace"})).To(BeTrue())
		Expect(matches([]any{"primary", "wide"}, engine.Expectation{Kind: engine.ExpectContains, Expected: "primary"})).To(BeTrue())
		Expect(matches(3, engine.Expectation{Kind: engine.ExpectNumber, Operator: ">=", Expected: 2})).To(BeTrue())
		Expect(matches("", engine.Expectation{Kind: engine.ExpectEmpty})).To(BeTrue())
		Expect(matches("ready", engine.Expectation{Kind: engine.ExpectAll, Children: []engine.Expectation{
			{Kind: engine.ExpectContains, Expected: "read"},
			{Kind: engine.ExpectNot, Children: []engine.Expectation{{Kind: engine.ExpectEqual, Expected: "loading"}}},
		}})).To(BeTrue())
		Expect(matches("ready", engine.Expectation{Kind: engine.ExpectAny, Children: []engine.Expectation{
			{Kind: engine.ExpectEqual, Expected: "done"},
			{Kind: engine.ExpectEqual, Expected: "ready"},
		}})).To(BeTrue())

		_, err := engine.MatchExpectation("value", engine.Expectation{Kind: engine.ExpectRegexp, Expected: "["})
		Expect(err).To(MatchError(ContainSubstring("invalid regular expression")))
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

	It("can bound a stuck browser read without consuming the entire poll budget", func() {
		attempts := 0
		result, err := engine.Poll(context.Background(), engine.PollPolicy{
			Timeout: 100 * time.Millisecond, AttemptTimeout: 10 * time.Millisecond,
		}, func(ctx context.Context) (engine.Observation, bool, error) {
			attempts++
			if attempts == 1 {
				<-ctx.Done()
				return engine.Observation{}, false, ctx.Err()
			}
			return engine.Observation{Value: "ready"}, true, nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result.AttemptCount).To(Equal(2))
		Expect(result.Final.Value).To(Equal("ready"))
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
		if request.URL.Path == "/socket" {
			connection, readWriter, err := response.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			key := request.Header.Get("Sec-WebSocket-Key")
			digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
			fmt.Fprintf(readWriter, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(digest[:]))
			_ = readWriter.Flush()
			_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _ = connection.Read(make([]byte, 32))
			_ = connection.Close()
			return
		}
		if request.URL.Path == "/redirect" {
			http.Redirect(response, request, "/slow", http.StatusFound)
			return
		}
		if request.URL.Path == "/cacheable" {
			cacheRequests.Add(1)
			response.Header().Set("Cache-Control", "public, max-age=3600")
			fmt.Fprint(response, "cacheable")
			return
		}
		if request.URL.Path == "/slow-download" {
			response.Header().Set("Content-Disposition", `attachment; filename="slow.bin"`)
			response.Header().Set("Content-Type", "application/octet-stream")
			flusher, _ := response.(http.Flusher)
			chunk := make([]byte, 8192)
			for range 100 {
				select {
				case <-request.Context().Done():
					cancelledDownloadHTTP.Add(1)
					return
				default:
				}
				if _, err := response.Write(chunk); err != nil {
					cancelledDownloadHTTP.Add(1)
					return
				}
				flusher.Flush()
				time.Sleep(10 * time.Millisecond)
			}
			return
		}
		if request.URL.Path == "/download" {
			response.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
			response.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(response, "downloaded report")
			return
		}
		if request.URL.Path == "/echo" {
			body, _ := io.ReadAll(request.Body)
			response.Header().Set("X-Echo-Method", request.Method)
			response.Header().Set("X-Echo-Header", request.Header.Get("X-Changed"))
			fmt.Fprintf(response, "%s:%s", request.Method, body)
			return
		}
		if request.URL.Path == "/modifiable" {
			response.Header().Set("X-Original", "yes")
			response.WriteHeader(http.StatusCreated)
			fmt.Fprint(response, "original body")
			return
		}
		if request.URL.Path == "/transformed" {
			response.Header().Add("X-Duplicate", "first")
			response.Header().Add("X-Duplicate", "second")
			fmt.Fprint(response, "original body")
			return
		}
		if request.URL.Path == "/slow" {
			time.Sleep(150 * time.Millisecond)
			fmt.Fprint(response, "slow")
			return
		}
		if request.URL.Path == "/not-found" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		if request.URL.Path == "/destination" {
			fmt.Fprint(response, `<!doctype html><h1 data-testid="destination">arrived</h1>`)
			return
		}
		if request.URL.Path == "/dom-surface" {
			fmt.Fprint(response, `<!doctype html><style>#scroll{height:60px;width:160px;overflow:auto}#space{height:180px}.box{width:80px;height:40px}.hidden{display:none}#hover:hover{color:rgb(1, 2, 3)}#shout{text-transform:uppercase}</style><div id="texts">  visible <span class="hidden">SECRET</span> <span id="shout">shout</span> </div><label for="email">Email address</label><input id="email" placeholder="you@example.com" data-qa="email" value="Ada"><select id="choice"><option value="a">Alpha</option><option value="b">Beta</option></select><img alt="Portrait" title="Ada image"><button id="action" class="primary wide" data-json='{"ready":true}' data-note="memo">Act</button><input id="check" type="checkbox"><div id="hover" class="box">Hover</div><div id="scroll"><div id="space"></div><div id="target" class="box">target text</div></div><div class="item" data-rank="a">One</div><div class="item hidden" data-rank="b">Two</div><p id="selection">alpha <strong>beta</strong> alpha</p><script>window.events=[];for(const type of ['click','dblclick','contextmenu','auxclick','mouseover','focus','blur'])document.addEventListener(type,e=>window.events.push({type,button:e.button,shift:e.shiftKey,target:e.target.id}));document.querySelector('#action').customEcho=function(v){return this.id+':'+v}</script>`)
			return
		}
		fmt.Fprint(response, `<!doctype html><a data-testid="go" href="/destination">Go</a><button aria-label="Save">Save</button><input data-testid="name"><input data-testid="upload" type="file"><div id="status">loading</div><div data-testid="drag-source" style="width:40px;height:40px;background:red"></div><div data-testid="drop-target" style="width:100px;height:80px;background:blue;margin-left:200px"></div><div id="drag-status">waiting</div><script>document.querySelector('button').onclick=()=>{document.querySelector('#status').textContent='saved'};setTimeout(()=>{if(document.querySelector('#status').textContent==='loading')document.querySelector('#status').textContent='ready'},30);let dragging=false;document.querySelector('[data-testid="drag-source"]').onpointerdown=()=>dragging=true;document.onpointerup=e=>{if(dragging&&document.querySelector('[data-testid="drop-target"]').contains(document.elementFromPoint(e.clientX,e.clientY)))document.querySelector('#drag-status').textContent='dropped';dragging=false}</script>`)
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

	It("executes XPath after locator-level composition", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		text, err := session.Text(ctx, engine.XPath("//button").First())
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("Save"))
	})

	It("drives realistic pointer and keyboard input through the runner-neutral session", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		Expect(session.RealisticClick(ctx, engine.Role("button", "Save", engine.Exact))).To(Succeed())
		text, err := session.Text(ctx, engine.CSS("#status"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("saved"))

		Expect(session.RealisticSetValue(ctx, engine.TestID("name"), "Ada")).To(Succeed())
		Expect(session.Type(ctx, engine.TestID("name"), " Lovelace", true)).To(Succeed())
		value, err := session.Value(ctx, engine.TestID("name"))
		Expect(err).NotTo(HaveOccurred())
		Expect(value.Value).To(Equal("Ada Lovelace"))
	})

	It("keeps the target executor alive after a click initiates navigation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		clickCtx, cancelClick := context.WithTimeout(context.Background(), time.Second)
		Expect(session.RealisticClick(clickCtx, engine.TestID("go"))).To(Succeed())
		cancelClick()
		navigationCtx, cancelNavigation := context.WithTimeout(context.Background(), time.Second)
		_, err = engine.Poll(navigationCtx, engine.PollPolicy{Timeout: time.Second}, func(ctx context.Context) (engine.Observation, bool, error) {
			value, readErr := session.Evaluate(ctx, `window.location.pathname`)
			return engine.Observation{Value: value}, value == "/destination", readErr
		})
		Expect(err).NotTo(HaveOccurred())
		cancelNavigation()
		readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
		defer cancelRead()
		result, err := engine.Poll(readCtx, engine.PollPolicy{Timeout: time.Second}, func(ctx context.Context) (engine.Observation, bool, error) {
			observation, readErr := session.Text(ctx, engine.TestID("destination"))
			return observation, observation.Value == "arrived", readErr
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Final.Value).To(Equal("arrived"))
	})

	It("drags between resolved elements with browser pointer input", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		Expect(session.DragTo(ctx, engine.TestID("drag-source"), engine.TestID("drop-target"))).To(Succeed())
		status, err := session.Text(ctx, engine.CSS("#drag-status"))
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Value).To(Equal("dropped"))
	})

	It("opens a sibling tab in the same browser context and controls its document lifecycle", func(ctx SpecContext) {
		parent, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(parent.Close)
		Expect(parent.Navigate(ctx, server.URL)).To(Succeed())
		_, err = parent.Evaluate(ctx, `document.cookie = "shared=present; path=/"`)
		Expect(err).NotTo(HaveOccurred())

		sibling, err := parent.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(sibling.Close)
		Expect(sibling.AddInitScript(ctx, `window.__bilobaInitMarker = "installed"`)).To(Succeed())
		Expect(sibling.Navigate(ctx, server.URL)).To(Succeed())
		Expect(sibling.Activate(ctx)).To(Succeed())
		value, err := sibling.Evaluate(ctx, `[document.cookie.includes("shared=present"), window.__bilobaInitMarker]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal([]any{true, "installed"}))

		Expect(sibling.Close()).To(Succeed())
		value, err = parent.Evaluate(ctx, `document.cookie.includes("shared=present")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeTrue(), "closing a sibling tab must not dispose the shared browser context")
	})

	It("awaits promises, resizes the viewport, and sets file inputs through the runner-neutral session", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		value, err := session.EvaluateAsync(ctx, `Promise.resolve("settled")`)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("settled"))

		Expect(session.SetWindowSize(ctx, 375, 812)).To(Succeed())
		dimensions, err := session.Evaluate(ctx, `[window.innerWidth, window.innerHeight]`)
		Expect(err).NotTo(HaveOccurred())
		Expect(dimensions).To(Equal([]any{float64(375), float64(812)}))

		path := GinkgoT().TempDir() + "/avatar.txt"
		Expect(os.WriteFile(path, []byte("avatar"), 0o600)).To(Succeed())
		observation, err := session.SetUpload(ctx, engine.TestID("upload"), []string{path})
		Expect(err).NotTo(HaveOccurred())
		Expect(observation.Found).To(HaveValue(BeTrue()))
		name, err := session.Evaluate(ctx, `document.querySelector('[data-testid="upload"]').files[0].name`)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("avatar.txt"))
	})

	It("records requests made by the session", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err = session.EvaluateAsync(ctx, `fetch("/saved", {method: "POST"})`)
		Expect(err).NotTo(HaveOccurred())

		Eventually(session.Requests).Should(ContainElement(SatisfyAll(
			HaveField("URL", HaveSuffix("/saved")),
			HaveField("Method", Equal("POST")),
		)))
		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Requests()).To(BeEmpty())
	})

	It("holds a matching response until it is explicitly released", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		holdID, err := session.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/held"})
		Expect(err).NotTo(HaveOccurred())

		done := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			_, evaluateErr := session.EvaluateAsync(ctx, `fetch("/held").then(response => response.text())`)
			done <- evaluateErr
		}()
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		held, err := session.AwaitResponseHold(waitCtx, holdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(held.URL).To(HaveSuffix("/held"))
		Consistently(done, 20*time.Millisecond).ShouldNot(Receive())
		Expect(session.ReleaseResponseHold(ctx, holdID)).To(Succeed())
		Eventually(done).Should(Receive(Succeed()))
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
