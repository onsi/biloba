package biloba

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

type Download struct {
	GUID        string
	URL         string
	Filename    string
	complete    bool
	canceled    bool
	fetched     bool
	downloadDir string

	content []byte
	lock    *sync.Mutex
}

func (d *Download) IsComplete() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.complete
}

func (d *Download) IsCanceled() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.canceled
}

func (d *Download) IsActive() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return !d.canceled && !d.complete
}

func (d *Download) Content() []byte {
	d.lock.Lock()
	defer d.lock.Unlock()
	if !d.complete {
		return nil
	}
	if !d.fetched {
		var err error
		d.content, err = os.ReadFile(filepath.Join(d.downloadDir, d.GUID))
		if err != nil {
			fmt.Println(err)
		}
		d.fetched = true
	}
	return d.content
}

const CHROME_DOWNLOAD_LIMIT = 10

func minDt(a, b time.Duration) time.Duration {
	if a < b {
		return a
	} else {
		return b
	}
}

func (b *Biloba) blockIfNecessaryToEnsureSuccessfulDownloads() {
	b.lock.Lock()
	if len(b.downloadHistory) < CHROME_DOWNLOAD_LIMIT {
		b.lock.Unlock()
		return
	}
	b.lock.Unlock()
	for {
		active := 0
		guidsToDelete := []string{}
		waitingTime := time.Second
		b.lock.Lock()
		for guid, t := range b.downloadHistory {
			if t.IsZero() {
				active += 1
			} else if dt := time.Since(t); dt < time.Second {
				active += 1
				waitingTime = minDt(time.Second-dt, waitingTime)
			} else {
				guidsToDelete = append(guidsToDelete, guid)
			}
		}
		for _, guid := range guidsToDelete {
			delete(b.downloadHistory, guid)
		}
		b.lock.Unlock()
		if active < CHROME_DOWNLOAD_LIMIT {
			return
		}
		time.Sleep(waitingTime)
	}
}

func (b *Biloba) AllDownloads() []*Download {
	b.lock.Lock()
	defer b.lock.Unlock()
	out := []*Download{}
	for _, dl := range b.downloads {
		out = append(out, dl)
	}
	return out
}

func (b *Biloba) AllCompleteDownloads() []*Download {
	b.lock.Lock()
	defer b.lock.Unlock()
	out := []*Download{}
	for _, dl := range b.downloads {
		if dl.IsComplete() {
			out = append(out, dl)
		}
	}
	return out
}

func (b *Biloba) hasActiveDownloads() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	for _, dl := range b.downloads {
		if dl.IsActive() {
			return true
		}
	}
	return false
}

func (b *Biloba) activeDownloadsShouldBlockTabFromClosing(closingTab *Biloba) bool {
	closingTabBrowserId := closingTab.browserContextID
	for _, tab := range b.AllTabs() {
		if tab == closingTab {
			continue
		}
		if tab.browserContextID != closingTabBrowserId {
			continue
		}
		if tab.hasActiveDownloads() {
			return true
		}
	}
	return false
}

func (b *Biloba) HaveCompleteDownload(f func(*Download) bool) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.FindCompleteDownload(f) != nil, nil
	}).WithTemplate("Did not find download satisfying requirements.")
}

func (b *Biloba) FindCompleteDownload(f func(*Download) bool) *Download {
	for _, dl := range b.AllCompleteDownloads() {
		if f(dl) {
			return dl
		}
	}
	return nil
}

func (b *Biloba) DownloadWithURL(url any) func(*Download) bool {
	m := matcherOrEqual(url)
	return func(dl *Download) bool {
		match, _ := m.Match(dl.URL)
		return match
	}
}

func (b *Biloba) DownloadWithFilename(filename any) func(*Download) bool {
	m := matcherOrEqual(filename)
	return func(dl *Download) bool {
		match, _ := m.Match(dl.Filename)
		return match
	}
}

func (b *Biloba) DownloadWithContent(content any) func(*Download) bool {
	m := matcherOrEqual(content)
	return func(dl *Download) bool {
		match, _ := m.Match(dl.Content())
		return match
	}
}

func (b *Biloba) handleEventDownloadWillBegin(ev *browser.EventDownloadWillBegin) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.downloads[ev.GUID] = &Download{
		GUID:        ev.GUID,
		URL:         ev.URL,
		Filename:    ev.SuggestedFilename,
		downloadDir: b.root.downloadDir,
		lock:        &sync.Mutex{},
	}
	b.downloadHistory[ev.GUID] = time.Time{}
}

func (b *Biloba) handleEventDownloadProgress(ev *browser.EventDownloadProgress) {
	b.lock.Lock()
	dl := b.downloads[ev.GUID]
	defer b.lock.Unlock()

	switch ev.State {
	case browser.DownloadProgressStateCanceled:
		dl.lock.Lock()
		dl.canceled = true
		dl.lock.Unlock()
		b.downloadHistory[ev.GUID] = time.Now()
	case browser.DownloadProgressStateCompleted:
		dl.lock.Lock()
		dl.complete = true
		dl.lock.Unlock()
		b.downloadHistory[ev.GUID] = time.Now()
	}
}
