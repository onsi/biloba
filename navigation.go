package biloba

import (
	"net/http"

	"github.com/chromedp/chromedp"
)

/*
Navigate() causes this tab to navigate to the provided URL.  The spec fails if the response does not have status code 200

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) Navigate(url string) *Biloba {
	return b.NavigateWithStatus(url, http.StatusOK)
}

/*
NavigateWithStatus() causes this tab to navigate to the provided URL and asserts that the response has the provided status code.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) NavigateWithStatus(url string, status int) *Biloba {
	b.gt.Helper()
	resp, err := chromedp.RunResponse(b.Context, chromedp.Navigate(url))
	if err != nil {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
		return b
	}
	if resp != nil && status != int(resp.Status) {
		b.gt.Fatalf("failed to navigate to %s: expected status code %d, got %d", url, status, resp.Status)
		return b
	}
	return b
}

/*
Location() returns the location (i.e. url) of the current tab.
*/
func (b *Biloba) Location() string {
	var location string
	err := chromedp.Run(b.Context, chromedp.Location(&location))
	if err != nil {
		b.gt.Fatalf("Failed to fetch location:\n%s", err.Error())
		return ""
	}
	return location
}

/*
Title() returns the window title of the current tab.
*/
func (b *Biloba) Title() string {
	var title string
	err := chromedp.Run(b.Context, chromedp.Title(&title))
	if err != nil {
		b.gt.Fatalf("Failed to fetch title:\n%s", err.Error())
		return ""
	}
	return title
}
