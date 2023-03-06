package biloba_test

import (
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Downloading Files", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/downloads.html")
		Eventually("#download").Should(b.Exist())
	})

	It("can download files and make them available", func() {
		b.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(1))
		dl := b.FindCompleteDownload(b.DownloadWithFilename("filename.txt"))
		Ω(string(dl.Content())).Should(Equal("My Content"))
	})

	It("can handle multiple files", func() {
		b.Click("#download")
		b.SetValue("#content", "Some new content")
		b.SetValue("#filename", "new-file.txt")
		b.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(2))

		dl := b.FindCompleteDownload(b.DownloadWithFilename("filename.txt"))
		Ω(string(dl.Content())).Should(Equal("My Content"))

		dl = b.FindCompleteDownload(b.DownloadWithFilename("new-file.txt"))
		Ω(string(dl.Content())).Should(Equal("Some new content"))
	})

	It("can handle many downloads", func(ctx SpecContext) {
		t := time.Now()
		for i := 1; i < 15; i++ {
			b.Click("#download")
			Eventually(ctx, b.AllCompleteDownloads).Should(HaveLen(i))
		}
		Ω(time.Since(t)).Should(BeNumerically(">", time.Second), "we should have waited around a bit for chrome's 10-download/second limit to complete")
	}, SpecTimeout(3*time.Second))

	It("can handle many downloads (simulating multiple tabs)", func(ctx SpecContext) {
		tab := b.NewTab().Navigate(fixtureServer + "/downloads.html")
		Eventually("#download").Should(tab.Exist())

		b.Click("#download")
		b.Click("#download")
		b.Click("#download")
		Eventually(ctx, b.AllCompleteDownloads).Should(HaveLen(3))

		for i := 1; i <= 11; i++ {
			t := time.Now()
			tab.Click("#download")
			if i <= 10 {
				//these are fast
				Ω(time.Since(t)).Should(BeNumerically("<", 500*time.Millisecond))
			} else {
				//the 11th one is slow
				Ω(time.Since(t)).Should(BeNumerically(">", 500*time.Millisecond))
			}
			Eventually(ctx, tab.AllCompleteDownloads).Should(HaveLen(i))
		}
	}, SpecTimeout(3*time.Second))

	It("can handle many downloads (when the downloads come from a tab spawned from the root tab)", func(ctx SpecContext) {
		b.Click(b.XPath("a").WithTextContains("Open in New Tab"))
		Eventually(b.AllSpawnedTabs).Should(HaveLen(1))
		newTab := b.FindSpawnedTab(b.TabWithTitle("Downloads Testpage"))

		b.Click("#download")
		b.Click("#download")
		b.Click("#download")
		Eventually(ctx, b.AllCompleteDownloads).Should(HaveLen(3))

		for i := 1; i <= 11; i++ {
			t := time.Now()
			newTab.Click("#download")
			if i <= 10 {
				//these are fast
				Ω(time.Since(t)).Should(BeNumerically("<", 500*time.Millisecond))
			} else {
				//the 11th one is slow
				Ω(time.Since(t)).Should(BeNumerically(">", 500*time.Millisecond))
			}
			Eventually(ctx, newTab.AllCompleteDownloads).Should(HaveLen(i))
		}
	}, SpecTimeout(3*time.Second))

	It("can handle many downloads (simulating multiple processes)", func(ctx SpecContext) {
		gOtherProcess := biloba.ConnectToChrome(gt).Navigate(fixtureServer + "/downloads.html")
		Eventually("#download").Should(gOtherProcess.Exist())

		b.Click("#download")
		b.Click("#download")
		b.Click("#download")
		Eventually(ctx, b.AllCompleteDownloads).Should(HaveLen(3))

		for i := 1; i <= 11; i++ {
			t := time.Now()
			gOtherProcess.Click("#download")
			if i <= 10 {
				//these are fast
				Ω(time.Since(t)).Should(BeNumerically("<", 500*time.Millisecond))
			} else {
				//the 11th one is slow
				Ω(time.Since(t)).Should(BeNumerically(">", 500*time.Millisecond))
			}
			Eventually(ctx, gOtherProcess.AllCompleteDownloads).Should(HaveLen(i))
		}
	}, SpecTimeout(3*time.Second))

	Describe("finding files and matching files", func() {
		BeforeEach(func() {
			b.Click("#download")
			b.SetValue("#content", "Some new content")
			b.SetValue("#filename", "new-file.txt")
			b.Click("#download")
			b.SetValue("#content", "Yet more content")
			b.SetValue("#filename", "yet-file.txt")
			b.Click("#download")
		})

		It("can find files by filename", func() {
			Eventually(b).Should(b.HaveCompleteDownload(b.DownloadWithFilename("yet-file.txt")))
		})

		It("can find files by content", func() {
			Eventually(b).Should(b.HaveCompleteDownload(b.DownloadWithContent([]byte("Yet more content"))))
		})
	})

	It("works when multiple tabs are in play", func() {
		By("ensuring we can download from the root tab")
		b.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(1))
		dl := b.FindCompleteDownload(b.DownloadWithFilename("filename.txt"))
		Ω(string(dl.Content())).Should(Equal("My Content"))

		By("ensuring we can download from a new tab")
		tab := b.NewTab().Navigate(fixtureServer + "/downloads.html")
		Eventually("#download").Should(tab.Exist())
		Ω(tab.AllDownloads()).Should(HaveLen(0))
		tab.Click("#download")
		Eventually(tab.AllCompleteDownloads).Should(HaveLen(1))

		By("opening and closing another new tab")
		otherTab := b.NewTab()
		Ω(otherTab.Close()).Should(Succeed())

		By("ensuring we can still download things on the root and new tab")
		b.SetValue("#content", "Some new content")
		b.SetValue("#filename", "new-file.txt")
		b.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(2))
		dl = b.FindCompleteDownload(b.DownloadWithFilename("new-file.txt"))
		Ω(string(dl.Content())).Should(Equal("Some new content"))
		tab.Click("#download")
		Eventually(tab.AllCompleteDownloads).Should(HaveLen(2))

		By("spawning then closing a new tab (this will have the same BrowserContextID as our root tab)")
		b.Click(b.XPath("a").WithTextContains("Open in New Tab"))
		Eventually(b.AllSpawnedTabs).Should(HaveLen(1))
		Eventually(b.FindSpawnedTab(b.TabWithTitle("Downloads Testpage")).Close).Should(Succeed()) // only closes if any downloads are completed
		Eventually(b.AllSpawnedTabs).Should(HaveLen(0))

		By("ensuring that the closed spawned tab does not mess up the download config for the root tab")
		b.Click("#download")
		tab.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(3))
		Eventually(tab.AllCompleteDownloads).Should(HaveLen(3))

		By("spawning then closing a new tab (this time from a different tab)")
		tab.Click(tab.XPath("a").WithTextContains("Open in New Tab"))
		Eventually(tab.AllSpawnedTabs).Should(HaveLen(1))
		Eventually(tab.FindSpawnedTab(tab.TabWithTitle("Downloads Testpage")).Close).Should(Succeed()) // only
		Eventually(tab.AllSpawnedTabs).Should(HaveLen(0))

		By("ensuring that the closed spawned tab does not mess up the download config for the root tab")
		b.Click("#download")
		tab.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(4))
		Eventually(tab.AllCompleteDownloads).Should(HaveLen(4))
	})
})
