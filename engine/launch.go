package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// DefaultLaunchAttemptTimeout is how long each launch attempt waits for Chrome to print its DevTools
// line.  LaunchChrome makes at most two attempts, so the whole wait stays at the 60s a single attempt
// used to get.
const DefaultLaunchAttemptTimeout = 30 * time.Second

// launchAttemptTimeout is DefaultLaunchAttemptTimeout, shortened by specs (see export_test.go).
var launchAttemptTimeout = DefaultLaunchAttemptTimeout

// LaunchOptions configures LaunchChrome.
type LaunchOptions struct {
	// ProfilePrefix names the temporary profile directory LaunchChrome makes for each attempt.
	ProfilePrefix string
	// Allocator holds the Chrome flags.  LaunchChrome adds the profile and the DevTools-line
	// timeout ahead of them.
	Allocator []chromedp.ExecAllocatorOption
	// Browser holds the options for the browser context Start runs on.
	Browser []chromedp.ContextOption
	// Slowf, when not nil, is told about listeners that hold their target's messages too long.
	Slowf func(string, ...any)
	// Logf, when not nil, is told when an attempt fails and LaunchChrome tries again.
	Logf func(string, ...any)
	// AttemptTimeout overrides the time each attempt waits for the DevTools line.  Zero uses
	// DefaultLaunchAttemptTimeout.
	AttemptTimeout time.Duration
	// Start runs the first chromedp command on the browser context, which is what starts Chrome.
	Start func(context.Context) error
}

// LaunchedChrome is a Chrome process LaunchChrome started.
type LaunchedChrome struct {
	// Context is the browser context Start ran on.  Cancelling it ends Chrome.
	Context context.Context
	// Profile is the profile directory LaunchChrome made for this Chrome.
	Profile string
	cancel  func()
}

// Cancel ends Chrome and removes its profile directory.
func (l *LaunchedChrome) Cancel() { l.cancel() }

/*
LaunchChrome starts one Chrome process with a fresh temporary profile and runs opts.Start on it.

Chrome occasionally hangs before it prints its DevTools line (on macOS a forked child deadlocks before
it execs, which leaves an orphan behind).  When that happens LaunchChrome kills every process whose
command line names that attempt's profile, removes the profile, and tries once more with a new one.
Each attempt gets half of the 60s budget.  Other launch errors are returned straight away.

The kill matches only the profile directory LaunchChrome made for the attempt, so it never reaches a
Chrome it did not start.  A user-data-dir passed in opts.Allocator replaces that profile on Chrome's
command line, so nothing matches and nothing is killed.
*/
func LaunchChrome(parent context.Context, opts LaunchOptions) (*LaunchedChrome, error) {
	timeout := opts.AttemptTimeout
	if timeout <= 0 {
		timeout = launchAttemptTimeout
	}
	launched, err := launchAttempt(parent, opts, timeout)
	if !printedNoDevToolsLine(err) {
		return launched, err
	}
	if opts.Logf != nil {
		opts.Logf("Chrome printed no DevTools line within %s; retrying once with a fresh profile", timeout)
	}
	launched, err = launchAttempt(parent, opts, timeout)
	switch {
	case printedNoDevToolsLine(err):
		return nil, fmt.Errorf("Chrome printed no DevTools line on either of two attempts, each with a fresh profile: %w", err)
	case err != nil:
		return nil, fmt.Errorf("retried once with a fresh profile after Chrome printed no DevTools line: %w", err)
	}
	return launched, nil
}

func launchAttempt(parent context.Context, opts LaunchOptions, timeout time.Duration) (*LaunchedChrome, error) {
	profile, err := os.MkdirTemp("", opts.ProfilePrefix)
	if err != nil {
		return nil, err
	}
	allocator := append([]chromedp.ExecAllocatorOption{
		chromedp.UserDataDir(profile),
		chromedp.WSURLReadTimeout(timeout),
	}, opts.Allocator...)
	allocCtx, cancelAllocator := NewExecAllocator(parent, opts.Slowf, allocator...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx, opts.Browser...)
	cancel := func() {
		cancelBrowser()
		cancelAllocator()
		_ = os.RemoveAll(profile)
	}
	if err := opts.Start(browserCtx); err != nil {
		cancelBrowser()
		cancelAllocator()
		// The main process is gone now, but a child it forked can outlive it.
		killProcessesNaming(profile)
		_ = os.RemoveAll(profile)
		return nil, err
	}
	return &LaunchedChrome{Context: browserCtx, Profile: profile, cancel: cancel}, nil
}

// printedNoDevToolsLine reports chromedp's error for a Chrome that printed no DevTools line in time.
// chromedp has no sentinel for it.
func printedNoDevToolsLine(err error) bool {
	return err != nil && strings.Contains(err.Error(), "websocket url timeout reached")
}

// killProcessesNaming kills the processes whose command line has --user-data-dir=profile as a whole
// argument, until a few passes in a row find none (a child can fork just as its parent dies).  It is
// how chromedp cleans up after the temporary profiles it makes itself.
func killProcessesNaming(profile string) {
	if profile == "" {
		return
	}
	const (
		quietPasses = 3
		pause       = 10 * time.Millisecond
		limit       = 2 * time.Second
	)
	want := "--user-data-dir=" + profile
	deadline := time.Now().Add(limit)
	for quiet := 0; quiet < quietPasses && time.Now().Before(deadline); time.Sleep(pause) {
		if killCommandLinesContaining(want) == 0 {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// namesArgument reports whether a command line (arguments joined by spaces) has want as a whole
// argument, so that /tmp/profile-1 never matches /tmp/profile-12.
func namesArgument(commandLine string, want string) bool {
	return strings.Contains(" "+commandLine+" ", " "+want+" ")
}
