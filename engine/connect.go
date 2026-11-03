package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// dialTimeout bounds the websocket handshake with Chrome, as chromedp's own dialers do.
const dialTimeout = 10 * time.Second

/*
NewExecAllocator is chromedp.NewExecAllocator for a Chrome that other processes attach to: Chrome
opens a debugging port (and writes DevToolsActivePort) rather than talking over a pipe, and the
allocator drives it through the ordered event stream ListenTarget reads (see events.go).  slowf, when
not nil, is told about listeners that hold their session's worker too long.
*/
func NewExecAllocator(parent context.Context, slowf func(string, ...any), opts ...chromedp.ExecAllocatorOption) (context.Context, context.CancelFunc) {
	allocator := &hubAllocator{}
	opts = append(opts, chromedp.WithDialer(func(ctx context.Context, wsURL string) (chromedp.Transport, error) {
		dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		conn, err := remote.DialContext(dialCtx, wsURL)
		if err != nil {
			return nil, err
		}
		hub := newEventHub(slowf) // one per connection
		allocator.mu.Lock()
		allocator.dialed = hub
		allocator.mu.Unlock()
		return newOrderedTransport(conn, hub), nil
	}))
	execCtx, cancelExec := chromedp.NewExecAllocator(parent, opts...)
	allocator.Allocator = chromedp.FromContext(execCtx).Allocator
	allocCtx, cancelAlloc := chromedp.NewAllocatorContext(execCtx, allocator)
	return allocCtx, func() {
		cancelAlloc()
		cancelExec() // waits for the browser process, as chromedp.NewExecAllocator's cancel does
	}
}

// hubAllocator registers the hub of each browser its allocator starts: the hub the dialer made for
// that browser's connection.
type hubAllocator struct {
	chromedp.Allocator
	mu       sync.Mutex
	allocate sync.Mutex // one Allocate at a time, so each picks up its own connection's hub
	dialed   *eventHub
}

func (a *hubAllocator) Allocate(ctx context.Context, opts ...chromedp.BrowserOption) (*chromedp.Browser, error) {
	a.allocate.Lock()
	defer a.allocate.Unlock()
	browser, err := a.Allocator.Allocate(ctx, opts...)
	a.mu.Lock()
	hub := a.dialed
	a.dialed = nil
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if hub == nil {
		return nil, errors.New("the browser's connection did not go through Biloba's dialer")
	}
	registerHub(browser, hub)
	return browser, nil
}

/*
NewRemoteAllocator is remote.NewAllocator, attaching to a running Chrome at wsURL through the ordered
event stream ListenTarget reads.  modifyURL resolves the address the way remote.NewAllocator does
(an IP for the host, and /json/version for an address without /devtools/browser/); without it the
address is used as given (remote.NoModifyURL).
*/
func NewRemoteAllocator(parent context.Context, wsURL string, modifyURL bool, slowf func(string, ...any)) (context.Context, context.CancelFunc) {
	return chromedp.NewAllocatorContext(parent, &attachAllocator{wsURL: wsURL, modifyURL: modifyURL, slowf: slowf})
}

// attachAllocator is remote.Allocator with the connection wrapped in an orderedTransport;
// remote.Allocator dials inside Allocate, which leaves nowhere to wrap it.  Each Allocate is a
// connection of its own, with a hub of its own: a connection that goes away (a retried bootstrap's
// first attempt) clears only its own listeners.
type attachAllocator struct {
	wsURL     string
	modifyURL bool
	slowf     func(string, ...any)
	wg        sync.WaitGroup
}

func (a *attachAllocator) Allocate(ctx context.Context, opts ...chromedp.BrowserOption) (*chromedp.Browser, error) {
	if chromedp.FromContext(ctx) == nil {
		return nil, chromedp.ErrInvalidContext
	}
	wsURL := a.wsURL
	if a.modifyURL {
		var err error
		if wsURL, err = modifyWebSocketURL(ctx, wsURL); err != nil {
			return nil, fmt.Errorf("modifying the websocket address: %w", err)
		}
	}
	// The connection gets its own context so the pages close before it does.
	wctx, cancel := context.WithCancel(context.Background())
	a.wg.Go(func() {
		<-ctx.Done()
		chromedp.Cancel(ctx) // blocks until the pages are closed
		cancel()
	})
	dialCtx, dialCancel := context.WithTimeout(wctx, dialTimeout)
	defer dialCancel()
	conn, err := remote.DialContext(dialCtx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("could not dial %q: %w", wsURL, err)
	}
	hub := newEventHub(a.slowf)
	browser, err := chromedp.NewBrowserTransport(wctx, newOrderedTransport(conn, hub), opts...)
	if err != nil {
		return nil, err
	}
	registerHub(browser, hub)
	return browser, nil
}

func (a *attachAllocator) Wait()          { a.wg.Wait() }
func (a *attachAllocator) Attaches() bool { return true }

// modifyWebSocketURL is the address resolution of chromedp's remote allocator (remote/util.go,
// which keeps it unexported).  Since Chrome 66 a DevTools client must send the Host header as an IP
// address or "localhost", so a host name is resolved first; an address that does not name a browser
// endpoint is looked up from http://host:port/json/version.
func modifyWebSocketURL(ctx context.Context, wsURL string) (string, error) {
	lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if strings.Contains(wsURL, "/devtools/browser/") {
		return forceIP(lctx, wsURL)
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return "", err
	}
	u.Scheme = "http"
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", err
	}
	if host, err = resolveHost(lctx, host); err != nil {
		return "", err
	}
	u.Host = net.JoinHostPort(host, port)
	u.Path = "/json/version"
	req, err := http.NewRequestWithContext(lctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%s names no webSocketDebuggerUrl", u)
	}
	return result.WebSocketDebuggerURL, nil
}

func forceIP(ctx context.Context, wsURL string) (string, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return "", err
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", err
	}
	if host, err = resolveHost(ctx, host); err != nil {
		return "", err
	}
	u.Host = net.JoinHostPort(host, port)
	return u.String(), nil
}

func resolveHost(ctx context.Context, host string) (string, error) {
	if host == "localhost" || net.ParseIP(host) != nil {
		return host, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	return addrs[0].IP.String(), nil
}
