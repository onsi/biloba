package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/onsi/biloba/engine"
	"github.com/onsi/biloba/protocol"
)

type config struct {
	chromePath                 string
	chromeWSURL                string
	chromeMode                 engine.ChromeMode
	chromeArgs                 []string
	autoInstall                bool
	windowWidth                int
	windowHeight               int
	attachedLaunch             *protocol.WireLaunchMetadata
	debugLog                   bool
	artifactDir                string
	visualArtifactDir          string
	screenshotBaselinesDir     string
	updateScreenshots          bool
	screenshotPixelTolerance   float64
	screenshotChannelTolerance int
	maxScreenshotBytes         int
}

type browserReady struct {
	WSURL  string             `json:"wsURL"`
	PID    int                `json:"pid"`
	Launch hostLaunchMetadata `json:"launch"`
}

type hostLaunchMetadata struct {
	Mode           string   `json:"mode"`
	ExecutablePath string   `json:"executablePath"`
	ChromeArgs     []string `json:"chromeArgs"`
	WindowSize     struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"windowSize"`
	AutoInstalled bool `json:"autoInstalled"`
}

type stringList []string

func (values *stringList) String() string { return fmt.Sprint([]string(*values)) }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	args := os.Args[1:]
	var err error
	if len(args) > 0 && args[0] == "serve-browser" {
		err = runBrowserHost(ctx, args[1:], os.Stdout, os.Stdin)
	} else {
		err = run(ctx, args, os.Stdout, os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, stdin io.Reader) error {
	config, err := parseConfig(args)
	if err != nil {
		return err
	}
	var debug *debugHub
	browserConfig := engine.BrowserConfig{ExecutablePath: config.chromePath, WebSocketURL: config.chromeWSURL, Mode: config.chromeMode, Arguments: config.chromeArgs, WindowWidth: config.windowWidth, WindowHeight: config.windowHeight, ArtifactDir: config.artifactDir, AutoInstall: config.autoInstall}
	if config.attachedLaunch != nil {
		browserConfig.Mode = engine.ChromeMode(config.attachedLaunch.Mode)
		browserConfig.WindowWidth = config.attachedLaunch.Width
		browserConfig.WindowHeight = config.attachedLaunch.Height
	}
	if config.debugLog {
		debug = newDebugHub()
		browserConfig.DebugSink = debug.publish
	}
	browser, err := engine.StartBrowser(ctx, browserConfig)
	if err != nil {
		return err
	}
	backend := &engineBackend{browser: browser, debug: debug, visual: engine.VisualOptions{
		BaselineDir: config.screenshotBaselinesDir, ArtifactDir: config.artifactDir, Update: config.updateScreenshots,
		Tolerance: engine.ScreenshotTolerance{PixelFraction: config.screenshotPixelTolerance, ChannelDelta: config.screenshotChannelTolerance}, MaxBytes: config.maxScreenshotBytes,
	}, maxScreenshotBytes: config.maxScreenshotBytes}
	backend.visual.ArtifactDir = config.visualArtifactDir
	if backend.visual.ArtifactDir == "" {
		backend.visual.ArtifactDir = config.artifactDir
	}
	if config.attachedLaunch != nil {
		backend.launch = *config.attachedLaunch
	}
	server := protocol.NewServer(backend)
	defer server.Close()

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- protocol.ServeStdio(ctx, server, stdin, stdout) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-serveErrors:
		return err
	}
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("bilobad", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var result config
	flags.StringVar(&result.chromePath, "chrome-path", "", "Chrome or chrome-headless-shell executable (default: the same search Biloba's Go suite uses)")
	flags.StringVar(&result.chromeWSURL, "chrome-ws-url", "", "browser-level Chrome DevTools websocket URL")
	var mode string
	flags.StringVar(&mode, "chrome-mode", "", "headless-shell, headless, or headful")
	flags.Var((*stringList)(&result.chromeArgs), "chrome-arg", "raw Chrome argument; repeatable")
	flags.BoolVar(&result.autoInstall, "auto-install", false, "install chrome-headless-shell when missing")
	flags.IntVar(&result.windowWidth, "window-width", 0, "starting viewport width")
	flags.IntVar(&result.windowHeight, "window-height", 0, "starting viewport height")
	var attachedMetadata string
	flags.StringVar(&attachedMetadata, "attached-launch-metadata", "", "base64 encoded descriptive shared-browser launch metadata")
	flags.BoolVar(&result.debugLog, "debug-log", false, "emit structured CDP debug events")
	flags.StringVar(&result.artifactDir, "artifact-dir", "", "failure artifact directory")
	flags.StringVar(&result.visualArtifactDir, "visual-artifact-dir", "", "visual comparison artifact directory")
	flags.StringVar(&result.screenshotBaselinesDir, "screenshot-baselines-dir", "biloba-baselines", "visual screenshot baseline directory")
	flags.BoolVar(&result.updateScreenshots, "update-screenshots", false, "create and update visual screenshot baselines")
	flags.Float64Var(&result.screenshotPixelTolerance, "screenshot-pixel-tolerance", 0, "fraction of pixels allowed to differ")
	flags.IntVar(&result.screenshotChannelTolerance, "screenshot-channel-tolerance", 0, "per-channel pixel delta allowed")
	flags.IntVar(&result.maxScreenshotBytes, "max-screenshot-bytes", protocol.MaxScreenshotBytes, "maximum decoded PNG bytes")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	result.chromeMode = engine.ChromeMode(mode)
	if mode != "" && result.chromeMode != engine.ChromeModeHeadlessShell && result.chromeMode != engine.ChromeModeHeadless && result.chromeMode != engine.ChromeModeHeadful {
		return config{}, fmt.Errorf("chrome-mode must be headless-shell, headless, or headful")
	}
	if (result.windowWidth == 0) != (result.windowHeight == 0) || result.windowWidth < 0 || result.windowHeight < 0 {
		return config{}, fmt.Errorf("window-width and window-height must both be positive")
	}
	if result.chromeWSURL != "" && (result.chromePath != "" || mode != "" || len(result.chromeArgs) > 0 || result.autoInstall || result.windowWidth != 0) {
		return config{}, fmt.Errorf("chrome-ws-url conflicts with process launch options")
	}
	if result.autoInstall && result.chromeMode != "" && result.chromeMode != engine.ChromeModeHeadlessShell {
		return config{}, fmt.Errorf("auto-install is only valid for headless-shell mode")
	}
	if attachedMetadata != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(attachedMetadata)
		if decodeErr != nil {
			return config{}, fmt.Errorf("attached-launch-metadata must be base64 JSON: %w", decodeErr)
		}
		var metadata protocol.WireLaunchMetadata
		if decodeErr = json.Unmarshal(decoded, &metadata); decodeErr != nil {
			return config{}, fmt.Errorf("attached-launch-metadata must be valid JSON: %w", decodeErr)
		}
		validMode := metadata.Mode == string(engine.ChromeModeHeadlessShell) || metadata.Mode == string(engine.ChromeModeHeadless) || metadata.Mode == string(engine.ChromeModeHeadful)
		if result.chromeWSURL == "" || !metadata.Attached || !validMode || metadata.Width <= 0 || metadata.Height <= 0 {
			return config{}, fmt.Errorf("attached-launch-metadata is incomplete")
		}
		metadata.Arguments = append([]string(nil), metadata.Arguments...)
		result.attachedLaunch = &metadata
	}
	if math.IsNaN(result.screenshotPixelTolerance) || math.IsInf(result.screenshotPixelTolerance, 0) || result.screenshotPixelTolerance < 0 || result.screenshotPixelTolerance > 1 {
		return config{}, fmt.Errorf("screenshot-pixel-tolerance must be between 0 and 1")
	}
	if result.screenshotChannelTolerance < 0 || result.screenshotChannelTolerance > 255 {
		return config{}, fmt.Errorf("screenshot-channel-tolerance must be between 0 and 255")
	}
	if result.maxScreenshotBytes <= 0 || result.maxScreenshotBytes > protocol.MaxScreenshotBytes {
		return config{}, fmt.Errorf("max-screenshot-bytes must be between 1 and %d", protocol.MaxScreenshotBytes)
	}
	for _, path := range []*string{&result.artifactDir, &result.visualArtifactDir, &result.screenshotBaselinesDir} {
		if *path == "" {
			continue
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return config{}, err
		}
		*path = absolute
	}
	return result, nil
}

// resolveChrome finds the browser this daemon should drive.  A worker daemon pointed at a shared
// Chrome does not need one at all; otherwise it runs the same runner-neutral search the Go suite
// does, so `bilobad` works with no flags wherever `ginkgo` does - and fails with the reason rather
// than with a bare exec error when it does not.
func runBrowserHost(ctx context.Context, args []string, stdout io.Writer, stdin io.Reader) error {
	config, err := parseConfig(args)
	if err != nil {
		return err
	}
	if config.chromeWSURL != "" || config.attachedLaunch != nil {
		return fmt.Errorf("serve-browser does not accept attachment options")
	}
	browser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: config.chromePath, Mode: config.chromeMode, Arguments: config.chromeArgs, WindowWidth: config.windowWidth, WindowHeight: config.windowHeight, AutoInstall: config.autoInstall})
	if err != nil {
		return err
	}
	defer browser.Close()
	ready, err := json.Marshal(browserReady{WSURL: browser.WebSocketURL(), PID: os.Getpid(), Launch: launchMetadataForHost(browser.LaunchMetadata())})
	if err != nil {
		return fmt.Errorf("marshal browser ready message: %w", err)
	}
	if _, err := fmt.Fprintln(stdout, string(ready)); err != nil {
		return fmt.Errorf("write browser ready message: %w", err)
	}
	parentClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		close(parentClosed)
	}()
	select {
	case <-ctx.Done():
		return nil
	case <-parentClosed:
		return nil
	}
}
