package biloba

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/context"
)

func (b *Biloba) SetWindowSize(width, height int, opts ...chromedp.EmulateViewportOption) {
	originalWidth, originalHeight := b.WindowSize()
	b.gt.Helper()
	err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(width), int64(height), opts...))
	if err != nil {
		b.gt.Fatalf("failed to set window size: %s", err.Error())
	}

	b.gt.DeferCleanup(func() {
		err := chromedp.Run(b.Context, chromedp.EmulateViewport(int64(originalWidth), int64(originalHeight), chromedp.EmulatePortrait))
		if err != nil {
			b.gt.Fatalf("failed to reset window size: %s", err.Error())
		}
	})
}

func (b *Biloba) WindowSize() (int, int) {
	b.gt.Helper()
	var dimensions []int
	b.Run(`[window.innerWidth, window.innerHeight]`, &dimensions)
	return dimensions[0], dimensions[1]
}

func (b *Biloba) CaptureScreenshot() []byte {
	var img []byte
	err := chromedp.Run(b.Context, chromedp.CaptureScreenshot(&img))
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	return img
}

func (b *Biloba) CaptureImgcatScreenshot() string {
	return b.asImgCat(b.CaptureScreenshot())
}

func (b *Biloba) asImgCat(img []byte) string {
	buf := &bytes.Buffer{}
	buf.WriteString("\033]1337;File=;inline=1:")
	encoder := base64.NewEncoder(base64.StdEncoding, buf)
	_, err := encoder.Write(img)
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	encoder.Close()
	buf.WriteString("\033\\")

	return string(buf.Bytes())
}

type tabScreenshot struct {
	title            string
	imgcatScreenshot string
	failure          string
}

func (b *Biloba) safeAllTabScreenshots() []tabScreenshot {
	out := []tabScreenshot{}
	for _, tab := range b.AllTabs() {
		ctx, cancel := context.WithTimeout(tab.Context, time.Second)
		defer cancel()
		var img []byte
		var title string
		err := chromedp.Run(ctx,
			chromedp.Title(&title),
			chromedp.CaptureScreenshot(&img),
		)
		if ctx.Err() != nil {
			out = append(out, tabScreenshot{failure: "Timed out attempting to fetch screenshot for tab"})
			continue
		} else if err != nil {
			out = append(out, tabScreenshot{failure: fmt.Sprintf("Failed to fetch screenshot for tab: %s", err.Error())})
			continue
		}
		out = append(out, tabScreenshot{
			title:            title,
			imgcatScreenshot: b.asImgCat(img),
		})
	}
	return out
}
