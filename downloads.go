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

/*
Download represents a downloaded file

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
type Download struct {
	GUID        string
	URL         string
	Filename    string
	complete    bool
	cancelled   bool
	fetched     bool
	downloadDir string

	content []byte
	lock    *sync.Mutex
}

/*
IsComplete() returns true if the download is complete

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d *Download) IsComplete() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.complete
}

/*
IsComplete() returns true if the download was cancelled

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d *Download) IsCancelled() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.cancelled
}

/*
IsActive() returns true if the download is still in progress

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d *Download) IsActive() bool {
	d.lock.Lock()
	defer d.lock.Unlock()
	return !d.cancelled && !d.complete
}

/*
Content() returns the contents of the file that was downloaded

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
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

const _CHROME_DOWNLOAD_LIMIT = 10

func minDt(a, b time.Duration) time.Duration {
	if a < b {
		return a
	} else {
		return b
	}
}

func (b *Biloba) blockIfNecessaryToEnsureSuccessfulDownloads() {
	b.lock.Lock()
	if len(b.downloadHistory) < _CHROME_DOWNLOAD_LIMIT {
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
		if active < _CHROME_DOWNLOAD_LIMIT {
			return
		}
		time.Sleep(waitingTime)
	}
}

/*
Downloads represents a slice of *Download
*/
type Downloads []*Download

/*
Find returns the first download that matches DownloadFilter, or nil if none match

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d Downloads) Find(f DownloadFilter) *Download {
	for _, dl := range d {
		if f(dl) {
			return dl
		}
	}
	return nil
}

/*
Filter returns a Downloads slice containing all matching *Download objects

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d Downloads) Filter(f DownloadFilter) Downloads {
	out := Downloads{}
	for _, dl := range d {
		if f(dl) {
			out = append(out, dl)
		}
	}
	return out
}

/*
AllDownloads() returns all downloads associated with this tab

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) AllDownloads() Downloads {
	b.lock.Lock()
	defer b.lock.Unlock()
	out := Downloads{}
	for _, dl := range b.downloads {
		out = append(out, dl)
	}
	return out
}

/*
AllCompleteDownloads() returns all downloads associated with this tab that are complete

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) AllCompleteDownloads() Downloads {
	b.lock.Lock()
	defer b.lock.Unlock()
	out := Downloads{}
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
	for _, tab := range b.AllTabs().Filter(b.isSiblingTab) {
		if tab.hasActiveDownloads() {
			return true
		}
	}
	return false
}

/*
DownloadFilter is used to filter downloads

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
type DownloadFilter func(*Download) bool

/*
HaveCompleteDownload() is a matcher that passes if this tab has a complete download that satisfies the passed in DownloadFilter

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) HaveCompleteDownload(f DownloadFilter) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.AllCompleteDownloads().Find(f) != nil, nil
	}).WithTemplate("Did not find download satisfying requirements.")
}

/*
DownloadWithURL() returns a filter that selects Downloads with a matching url.  url may be a string (exact match) or Gomega matcher

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) DownloadWithURL(url any) DownloadFilter {
	m := matcherOrEqual(url)
	return func(dl *Download) bool {
		match, _ := m.Match(dl.URL)
		return match
	}
}

/*
DownloadWithFilename() returns a filter that selects Downloads with a matching filename - this is the filename suggested to the browser when the download commences.  filename may be a string (exact match) or Gomega matcher

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) DownloadWithFilename(filename any) DownloadFilter {
	m := matcherOrEqual(filename)
	return func(dl *Download) bool {
		match, _ := m.Match(dl.Filename)
		return match
	}
}

/*
DownloadWithContent() returns a filter that selects Downloads with matching content (a []byte slice).  content may be a []byte slice (exact match) or Gomega matcher

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) DownloadWithContent(content any) DownloadFilter {
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
		dl.cancelled = true
		dl.lock.Unlock()
		b.downloadHistory[ev.GUID] = time.Now()
	case browser.DownloadProgressStateCompleted:
		dl.lock.Lock()
		dl.complete = true
		dl.lock.Unlock()
		b.downloadHistory[ev.GUID] = time.Now()
	}
}
