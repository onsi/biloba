package engine_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// silentChromeScript writes a stand-in for Chrome that records each launch's profile in attempts,
// then starts a child holding stdout that never prints - the shape of Chrome's fork child deadlocking
// before it execs.  The child is a shell, so its command line keeps every argument Chrome got,
// --user-data-dir included.  When real is not empty, every launch after the first execs it instead.
func silentChromeScript(dir string, real string) (script string, attempts string) {
	GinkgoHelper()
	script = filepath.Join(dir, "silent-chrome")
	attempts = filepath.Join(dir, "attempts")
	launchedOnce := filepath.Join(dir, "launched-once")
	body := "#!/bin/sh\n" +
		"for a in \"$@\"; do case \"$a\" in --user-data-dir=*) printf '%s\\n' \"${a#--user-data-dir=}\" >> '" + attempts + "';; esac; done\n"
	if real != "" {
		body += "if [ -e '" + launchedOnce + "' ]; then exec '" + real + "' \"$@\"; fi\n" +
			"touch '" + launchedOnce + "'\n"
	}
	body += "sh -c 'while :; do sleep 1 >/dev/null 2>&1; done' biloba-silent-child \"$@\" &\n" +
		"wait\n"
	Expect(os.WriteFile(script, []byte(body), 0o755)).To(Succeed())
	return script, attempts
}

func launchedProfiles(attempts string) []string {
	GinkgoHelper()
	contents, err := os.ReadFile(attempts)
	Expect(err).NotTo(HaveOccurred())
	return strings.Fields(string(contents))
}

// processesNaming lists the running processes whose command line names path.
func processesNaming(path string) []string {
	GinkgoHelper()
	out, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	Expect(err).NotTo(HaveOccurred())
	var matches []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, path) {
			matches = append(matches, strings.TrimSpace(line))
		}
	}
	return matches
}

var _ = Describe("launching Chrome", func() {
	BeforeEach(func() {
		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			Skip("leftover processes are only reaped on macOS and Linux")
		}
	})

	It("retries once with a fresh profile when Chrome prints no DevTools line, then fails and kills what is left", func(ctx SpecContext) {
		DeferCleanup(engine.SetLaunchAttemptTimeoutForTest(time.Second))
		script, attempts := silentChromeScript(GinkgoT().TempDir(), "")

		started := time.Now()
		browser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: script})

		Expect(browser).To(BeNil())
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeBrowserStart))
		Expect(engineErr.Message).To(And(
			ContainSubstring("two attempts, each with a fresh profile"),
			ContainSubstring("websocket url timeout reached after 1s"),
		))
		Expect(time.Since(started)).To(BeNumerically("<", 10*time.Second))

		profiles := launchedProfiles(attempts)
		Expect(profiles).To(HaveLen(2))
		Expect(profiles[0]).NotTo(Equal(profiles[1]))
		for _, profile := range profiles {
			Expect(filepath.Base(profile)).To(HavePrefix("biloba-engine-profile-"))
			Expect(processesNaming(profile)).To(BeEmpty(), "a process still names %s", profile)
			Expect(profile).NotTo(BeADirectory())
		}
	})

	It("starts Chrome on the second attempt when the first prints no DevTools line", func(ctx SpecContext) {
		DeferCleanup(engine.SetLaunchAttemptTimeoutForTest(5 * time.Second))
		script, attempts := silentChromeScript(GinkgoT().TempDir(), chromePath())

		browser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: script})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(browser.Close)
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		value, err := session.Evaluate(ctx, "1 + 1")
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprint(value)).To(Equal("2"))

		profiles := launchedProfiles(attempts)
		Expect(profiles).To(HaveLen(2))
		Expect(processesNaming(profiles[0])).To(BeEmpty(), "the failed attempt left a process behind")
		Expect(profiles[0]).NotTo(BeADirectory())
		Expect(profiles[1]).To(BeADirectory())
	})

	It("does not retry a launch that fails for another reason", func(ctx SpecContext) {
		dir := GinkgoT().TempDir()
		attempts := filepath.Join(dir, "attempts")
		script := filepath.Join(dir, "exiting-chrome")
		Expect(os.WriteFile(script, []byte("#!/bin/sh\necho launched >> '"+attempts+"'\necho 'bad flag'\nexit 1\n"), 0o755)).To(Succeed())

		_, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: script})

		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeBrowserStart))
		Expect(engineErr.Message).NotTo(ContainSubstring("attempts"))
		Expect(launchedProfiles(attempts)).To(HaveLen(1))
	})
})
