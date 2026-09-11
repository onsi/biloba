package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/onsi/biloba/engine"
)

// chromeInstaller and chromeLocator are the seams `install-chrome` calls through, so specs can
// stub them instead of hitting the network or a real filesystem cache.
var (
	chromeInstaller             = engine.InstallHeadlessShell
	chromeLocator               = engine.LocateChrome
	platformSupportsAutoInstall = engine.SupportsCurrentPlatform
)

// runInstallChrome implements the `install-chrome` subcommand: ensure the current Chrome for
// Testing Stable chrome-headless-shell is in Biloba's cache, then print the resolved path to
// stdout - a no-op download when that version is already cached.  stdout carries only the path, so
// scripts (and `npx biloba install-chrome`) can capture it; everything else goes to stderr.
func runInstallChrome(ctx context.Context, stdout, stderr io.Writer) error {
	installedPath, installErr := chromeInstaller(ctx)
	if installErr != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if fallback := chromeLocator(""); fallback != "" {
			fmt.Fprintf(stderr, "warning: could not install Chrome for Testing (%v); Biloba will use the chrome-headless-shell binary already found at %s\n", installErr, fallback)
			_, err := fmt.Fprintln(stdout, fallback)
			return err
		}
		return installErr
	}
	if resolved := chromeLocator(""); resolved != "" && resolved != installedPath {
		fmt.Fprintf(stderr, "note: Biloba will use %s instead of the binary just installed at %s (%s)\n", resolved, installedPath, chromeOverrideReason(resolved))
	}
	_, err := fmt.Fprintln(stdout, installedPath)
	return err
}

// chromeOverrideReason explains, in the same precedence LocateChrome searches, why the binary it
// resolves to differs from the one install-chrome just installed.
func chromeOverrideReason(resolved string) string {
	if env := os.Getenv(engine.ChromeEnvVar); env != "" && env == resolved {
		return fmt.Sprintf("%s is set", engine.ChromeEnvVar)
	}
	if onPath, err := exec.LookPath("chrome-headless-shell"); err == nil && onPath == resolved {
		return "it is on PATH"
	}
	return "a newer chrome-headless-shell is already cached"
}

// augmentChromeNotFoundError adds a next step to a "could not find chrome-headless-shell" failure
// that a TS/npm caller can actually act on - it has no other route to Biloba's docs at the point
// this error reaches it.  engine.ResolveHeadlessShell's own message stays runner-neutral (the
// Go/Ginkgo suite hits the same failure with different remedies), so the npm-flavored hint is
// added here instead of there.  Any other error, including a StartBrowser failure that has nothing
// to do with locating Chrome, passes through unchanged.
func augmentChromeNotFoundError(err error) error {
	if err == nil || !errors.Is(err, engine.ErrChromeNotFound) {
		return err
	}
	if !platformSupportsAutoInstall() {
		return fmt.Errorf("%w\nChrome for Testing has no chrome-headless-shell build for this platform; install a full Chrome or Chromium browser yourself and point Biloba at it with an explicit executable path in headless mode instead of relying on auto-install", err)
	}
	return fmt.Errorf("%w\nrun `npx biloba install-chrome` to install one, set %s, pass an explicit Chrome path, or enable autoInstall", err, engine.ChromeEnvVar)
}
