package biloba

import (
	"net/http"

	"github.com/chromedp/chromedp"
)

func (b *Biloba) Navigate(url string) *Biloba {
	return b.NavigateWithStatus(url, http.StatusOK)
}

func (b *Biloba) NavigateWithStatus(url string, status int) *Biloba {
	b.gt.Helper()
	resp, err := chromedp.RunResponse(b.Context, chromedp.Navigate(url))
	if err != nil {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
		return b
	}
	if status != int(resp.Status) {
		b.gt.Fatalf("failed to navigate to %s: expected status code %d, got %d", url, status, resp.Status)
		return b
	}
	return b
}

func (b *Biloba) Location() string {
	var location string
	err := chromedp.Run(b.Context, chromedp.Location(&location))
	if err != nil {
		b.gt.Fatalf("Failed to fetch location:\n%s", err.Error())
		return ""
	}
	return location
}

func (b *Biloba) Title() string {
	var title string
	err := chromedp.Run(b.Context, chromedp.Title(&title))
	if err != nil {
		b.gt.Fatalf("Failed to fetch title:\n%s", err.Error())
		return ""
	}
	return title
}
