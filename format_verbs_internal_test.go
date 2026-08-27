package biloba

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// %w is a verb only fmt.Errorf (and its wrapping relatives) understands.  Everything else in the
// formatting family - Sprintf, Printf, and every Ginkgo/Gomega helper built on Sprintf - renders it
// as a diagnostic instead:
//
//	failed to connect to chrome: %!w(*errors.errorString=&{could not create a browser context})
//
// Biloba had eight of these, all in the Chrome setup path (gt.Fatalf is Sprintf-based - Ginkgo's
// testingtproxy does fmt.Sprintf(format, args...) before failing).  So the one line a user gets when
// Chrome will not start hid the reason inside a formatting error, at the moment they have the least
// context and most need the sentence to be readable.
//
// go vet does not catch this: its printf analyser only knows the wrappers it can recognise, and
// gt.Fatalf reaches Sprintf through an interface method it does not follow.  `go vet ./...` was clean
// the whole time those eight sites were live.  That is the same reason the view-copy class needed
// tab_state_internal_test.go - a silent class that the toolchain cannot see needs a spec that can.
//
// The rule is exact rather than heuristic, which is what makes this cheap to keep: %w is correct in
// fmt.Errorf and nowhere else in this codebase.
var _ = ginkgo.Describe("format verbs in Biloba's own source", ginkgo.Label("no-browser"), func() {
	// wrapVerb finds a %w that is NOT preceded by an fmt.Errorf( on the same line.  Every legitimate
	// use in Biloba is a single-line fmt.Errorf, so a line carrying %w without one is the bug.
	errorfOnTheLine := regexp.MustCompile(`(fmt\.Errorf|errors\.Wrap)\(`)
	wrapVerb := regexp.MustCompile(`%w`)

	ginkgo.It("uses %w only in fmt.Errorf, never in a Sprintf-based failure", func() {
		sources, err := filepath.Glob("*.go")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(sources).NotTo(gomega.BeEmpty(), "should have found Biloba's own source files")

		offenders := []string{}
		for _, source := range sources {
			if strings.HasSuffix(source, "_test.go") {
				continue
			}
			contents, err := os.ReadFile(source)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			for i, line := range strings.Split(string(contents), "\n") {
				if wrapVerb.MatchString(line) && !errorfOnTheLine.MatchString(line) {
					offenders = append(offenders, source+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
				}
			}
		}

		gomega.Expect(offenders).To(gomega.BeEmpty(),
			"%%w is only a verb to fmt.Errorf.  Anywhere else - and every gt.Fatalf/Printf is Sprintf-based -\n"+
				"it renders as %%!w(*errors.errorString=&{...}), hiding the error inside a formatting diagnostic.\n"+
				"Use %%s.  Offending lines:\n  %s", strings.Join(offenders, "\n  "))
	})
})

// itoa keeps this file free of a strconv import for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
