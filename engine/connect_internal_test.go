package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// modifyWebSocketURL is the address resolution the Go client's ConnectToChrome kept from chromedp's
// remote allocator, which leaves it unexported.  These pin each rule of it without a browser.
var _ = ginkgo.Describe("modifyWebSocketURL", ginkgo.Label("no-browser"), func() {
	ginkgo.It("uses a browser endpoint on an IP address or localhost as given", func() {
		for _, address := range []string{"ws://127.0.0.1:9222/devtools/browser/abc", "ws://localhost:9222/devtools/browser/abc", "ws://[::1]:9222/devtools/browser/abc"} {
			gomega.Expect(modifyWebSocketURL(context.Background(), address)).To(gomega.Equal(address))
		}
	})

	ginkgo.It("resolves any other host name, since Chrome only accepts an IP or localhost as the Host", func() {
		// .invalid never resolves (RFC 2606), so the lookup is visible as the failure
		_, err := modifyWebSocketURL(context.Background(), "ws://biloba.invalid:9222/devtools/browser/abc")
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("looks an address that names no browser endpoint up from /json/version", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gomega.Expect(r.URL.Path).To(gomega.Equal("/json/version"))
			_, _ = w.Write([]byte(`{"Browser":"HeadlessChrome","webSocketDebuggerUrl":"ws://127.0.0.1:1234/devtools/browser/xyz"}`))
		}))
		ginkgo.DeferCleanup(server.Close)
		port := server.URL[strings.LastIndex(server.URL, ":")+1:]

		for _, address := range []string{server.URL, "http://127.0.0.1:" + port + "/", "ws://127.0.0.1:" + port + "/"} {
			gomega.Expect(modifyWebSocketURL(context.Background(), address)).To(gomega.Equal("ws://127.0.0.1:1234/devtools/browser/xyz"), address)
		}
	})
})
