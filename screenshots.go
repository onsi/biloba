package biloba

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/context"
)

/*
CaptureScreenshot() returns a full screenshot of the current tab as a []byte array (you can decode it with the image package)
*/
func (b *Biloba) CaptureScreenshot() []byte {
	var img []byte
	err := chromedp.Run(b.Context, chromedp.FullScreenshot(&img, 100))
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	return img
}

/*
CaptureImgCatScreenshot() returns a full screenshot of the current tab as an iTerm2 imgcat-compatible string.  Simply print it out to see images on your terminal.
*/
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

func (b *Biloba) safeAllTabScreenshots(width int, height int) []tabScreenshot {
	out := []tabScreenshot{}
	for _, tab := range b.AllTabs() {
		ctx, cancel := context.WithTimeout(tab.Context, time.Second)
		defer cancel()

		var originalWidth, originalHeight int
		if width > 0 && height > 0 {
			originalWidth, originalHeight = b.WindowSize()
			err := chromedp.Run(ctx, chromedp.EmulateViewport(int64(width), int64(height)))
			if err != nil {
				out = append(out, tabScreenshot{failure: fmt.Sprintf("failed to set window size: %s", err.Error())})
				continue
			}
		}
		var img []byte
		var title string
		err := chromedp.Run(ctx,
			chromedp.Title(&title),
			chromedp.FullScreenshot(&img, 100),
		)
		if width > 0 && height > 0 {
			err := chromedp.Run(ctx, chromedp.EmulateViewport(int64(originalWidth), int64(originalHeight), chromedp.EmulatePortrait))
			if err != nil {
				out = append(out, tabScreenshot{failure: fmt.Sprintf("failed to reset window size: %s", err.Error())})
				continue
			}
		}
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
