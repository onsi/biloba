package biloba_test

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

var _ = Describe("SpinUpChrome", Label("no-browser"), func() {
	It("retries once when Chrome prints no DevTools line, then fails and kills what is left", func() {
		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			Skip("leftover processes are only reaped on macOS and Linux")
		}
		DeferCleanup(biloba.SetLaunchAttemptTimeoutForTest(time.Second))
		dir := GinkgoT().TempDir()
		attempts := filepath.Join(dir, "attempts")
		script := filepath.Join(dir, "silent-chrome")
		// A child holding stdout that never prints, with every one of Chrome's arguments on its
		// command line: the shape of Chrome's fork child deadlocking before it execs.
		Ω(os.WriteFile(script, []byte("#!/bin/sh\n"+
			"for a in \"$@\"; do case \"$a\" in --user-data-dir=*) printf '%s\\n' \"${a#--user-data-dir=}\" >> '"+attempts+"';; esac; done\n"+
			"sh -c 'while :; do sleep 1 >/dev/null 2>&1; done' biloba-silent-child \"$@\" &\n"+
			"wait\n"), 0o755)).Should(Succeed())

		connection := biloba.SpinUpChrome(gt, biloba.HeadlessShellPath(script))

		Ω(connection.WebSocketURL).Should(BeEmpty())
		ExpectFailures(SatisfyAll(
			ContainSubstring("failed to spin up chrome"),
			ContainSubstring("two attempts, each with a fresh profile"),
			ContainSubstring("websocket url timeout reached after 1s"),
		))
		contents, err := os.ReadFile(attempts)
		Ω(err).ShouldNot(HaveOccurred())
		profiles := strings.Fields(string(contents))
		Ω(profiles).Should(HaveLen(2))
		out, err := exec.Command("ps", "-ax", "-o", "command=").Output()
		Ω(err).ShouldNot(HaveOccurred())
		for _, profile := range profiles {
			Ω(string(out)).ShouldNot(ContainSubstring(profile))
			Ω(profile).ShouldNot(BeADirectory())
		}
	})
})
