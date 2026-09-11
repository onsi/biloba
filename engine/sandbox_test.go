package engine_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("ChromeSandboxDisabled", func() {
	var restoreGOOS, restoreEuid, restoreApparmor func()

	BeforeEach(func() {
		// Default every seam to the "sandbox stays on" side so each It only has to set up the
		// one seam its branch cares about.
		restoreGOOS = engine.SetSandboxGOOSForTest("linux")
		restoreEuid = engine.SetSandboxEuidForTest(1000)
		restoreApparmor = engine.SetSandboxApparmorRestrictedForTest(false, nil)
	})

	AfterEach(func() {
		restoreGOOS()
		restoreEuid()
		restoreApparmor()
	})

	boolPtr := func(v bool) *bool { return &v }

	Context("auto-detection (override is nil)", func() {
		It("disables the sandbox on Linux when running as root, in headless-shell mode", func() {
			engine.SetSandboxEuidForTest(0)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, nil)
			Expect(disabled).To(BeTrue())
			Expect(reason).To(ContainSubstring("root"))
		})

		It("disables the sandbox on Linux when running as root, in headless mode", func() {
			engine.SetSandboxEuidForTest(0)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadless, nil)
			Expect(disabled).To(BeTrue())
			Expect(reason).To(ContainSubstring("root"))
		})

		It("disables the sandbox on Linux when AppArmor restricts unprivileged user namespaces", func() {
			engine.SetSandboxApparmorRestrictedForTest(true, nil)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, nil)
			Expect(disabled).To(BeTrue())
			Expect(reason).To(ContainSubstring("apparmor_restrict_unprivileged_userns"))
		})

		It("leaves the sandbox on when the AppArmor proc file is missing (e.g. non-Ubuntu Linux)", func() {
			engine.SetSandboxApparmorRestrictedForTest(false, errors.New("no such file or directory"))
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, nil)
			Expect(disabled).To(BeFalse())
			Expect(reason).To(BeEmpty())
		})

		It("leaves the sandbox on when neither root nor AppArmor-restricted", func() {
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, nil)
			Expect(disabled).To(BeFalse())
			Expect(reason).To(BeEmpty())
		})

		It("never auto-disables the sandbox for a headful launch, even as root under AppArmor restriction", func() {
			engine.SetSandboxEuidForTest(0)
			engine.SetSandboxApparmorRestrictedForTest(true, nil)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadful, nil)
			Expect(disabled).To(BeFalse())
			Expect(reason).To(BeEmpty())
		})

		It("never auto-disables the sandbox off Linux, even as root", func() {
			engine.SetSandboxGOOSForTest("darwin")
			engine.SetSandboxEuidForTest(0)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, nil)
			Expect(disabled).To(BeFalse())
			Expect(reason).To(BeEmpty())
		})
	})

	Context("explicit override", func() {
		It("forces the sandbox on, beating a root+AppArmor-restricted Linux headless launch", func() {
			engine.SetSandboxEuidForTest(0)
			engine.SetSandboxApparmorRestrictedForTest(true, nil)
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, boolPtr(true))
			Expect(disabled).To(BeFalse())
			Expect(reason).To(BeEmpty())
		})

		It("forces the sandbox off on darwin", func() {
			engine.SetSandboxGOOSForTest("darwin")
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadlessShell, boolPtr(false))
			Expect(disabled).To(BeTrue())
			Expect(reason).NotTo(BeEmpty())
		})

		It("forces the sandbox off for a headful launch", func() {
			disabled, reason := engine.ChromeSandboxDisabled(engine.ChromeModeHeadful, boolPtr(false))
			Expect(disabled).To(BeTrue())
			Expect(reason).NotTo(BeEmpty())
		})
	})
})
