package biloba

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/onsi/gomega/format"
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
Find returns the first download matching the passed-in DownloadQuery (see [Biloba.DownloadMatching]), or nil if none match:

	dl := b.AllCompleteDownloads().Find(b.DownloadMatching("report.csv"))

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d Downloads) Find(query *DownloadQuery) *Download {
	for _, dl := range d {
		if query.matches(dl) {
			return dl
		}
	}
	return nil
}

/*
Filter returns a Downloads slice containing all downloads matching the passed-in DownloadQuery (see [Biloba.DownloadMatching])

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (d Downloads) Filter(query *DownloadQuery) Downloads {
	out := Downloads{}
	for _, dl := range d {
		if query.matches(dl) {
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
	b.guardConfig("AllDownloads")
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
	b.guardConfig("AllCompleteDownloads")
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
	for _, tab := range b.AllSpawnedTabs() {
		if tab.hasActiveDownloads() {
			return true
		}
	}
	return false
}

/*
DownloadQuery is a chainable query over a tab's downloads.  A single value plays two roles:

  - a Gomega matcher you assert against a tab - read it as [Biloba.HaveDownloaded] (does this tab have a matching complete download?), and
  - a predicate you pass to [Downloads.Find] / [Downloads.Filter] - read it as [Biloba.DownloadMatching] (does this one download match?).

Unlike requests and cookies, a download has no single primary key, so all of its dimensions are refinements: chain WithURL and WithContent (and pass an optional filename to the constructor).  Every refinement applies to the same download.

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
type DownloadQuery struct {
	filenameMatcher types.GomegaMatcher
	urlMatcher      types.GomegaMatcher
	contentMatcher  types.GomegaMatcher
	observed        Downloads
}

/*
DownloadMatching() returns a [DownloadQuery].  Pass an optional filename (a string for an exact match, or a Gomega matcher) to key on the suggested download filename; omit it to start from "any download" and refine with WithURL/WithContent.  Use this spelling when the query reads as a predicate - i.e. when handing it to [Downloads.Find] / [Downloads.Filter]:

	dl := b.AllCompleteDownloads().Find(b.DownloadMatching("report.csv"))
	dl := b.AllCompleteDownloads().Find(b.DownloadMatching().WithContent([]byte("a,b,c")))

When you're asserting against a tab, the [Biloba.HaveDownloaded] alias reads more naturally.  The two are interchangeable - they return the same query.

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) DownloadMatching(filename ...any) *DownloadQuery {
	q := &DownloadQuery{}
	if len(filename) > 0 {
		q.filenameMatcher = matcherOrEqual(filename[0])
	}
	return q
}

/*
HaveDownloaded() is an alias for [Biloba.DownloadMatching] that reads as an assertion.  Apply the returned [DownloadQuery] to the tab so you can poll until a matching complete download has arrived:

	Eventually(b).Should(b.HaveDownloaded("report.csv"))
	Eventually(b).Should(b.HaveDownloaded("report.csv").WithContent([]byte("a,b,c")))
	Eventually(b).Should(b.HaveDownloaded().WithContent(ContainSubstring("totals")))

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (b *Biloba) HaveDownloaded(filename ...any) *DownloadQuery {
	return b.DownloadMatching(filename...)
}

/*
WithURL() refines the [DownloadQuery] to also require the download's URL to match.  url may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (q *DownloadQuery) WithURL(url any) *DownloadQuery {
	out := *q
	out.urlMatcher = matcherOrEqual(url)
	return &out
}

/*
WithContent() refines the [DownloadQuery] to also require the download's content to match.  content may be a []byte slice (exact match) or a Gomega matcher.  Only complete downloads have content, so a download that is still in progress never matches a content refinement.

Read https://onsi.github.io/biloba/#managing-downloads to learn more about managing Downloads in Biloba
*/
func (q *DownloadQuery) WithContent(content any) *DownloadQuery {
	out := *q
	out.contentMatcher = matcherOrEqual(content)
	return &out
}

// matches is the predicate role: does this single download satisfy every constraint?
func (q *DownloadQuery) matches(dl *Download) bool {
	if q.filenameMatcher != nil {
		if match, _ := q.filenameMatcher.Match(dl.Filename); !match {
			return false
		}
	}
	if q.urlMatcher != nil {
		if match, _ := q.urlMatcher.Match(dl.URL); !match {
			return false
		}
	}
	if q.contentMatcher != nil {
		if match, _ := q.contentMatcher.Match(dl.Content()); !match {
			return false
		}
	}
	return true
}

// Match is the Gomega matcher role: does the tab have any complete download that matches?
func (q *DownloadQuery) Match(actual any) (bool, error) {
	tab, ok := actual.(*Biloba)
	if !ok {
		return false, fmt.Errorf("HaveDownloaded must be passed a Biloba tab.  Got:\n%s", format.Object(actual, 1))
	}
	q.observed = tab.AllCompleteDownloads()
	return q.observed.Find(q) != nil, nil
}

func (q *DownloadQuery) description() string {
	clauses := []string{}
	if q.filenameMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("Filename matching %s", q.filenameMatcher.FailureMessage("")))
	}
	if q.urlMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("URL matching %s", q.urlMatcher.FailureMessage("")))
	}
	if q.contentMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("Content matching %s", q.contentMatcher.FailureMessage("")))
	}
	if len(clauses) == 0 {
		return "have a complete download"
	}
	return normalizeWhitespace("have a complete download with " + strings.Join(clauses, "\nand "))
}

func (q *DownloadQuery) presentDownloads() string {
	if len(q.observed) == 0 {
		return "The tab has no complete downloads."
	}
	out := &strings.Builder{}
	out.WriteString("The tab's complete downloads were:")
	for _, dl := range q.observed {
		fmt.Fprintf(out, "\n%s (%s)", dl.Filename, dl.URL)
	}
	return out.String()
}

func (q *DownloadQuery) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab to %s.\n%s", q.description(), q.presentDownloads())
}

func (q *DownloadQuery) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab not to %s, but it did.", q.description())
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
