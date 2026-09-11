package engine

import (
	"os"
	"runtime"
	"strings"
)

// apparmorRestrictUnprivilegedUserNSPath is the kernel file Ubuntu 23.10+ uses to report whether
// AppArmor is restricting unprivileged user namespaces - the setting that starves an unsandboxed
// Chrome's own sandbox of the namespaces it needs, and the reason CI containers built on newer
// Ubuntu bases see "No usable sandbox" from a chrome-headless-shell that has no AppArmor profile.
const apparmorRestrictUnprivilegedUserNSPath = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"

// sandboxGOOS, sandboxEuid, and sandboxApparmorRestricted are seams over runtime.GOOS, os.Geteuid,
// and the AppArmor proc file so ChromeSandboxDisabled's specs can drive every branch (root,
// AppArmor-restricted, neither, non-Linux) without needing a matching real host.
var (
	sandboxGOOS               = func() string { return runtime.GOOS }
	sandboxEuid               = os.Geteuid
	sandboxApparmorRestricted = readApparmorRestrictedUserNS
)

func readApparmorRestrictedUserNS() (bool, error) {
	contents, err := os.ReadFile(apparmorRestrictUnprivilegedUserNSPath)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(contents)) == "1", nil
}

// ChromeSandboxDisabled decides whether StartBrowser should launch Chrome with --no-sandbox.
//
// override is the tri-state knob: nil means auto-detect, a pointer to true forces the sandbox ON
// (the flag is never added, any mode or OS), and a pointer to false forces it OFF (the flag is
// always added). Auto-detection only ever disables the sandbox in a headless mode - ChromeModeHeadlessShell
// or ChromeModeHeadless - because a headful --no-sandbox launch shows an "unsupported command-line
// flag" infobar that changes the viewport. Within a headless mode, auto-detection disables the
// sandbox on Linux when the process is running as root (euid 0, where Chrome's own sandbox refuses
// to start at all) or when the kernel reports AppArmor is restricting unprivileged user namespaces
// (see apparmorRestrictUnprivilegedUserNSPath) - the state Ubuntu 23.10+ ships by default, which
// starves the sandbox of the namespaces it needs for a binary with no AppArmor profile, such as a
// chrome-headless-shell pulled into a cache directory. Anywhere else (macOS, an unrestricted Linux
// host) auto-detection leaves the sandbox on.
//
// It returns the decision plus a short human-readable reason, non-empty only when the sandbox is
// being disabled, suitable for a debug log.
func ChromeSandboxDisabled(mode ChromeMode, override *bool) (disabled bool, reason string) {
	if override != nil {
		if *override {
			return false, ""
		}
		return true, "sandbox explicitly disabled"
	}
	if mode != ChromeModeHeadlessShell && mode != ChromeModeHeadless {
		return false, ""
	}
	if sandboxGOOS() != "linux" {
		return false, ""
	}
	if sandboxEuid() == 0 {
		return true, "running as root on Linux"
	}
	if restricted, err := sandboxApparmorRestricted(); err == nil && restricted {
		return true, "kernel.apparmor_restrict_unprivileged_userns=1 (" + apparmorRestrictUnprivilegedUserNSPath + ")"
	}
	return false, ""
}
