package biloba_test

import (
	"net/url"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConnectToChrome", func() {
	It("finds the browser's endpoint itself when handed only its debugging address", func() {
		endpoint, err := url.Parse(b.ChromeConnection.WebSocketURL)
		Ω(err).ShouldNot(HaveOccurred())
		connection := b.ChromeConnection
		connection.WebSocketURL = "http://" + endpoint.Host + "/"

		tab := biloba.ConnectToChrome(gt, biloba.BilobaConfigWithChromeConnection(connection))
		Ω(tab).ShouldNot(BeNil())
		tab.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(tab.Exist())
	})
})
