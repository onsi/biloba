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
				return &Error{Code: CodeActionFailed, Operation: "set cookies", Message: "cookie needs a Domain or a session navigated to an HTTP origin", Observed: cookie.Name}
			}
			param.URL = location
		}
		if !cookie.Expires.IsZero() {
			expires := cdp.TimeSinceEpoch(cookie.Expires)
			param.Expires = &expires
		}
		params[index] = param
	}
	return withBrowserExecutor(targetCtx, func(browserCtx context.Context) error {
		return storage.SetCookies(params).WithBrowserContextID(browserContextID).Do(browserCtx)
	})
}

// GetCookiesContext reads every cookie in one isolated browser context.
func GetCookiesContext(targetCtx context.Context, browserContextID cdp.BrowserContextID) ([]Cookie, error) {
	var stored []*network.Cookie
	err := withBrowserExecutor(targetCtx, func(browserCtx context.Context) error {
		var readErr error
		stored, readErr = storage.GetCookies().WithBrowserContextID(browserContextID).Do(browserCtx)
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
	return withBrowserExecutor(targetCtx, func(browserCtx context.Context) error {
		return storage.ClearCookies().WithBrowserContextID(browserContextID).Do(browserCtx)
	})
}

// withBrowserExecutor runs one browser-scoped CDP command (the storage cookie commands take a
// BrowserContextID and so must be dispatched on the Browser connection rather than the target's).
// It goes through chromedp.Run - rather than reaching for chromedp.FromContext(...).Browser
// directly - because Run is what initializes the browser if this is the context's first command,
// and what turns a context that is not a chromedp context into an ErrInvalidContext *error*
// instead of a nil-pointer dereference: these are library calls that report failures, never panic.
func withBrowserExecutor(targetCtx context.Context, run func(context.Context) error) error {
	return chromedp.Run(targetCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
		chrome := chromedp.FromContext(runCtx)
		return run(cdp.WithExecutor(runCtx, chrome.Browser))
	}))
}
