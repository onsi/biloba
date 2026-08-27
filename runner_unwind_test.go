package biloba_test

import (
	"fmt"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fatalSentinel struct {
	message string
}

type unwindingT struct {
	biloba.GinkgoTInterface
}

func (t *unwindingT) Fatal(args ...any) {
	panic(fatalSentinel{message: fmt.Sprint(args...)})
}

func (t *unwindingT) Fatalf(format string, args ...any) {
	panic(fatalSentinel{message: fmt.Sprintf(format, args...)})
}

var _ = Describe("runner failure unwinding", func() {
	It("stops the public Go call at a runner-neutral engine failure", func() {
		runner := &unwindingT{GinkgoTInterface: GinkgoT()}
		tab := biloba.ConnectToChrome(runner, biloba.BilobaConfigWithChromeConnection(b.ChromeConnection))
		continued := false
		var recovered any

		func() {
			defer func() { recovered = recover() }()
			tab.Run(`throw new Error("unwind-marker")`)
			continued = true
		}()

		Expect(continued).To(BeFalse())
		sentinel, ok := recovered.(fatalSentinel)
		Expect(ok).To(BeTrue())
		Expect(sentinel.message).To(ContainSubstring("unwind-marker"))
	})
})
