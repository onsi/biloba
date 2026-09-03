package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/onsi/biloba/engine"
	"github.com/onsi/biloba/protocol"
)

type config struct {
	chromePath  string
	chromeWSURL string
	artifactDir string
}

type browserReady struct {
	WSURL  string `json:"wsURL"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	PID    int    `json:"pid"`
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
	chromePath, err := resolveChrome(config)
	if err != nil {
		return err
	}
	browser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath, WebSocketURL: config.chromeWSURL, ArtifactDir: config.artifactDir})
	if err != nil {
		return err
	}
	server := protocol.NewServer(&engineBackend{browser: browser})
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
	flags.StringVar(&result.artifactDir, "artifact-dir", "", "failure artifact directory")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	return result, nil
}

// resolveChrome finds the browser this daemon should drive.  A worker daemon pointed at a shared
// Chrome does not need one at all; otherwise it runs the same runner-neutral search the Go suite
// does, so `bilobad` works with no flags wherever `ginkgo` does - and fails with the reason rather
// than with a bare exec error when it does not.
func resolveChrome(config config) (string, error) {
	if config.chromeWSURL != "" {
		return "", nil
	}
	if path := engine.LocateChrome(config.chromePath); path != "" {
		return path, nil
	}
	if config.chromePath != "" {
		return "", fmt.Errorf("bilobad could not use the Chrome executable at %q", config.chromePath)
	}
	return "", fmt.Errorf("bilobad could not find chrome-headless-shell.\nInstall it with `npx @puppeteer/browsers install chrome-headless-shell@stable`, add it to your PATH, set %s=/path/to/chrome-headless-shell, or pass --chrome-path.", engine.ChromeEnvVar)
}

func runBrowserHost(ctx context.Context, args []string, stdout io.Writer, stdin io.Reader) error {
	config, err := parseConfig(args)
	if err != nil {
		return err
	}
	chromePath, err := resolveChrome(config)
	if err != nil {
		return err
	}
	browser, err := engine.StartBrowser(ctx, engine.BrowserConfig{ExecutablePath: chromePath})
	if err != nil {
		return err
	}
	defer browser.Close()
	ready, err := json.Marshal(browserReady{WSURL: browser.WebSocketURL(), Width: 1920, Height: 1080, PID: os.Getpid()})
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
