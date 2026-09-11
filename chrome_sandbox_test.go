package biloba_test

import (
	"github.com/onsi/biloba"
	"github.com/onsi/biloba/engine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ChromeSandbox reaching SpinUpChrome's sandbox decision", Label("no-browser"), func() {
	It("leaves the auto decision alone when ChromeSandbox is not passed", func() {
		disabled, reason := biloba.SandboxDecisionForTest(engine.ChromeModeHeadlessShell)
		// This process is not (in CI or locally) both root and running on an AppArmor-restricted
		// Linux host, so the auto decision leaves the sandbox on - proving the zero-value config
		// (no override) reaches engine.ChromeSandboxDisabled as a nil override, not a forced one.
		Expect(disabled).To(BeFalse())
		Expect(reason).To(BeEmpty())
	})

	It("forces the sandbox on when ChromeSandbox(true) is passed", func() {
		disabled, _ := biloba.SandboxDecisionForTest(engine.ChromeModeHeadlessShell, biloba.ChromeSandbox(true))
		Expect(disabled).To(BeFalse())
	})

	It("forces the sandbox off when ChromeSandbox(false) is passed, even in headful mode", func() {
		disabled, reason := biloba.SandboxDecisionForTest(engine.ChromeModeHeadful, biloba.ChromeSandbox(false))
		Expect(disabled).To(BeTrue())
		Expect(reason).NotTo(BeEmpty())
	})
})
