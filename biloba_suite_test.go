package biloba_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
)

func TestBiloba(t *testing.T) {
	gt = newBilobaT(GinkgoT())
	RegisterFailHandler(Fail)
	RunSpecs(t, "Biloba Suite")
}

var ginkgoFormatter = formatter.New(formatter.ColorModePassthrough)

var b *biloba.Biloba
var gt *bilobaT
var fixtureServer string
var failures []string

var _ = SynchronizedBeforeSuite(func() {
	biloba.SpinUpChrome(gt)
}, func() {
	b = biloba.ConnectToChrome(gt) //, biloba.BilobaConfigEnableDebugLogging())
	ServeFixtures()
})

var _ = BeforeEach(func() {
	gt.reset()
	if matches, _ := CurrentSpecReport().MatchesLabelFilter("!no-browser"); matches {
		b.Prepare()
	}
}, OncePerOrdered)

var _ = AfterEach(func() {
	Ω(gt.failures).Should(BeEmpty(), "Did you forget to call ExpectFailures?")
})

func ExpectFailures(expectedFailures ...any) {
	GinkgoHelper()
	matchers := []any{}
	for _, failure := range expectedFailures {
		matchers = append(matchers, matcherOrEqual(failure))
	}
	Expect(gt.failures).To(ConsistOf(matchers...))
	gt.failures = []string{}
}

type bilobaT struct {
	ginkgoT  biloba.GinkgoTInterface
	buffer   *gbytes.Buffer
	failures []string
}

func newBilobaT(ginkgoT biloba.GinkgoTInterface) *bilobaT {
	return &bilobaT{
		ginkgoT:  ginkgoT,
		buffer:   gbytes.NewBuffer(),
		failures: []string{},
	}
}
func (b *bilobaT) reset() {
	Ω(b.buffer.Clear()).Should(Succeed())
	b.failures = []string{}
}

func (b *bilobaT) Helper() { types.MarkAsHelper(1) }
func (b *bilobaT) Logf(format string, args ...interface{}) {
	fmt.Fprintf(b.buffer, format, args...)
	GinkgoWriter.Printf(format+"\n", args...)
}
func (b *bilobaT) Fatal(args ...interface{}) { b.failures = append(b.failures, fmt.Sprintln(args...)) }
func (b *bilobaT) Fatalf(format string, args ...interface{}) {
	b.failures = append(b.failures, fmt.Sprintf(format, args...))
}
func (b *bilobaT) TempDir() string { return b.ginkgoT.TempDir() }
func (b *bilobaT) Failed() bool    { return b.ginkgoT.Failed() }

func (b *bilobaT) GinkgoRecover()           { b.ginkgoT.GinkgoRecover() }
func (b *bilobaT) DeferCleanup(args ...any) { b.ginkgoT.DeferCleanup(args...) }
func (b *bilobaT) Print(args ...any) {
	fmt.Fprint(b.buffer, args...)
	GinkgoWriter.Print(args...)
}
func (b *bilobaT) Printf(format string, args ...any) {
	fmt.Fprintf(b.buffer, format, args...)
	GinkgoWriter.Printf(format, args...)
}
func (b *bilobaT) Println(args ...interface{}) {
	fmt.Fprintln(b.buffer, args...)
	GinkgoWriter.Println(args...)
}
func (b *bilobaT) F(format string, args ...any) string {
	return ginkgoFormatter.F(format, args...)
}
func (b *bilobaT) Fi(indentation uint, format string, args ...any) string {
	return ginkgoFormatter.Fi(indentation, format, args...)
}
func (b *bilobaT) Fiw(indentation uint, maxWidth uint, format string, args ...any) string {
	return ginkgoFormatter.Fiw(indentation, maxWidth, format, args...)
}
func (b *bilobaT) AddReportEntryVisibilityFailureOrVerbose(name string, args ...any) {
	b.ginkgoT.AddReportEntryVisibilityFailureOrVerbose(name, args...)
}
func (b *bilobaT) ParallelProcess() int {
	return b.ginkgoT.ParallelProcess()
}
func (b *bilobaT) ParallelTotal() int {
	return b.ginkgoT.ParallelTotal()
}
func (b *bilobaT) AttachProgressReporter(f func() string) func() {
	return b.ginkgoT.AttachProgressReporter(f)
}

func matcherOrEqual(expected interface{}) OmegaMatcher {
	var matcher OmegaMatcher
	switch v := expected.(type) {
	case OmegaMatcher:
		matcher = v
	default:
		matcher = Equal(v)
	}
	return matcher
}

func ServeFixtures() {
	s := ghttp.NewServer()
	s.RouteToHandler("GET", regexp.MustCompile(`/[a-z\.]*`), func(w http.ResponseWriter, r *http.Request) {
		fname := strings.Trim(r.URL.Path, "/")
		fixture, err := os.ReadFile("./fixtures/" + fname)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(fixture)
	})
	fixtureServer = s.URL()
	DeferCleanup(s.Close)
}
