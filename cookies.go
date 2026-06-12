package biloba

import (
	"context"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

/*
Cookie represents a browser cookie.  It is used both to set cookies (via [Biloba.SetCookie]) and to return cookies (via [Biloba.GetCookies]).

When setting a cookie only Name and Value are required.  If Domain and Path are not provided Chrome derives them from the current URL - so make sure you have navigated to an origin before setting a cookie (a cookie cannot be associated with about:blank).  Pass a non-zero Expires to set a persistent cookie; leave it as the zero time.Time to set a session cookie.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite string

	//Session is only ever populated by GetCookies and is true when the cookie has no expiration
	Session bool
}

/*
SetCookie() sets one or more cookies on this tab's BrowserContextID.  At minimum each cookie must have a Name and Value:

	b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})

Cookies are scoped to the tab's isolated BrowserContextID, so cookies set on one tab will not leak into other tabs.

Unless you set a Domain explicitly, the cookie is attached to the tab's current location (an explicit Path still applies).  A cookie therefore needs the tab to be on a real origin: if there is no Domain and the tab is on about:blank (or no page), SetCookie fails the spec with a clear message rather than letting Chrome silently drop the cookie - navigate to a real URL first, or set the Domain explicitly.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) SetCookie(cookies ...Cookie) {
	b.gt.Helper()
	location := b.Location()
	params := make([]*network.CookieParam, len(cookies))
	for i, cookie := range cookies {
		param := &network.CookieParam{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HTTPOnly,
			SameSite: network.CookieSameSite(cookie.SameSite),
		}
		// A cookie must be tied to an origin via either an explicit Domain or a URL. When no
		// Domain is given we fall back to the tab's current location as the URL (an explicit
		// Path, if any, still overrides the path the URL would imply). We fail loudly when
		// there's no Domain and no usable origin, rather than letting Chrome silently drop the
		// cookie - a sharp edge that otherwise only surfaces later when the cookie isn't there.
		if cookie.Domain == "" {
			if !isUsableCookieOrigin(location) {
				name := cookie.Name
				if name == "" {
					name = "<unnamed>"
				}
				b.gt.Fatalf("Failed to set cookie %q: a cookie needs an origin, but it has no Domain and the tab's current location (%q) is not one a cookie can attach to.\nNavigate the tab to a real URL before calling SetCookie, or set the cookie's Domain explicitly.", name, location)
				return
			}
			param.URL = location
		}
		if !cookie.Expires.IsZero() {
			expires := cdp.TimeSinceEpoch(cookie.Expires)
			param.Expires = &expires
		}
		params[i] = param
	}
	err := b.runWithBrowserExecutor(func(ctx context.Context) error {
		return storage.SetCookies(params).WithBrowserContextID(b.browserContextID).Do(ctx)
	})
	if err != nil {
		b.gt.Fatalf("Failed to set cookies:\n%s", err.Error())
	}
}

// isUsableCookieOrigin reports whether location is a URL a cookie can attach to. about:blank
// (and other opaque/empty origins) cannot hold cookies, which is the common reason a SetCookie
// silently does nothing.
func isUsableCookieOrigin(location string) bool {
	return location != "" && !strings.HasPrefix(location, "about:")
}

/*
Cookies represents a slice of Cookie.  Search it with Find/Filter and a [Biloba.CookieMatching] query.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
type Cookies []Cookie

/*
Find returns the first cookie matching the passed-in CookieMatcher (see [Biloba.CookieMatching]), or the zero Cookie if none match.  The returned bool reports whether a match was found:

	cookie, ok := b.GetCookies().Find(b.CookieMatching("session").WithPath("/"))

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (c Cookies) Find(query *CookieMatcher) (Cookie, bool) {
	for _, cookie := range c {
		if query.matches(cookie) {
			return cookie, true
		}
	}
	return Cookie{}, false
}

/*
Filter returns all cookies matching the passed-in CookieMatcher (see [Biloba.CookieMatching])

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (c Cookies) Filter(query *CookieMatcher) Cookies {
	out := Cookies{}
	for _, cookie := range c {
		if query.matches(cookie) {
			out = append(out, cookie)
		}
	}
	return out
}

/*
GetCookies() returns all the cookies associated with this tab's BrowserContextID:

	cookies := b.GetCookies()

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) GetCookies() Cookies {
	b.gt.Helper()
	var networkCookies []*network.Cookie
	err := b.runWithBrowserExecutor(func(ctx context.Context) error {
		var err error
		networkCookies, err = storage.GetCookies().WithBrowserContextID(b.browserContextID).Do(ctx)
		return err
	})
	if err != nil {
		b.gt.Fatalf("Failed to get cookies:\n%s", err.Error())
		return nil
	}
	cookies := make(Cookies, len(networkCookies))
	for i, c := range networkCookies {
		cookie := Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: string(c.SameSite),
			Session:  c.Session,
		}
		if !c.Session && c.Expires > 0 {
			cookie.Expires = time.Unix(int64(c.Expires), 0)
		}
		cookies[i] = cookie
	}
	return cookies
}

/*
ClearCookies() clears all the cookies associated with this tab's BrowserContextID.  This is a common DeferCleanup to ensure cookie state does not leak between specs:

	DeferCleanup(b.ClearCookies)

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) ClearCookies() {
	b.gt.Helper()
	err := b.runWithBrowserExecutor(func(ctx context.Context) error {
		return storage.ClearCookies().WithBrowserContextID(b.browserContextID).Do(ctx)
	})
	if err != nil {
		b.gt.Fatalf("Failed to clear cookies:\n%s", err.Error())
	}
}

// resetBrowsingState clears cookies and web storage so the reusable root tab starts each
// spec from a clean slate. It is called from Prepare() and is best-effort: errors are
// ignored rather than failing the spec, since this runs on the critical between-specs path.
//
// Cookies are cleared at the browser-context level, so this is origin-agnostic. Local and
// session storage are origin-scoped, so we clear the current origin's storage via JS while
// the root tab is still on the previous spec's page (Prepare navigates to about:blank
// afterwards). The try/catch makes the storage clear a no-op on about:blank and other
// opaque origins, where accessing window.localStorage throws.
func (b *Biloba) resetBrowsingState() {
	b.runWithBrowserExecutor(func(ctx context.Context) error {
		return storage.ClearCookies().WithBrowserContextID(b.browserContextID).Do(ctx)
	})
	b.RunErr(`try { window.localStorage.clear(); window.sessionStorage.clear(); } catch (e) {}`)
}

// runWithBrowserExecutor runs f against the browser-level CDP executor (as opposed to the
// target/tab executor). The storage cookie commands are browser-scoped and take a
// BrowserContextID, so they must be dispatched on the Browser connection.
func (b *Biloba) runWithBrowserExecutor(f func(ctx context.Context) error) error {
	return chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		return f(cdp.WithExecutor(ctx, c.Browser))
	}))
}
