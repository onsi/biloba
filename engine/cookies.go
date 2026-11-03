package engine

import (
	"context"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

func IsUsableCookieOrigin(location string) bool {
	return location != "" && !strings.HasPrefix(location, "about:")
}

// SetCookiesContext applies cookies to one isolated browser context using the
// browser-level executor associated with targetCtx.
func SetCookiesContext(targetCtx context.Context, browserContextID cdp.BrowserContextID, location string, cookies []Cookie) error {
	params := make([]*network.CookieParam, len(cookies))
	for index, cookie := range cookies {
		param := &network.CookieParam{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: network.CookieSameSite(cookie.SameSite),
		}
		if cookie.Domain == "" {
			if !IsUsableCookieOrigin(location) {
				return &Error{Code: CodeInvalidArgument, Operation: "set cookies", Message: "cookie needs a Domain or a session navigated to an HTTP origin", Observed: cookie.Name}
			}
			param.URL = location
		}
		if !cookie.Expires.IsZero() {
			// whole seconds, as cdproto sent a TimeSinceEpoch before it became a plain number
			param.Expires = cdp.TimeSinceEpoch(cookie.Expires.Unix())
		}
		params[index] = param
	}
	return withBrowserExecutor(targetCtx, func(browserCtx context.Context, browser cdp.Session) error {
		_, err := cdp.Call(browserCtx, browser, storage.SetCookies, storage.SetCookiesParams{Cookies: params, BrowserContextID: browserContextID})
		return err
	})
}

// GetCookiesContext reads every cookie in one isolated browser context.
func GetCookiesContext(targetCtx context.Context, browserContextID cdp.BrowserContextID) ([]Cookie, error) {
	var stored []*network.Cookie
	err := withBrowserExecutor(targetCtx, func(browserCtx context.Context, browser cdp.Session) error {
		read, readErr := cdp.Call(browserCtx, browser, storage.GetCookies, storage.GetCookiesParams{BrowserContextID: browserContextID})
		stored = read.Cookies
		return readErr
	})
	if err != nil {
		return nil, err
	}
	cookies := make([]Cookie, len(stored))
	for index, cookie := range stored {
		cookies[index] = Cookie{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: string(cookie.SameSite),
			Session: cookie.Session,
		}
		// Chrome reports the expiry as Unix seconds, and -1 for a session cookie.
		if !cookie.Session && cookie.Expires > 0 {
			cookies[index].Expires = time.Unix(int64(cookie.Expires), 0)
		}
	}
	return cookies, nil
}

func ClearCookiesContext(targetCtx context.Context, browserContextID cdp.BrowserContextID) error {
	return withBrowserExecutor(targetCtx, func(browserCtx context.Context, browser cdp.Session) error {
		_, err := cdp.Call(browserCtx, browser, storage.ClearCookies, storage.ClearCookiesParams{BrowserContextID: browserContextID})
		return err
	})
}

// withBrowserExecutor runs one browser-scoped CDP command (the storage cookie commands take a
// BrowserContextID and so must be dispatched on the Browser connection rather than the target's).
// It goes through chromedp.Do - rather than reaching for chromedp.FromContext(...).Browser
// directly - because Do is what initializes the browser if this is the context's first command,
// and what turns a context that is not a chromedp context into an ErrInvalidContext *error*
// instead of a nil-pointer dereference: these are library calls that report failures, never panic.
func withBrowserExecutor(targetCtx context.Context, run func(context.Context, cdp.Session) error) error {
	return chromedp.Do(targetCtx, chromedp.Func(func(runCtx context.Context, _ *chromedp.Target) error {
		return run(runCtx, chromedp.FromContext(runCtx).Browser)
	}))
}
