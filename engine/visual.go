package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
)

const DefaultMaxScreenshotBytes = 16 << 20
const DefaultMaxScreenshotPixels = 64 << 20

var visualFilenameRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type ScreenshotCaptureOptions struct {
	Masks       []Selector
	Animated    bool
	ColorScheme string
	MaxBytes    int
}

type Screenshot struct {
	PNG           []byte
	Width, Height int
	Warning       string
	FullyClipped  bool
	Vanished      bool
}

type ScreenshotTarget struct{ selector *Selector }

func PageScreenshotTarget() ScreenshotTarget { return ScreenshotTarget{} }

func ElementScreenshotTarget(selector Selector) ScreenshotTarget {
	return ScreenshotTarget{selector: &selector}
}

type VisualOptions struct {
	BaselineDir, ArtifactDir string
	Update                   bool
	Masks                    []Selector
	Tolerance                ScreenshotTolerance
	Animated                 bool
	ColorSchemes             []string
	MaxBytes                 int
	SettleAttempts           int
	SettleStreak             int
	SettleInterval           time.Duration
}

type VisualResult struct {
	Match, Updated bool
	Schemes        []VisualSchemeResult
	Warnings       []string
}

type VisualSchemeResult struct {
	Scheme                             string
	Match, Updated                     bool
	BaselinePath, ActualPath, DiffPath string
	Diff                               ScreenshotDiff
	Diagnosis                          string
}

func (s *Session) CapturePageScreenshot(ctx context.Context, options ScreenshotCaptureOptions) (shot Screenshot, err error) {
	err = s.serial(ctx, "capture page screenshot", func(opCtx context.Context) error {
		shot, err = s.captureScreenshot(opCtx, nil, options)
		return err
	})
	return
}

func (s *Session) CaptureElementScreenshot(ctx context.Context, selector Selector, options ScreenshotCaptureOptions) (shot Screenshot, err error) {
	err = s.serial(ctx, "capture element screenshot", func(opCtx context.Context) error {
		shot, err = s.captureScreenshot(opCtx, &selector, options)
		return err
	})
	return
}

func (s *Session) captureScreenshot(ctx context.Context, selector *Selector, options ScreenshotCaptureOptions) (shot Screenshot, err error) {
	if err = s.ensureBiloba(ctx); err != nil {
		return shot, err
	}
	if options.ColorScheme != "" && options.ColorScheme != "light" && options.ColorScheme != "dark" {
		return shot, fmt.Errorf("unsupported color scheme %q: use light or dark", options.ColorScheme)
	}
	if response, callErr := RunHandlerAsyncContext(ctx, "fontsReady", ""); callErr != nil || response.Err != "" {
		return shot, handlerCallError("wait for fonts", response, callErr)
	}
	if options.ColorScheme != "" {
		defer func() {
			cleanupCtx, cancel := s.visualCleanupContext()
			defer cancel()
			if restoreErr := s.clearVisualColor(cleanupCtx); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("restore color scheme emulation: %w", restoreErr))
			}
		}()
		if err = s.applyVisualColor(ctx, options.ColorScheme); err != nil {
			return shot, err
		}
	}
	if !options.Animated {
		defer func() {
			cleanupCtx, cancel := s.visualCleanupContext()
			defer cancel()
			if restoreErr := s.clearVisualFreeze(cleanupCtx); restoreErr != nil {
				err = errors.Join(err, restoreErr)
			}
		}()
		if err = s.applyVisualFreeze(ctx); err != nil {
			return shot, err
		}
	}

	var pngBytes []byte
	var originX, originY, cssWidth float64
	if selector == nil {
		pngBytes, err = CapturePageContext(ctx, &cssWidth)
	} else {
		response, callErr := RunHandlerContext(ctx, "boundingBox", selector.Encoded())
		if callErr != nil || response.Err != "" || !response.Success {
			return shot, handlerCallError("resolve screenshot element", response, callErr)
		}
		box, ok := response.Result.(map[string]any)
		if !ok {
			return shot, fmt.Errorf("unexpected bounding box result: %v", response.Result)
		}
		if clipper, clipped := box["clipper"].(string); clipped && clipper != "" {
			visibleFraction := floatValue(box["visibleFraction"])
			if visibleFraction <= 0 {
				shot.Warning = fmt.Sprintf("element %s was not painted because it is clipped by %s", selector.Description(), clipper)
				shot.FullyClipped = true
			} else {
				shot.Warning = fmt.Sprintf("element %s is partially clipped by %s (%.0f%% visible)", selector.Description(), clipper, visibleFraction*100)
			}
		}
		clip := &page.Viewport{X: floatValue(box["x"]), Y: floatValue(box["y"]), Width: floatValue(box["width"]), Height: floatValue(box["height"]), Scale: 1}
		originX, originY, cssWidth = clip.X, clip.Y, clip.Width
		inViewport, _ := box["inViewport"].(bool)
		pngBytes, err = CaptureClipContext(ctx, clip, !inViewport)
		if err == nil && !inViewport {
			stillThere, existsErr := RunHandlerContext(ctx, "exists", selector.Encoded())
			if existsErr == nil && stillThere.Err == "" && !stillThere.Success {
				shot.Vanished = true
				shot.Warning = fmt.Sprintf("element %s was present before this capture and gone after it; viewport expansion caused the page to remove its own subject", selector.Description())
			}
		}
	}
	if err != nil {
		return shot, err
	}
	if err = validateScreenshotPNG(pngBytes, options.MaxBytes); err != nil {
		return shot, err
	}

	if len(options.Masks) > 0 {
		config, _ := png.DecodeConfig(bytes.NewReader(pngBytes))
		scale := 1.0
		if cssWidth > 0 {
			scale = float64(config.Width) / cssWidth
		}
		encoded := make([]string, len(options.Masks))
		for i, mask := range options.Masks {
			encoded[i] = mask.Encoded()
		}
		response, callErr := RunHandlerContext(ctx, "maskBoxes", "", encoded)
		if callErr != nil || response.Err != "" {
			return shot, handlerCallError("resolve screenshot masks", response, callErr)
		}
		var rects []image.Rectangle
		for _, entry := range anySlice(response.Result) {
			box, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			x, y := floatValue(box["x"])-originX, floatValue(box["y"])-originY
			w, h := floatValue(box["width"]), floatValue(box["height"])
			rects = append(rects, image.Rect(int(math.Floor(x*scale)), int(math.Floor(y*scale)), int(math.Ceil((x+w)*scale)), int(math.Ceil((y+h)*scale))))
		}
		pngBytes, err = maskScreenshotPNG(pngBytes, rects)
		if err != nil {
			return shot, err
		}
		if err = validateScreenshotPNG(pngBytes, options.MaxBytes); err != nil {
			return shot, err
		}
	}
	config, _ := png.DecodeConfig(bytes.NewReader(pngBytes))
	shot.PNG, shot.Width, shot.Height = pngBytes, config.Width, config.Height
	return shot, nil
}

func (s *Session) visualCleanupContext() (context.Context, context.CancelFunc) {
	requestCtx, requestCancel := context.WithTimeout(context.Background(), 5*time.Second)
	cleanupCtx, cleanupCancel := executorContext(s.ctx, requestCtx)
	return cleanupCtx, func() {
		cleanupCancel()
		requestCancel()
	}
}

func (s *Session) CompareScreenshot(ctx context.Context, name string, target ScreenshotTarget, options VisualOptions) (result VisualResult, err error) {
	err = s.serial(ctx, "compare screenshot", func(opCtx context.Context) error {
		result, err = s.compareScreenshot(opCtx, name, target, options)
		return err
	})
	return
}

func (s *Session) compareScreenshot(ctx context.Context, name string, target ScreenshotTarget, options VisualOptions) (VisualResult, error) {
	if err := validateScreenshotTolerance(options.Tolerance); err != nil {
		return VisualResult{}, Fatal(err)
	}
	schemes := options.ColorSchemes
	if len(schemes) == 0 {
		schemes = []string{""}
	}
	result := VisualResult{Match: true}
	captures := map[string][]byte{}
	for _, scheme := range schemes {
		relative, err := ScreenshotBaselinePath(name, scheme)
		if err != nil {
			return result, Fatal(err)
		}
		baselinePath := filepath.Join(options.BaselineDir, relative)
		captureOptions := ScreenshotCaptureOptions{Masks: options.Masks, Animated: options.Animated, ColorScheme: scheme, MaxBytes: options.MaxBytes}
		var shot Screenshot
		settled := true
		if options.Update {
			shot, settled, err = s.captureSettled(ctx, target.selector, captureOptions, options)
		} else {
			shot, err = s.captureScreenshot(ctx, target.selector, captureOptions)
		}
		if err != nil {
			return result, err
		}
		if shot.FullyClipped {
			message := shot.Warning
			if options.Update {
				message = "refusing to write a screenshot baseline: " + message
				return result, Fatal(fmt.Errorf("%s", message))
			}
			return result, fmt.Errorf("%s", message)
		}
		if shot.Vanished {
			return result, fmt.Errorf("%s", shot.Warning)
		}
		if shot.Warning != "" {
			result.Warnings = append(result.Warnings, shot.Warning)
		}
		if options.Update && !settled {
			result.Warnings = append(result.Warnings, fmt.Sprintf("the screenshot for %s never settled: no %d captures in a row matched across %d captures; writing the last frame", visualLabel(name, scheme), resolvedSettleStreak(options), resolvedSettleAttempts(options)))
		}
		for previousScheme, previous := range captures {
			if bytes.Equal(previous, shot.PNG) {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%q captured byte-identical images under prefers-color-scheme %q and %q; only one rendering may have been exercised", name, previousScheme, scheme))
				break
			}
		}
		captures[scheme] = append([]byte(nil), shot.PNG...)
		entry := VisualSchemeResult{Scheme: scheme, BaselinePath: absolutePath(baselinePath)}
		baseline, readErr := os.ReadFile(baselinePath)
		if options.Update {
			if err := WriteScreenshotPNG(baselinePath, shot.PNG, options.MaxBytes); err != nil {
				return result, Fatal(fmt.Errorf("write screenshot baseline: %w", err))
			}
			entry.Match, entry.Updated = true, readErr != nil || !bytes.Equal(baseline, shot.PNG)
			result.Updated = result.Updated || entry.Updated
			result.Schemes = append(result.Schemes, entry)
			continue
		}
		if os.IsNotExist(readErr) {
			entry.ActualPath = s.writeVisualArtifact(options.ArtifactDir, name, scheme, "actual", shot.PNG, options.MaxBytes)
			result.Match = false
			result.Schemes = append(result.Schemes, entry)
			return result, Fatal(fmt.Errorf("there is no screenshot baseline for %s; expected %s; captured actual: %s; rerun in update mode after reviewing it", visualLabel(name, scheme), entry.BaselinePath, entry.ActualPath))
		}
		if readErr != nil {
			return result, Fatal(fmt.Errorf("read screenshot baseline %s: %w", baselinePath, readErr))
		}
		if err := validateScreenshotPNG(baseline, options.MaxBytes); err != nil {
			return result, Fatal(fmt.Errorf("invalid screenshot baseline %s: %w", baselinePath, err))
		}
		diff, diffPNG, err := CompareScreenshotPNGs(baseline, shot.PNG, options.Tolerance)
		if err != nil {
			return result, Fatal(err)
		}
		entry.Match, entry.Diff = diff.Match, diff
		if !diff.Match {
			entry.ActualPath = s.writeVisualArtifact(options.ArtifactDir, name, scheme, "actual", shot.PNG, options.MaxBytes)
			entry.DiffPath = s.writeVisualArtifact(options.ArtifactDir, name, scheme, "diff", diffPNG, options.MaxBytes)
			entry.Diagnosis = diff.Diagnose(visualLabel(name, scheme), ScreenshotPaths{Baseline: entry.BaselinePath, Actual: entry.ActualPath, Diff: entry.DiffPath})
			result.Match = false
		}
		result.Schemes = append(result.Schemes, entry)
	}
	return result, nil
}

func (s *Session) captureSettled(ctx context.Context, selector *Selector, captureOptions ScreenshotCaptureOptions, options VisualOptions) (Screenshot, bool, error) {
	attempts, streakTarget, interval := resolvedSettleAttempts(options), resolvedSettleStreak(options), options.SettleInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	var previous, current Screenshot
	streak := 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(screenshotSettleGap(interval, attempt-1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return Screenshot{}, false, ctx.Err()
			case <-timer.C:
			}
		}
		var err error
		current, err = s.captureScreenshot(ctx, selector, captureOptions)
		if err != nil {
			return Screenshot{}, false, err
		}
		if previous.PNG != nil {
			diff, _, compareErr := CompareScreenshotPNGs(previous.PNG, current.PNG, options.Tolerance)
			if compareErr == nil && diff.Match {
				streak++
			} else {
				streak = 1
			}
			if streak >= streakTarget {
				return current, true, nil
			}
		}
		previous = current
	}
	return current, false, nil
}

func screenshotSettleGap(base time.Duration, followup int) time.Duration {
	return base * time.Duration(followup*followup+3*followup+10) / 10
}

func resolvedSettleAttempts(options VisualOptions) int {
	if options.SettleAttempts > 0 {
		return options.SettleAttempts
	}
	return 8
}
func resolvedSettleStreak(options VisualOptions) int {
	if options.SettleStreak > 0 {
		return options.SettleStreak
	}
	return 3
}

func ScreenshotBaselinePath(name, scheme string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("screenshot baseline name must not be empty")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("screenshot baseline name %q must be relative", name)
	}
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("screenshot baseline name %q contains an invalid path segment", name)
		}
		parts[i] = strings.Trim(visualFilenameRE.ReplaceAllString(part, "_"), "_")
		if parts[i] == "" {
			return "", fmt.Errorf("screenshot baseline name %q contains an unusable path segment", name)
		}
	}
	if scheme != "" {
		if scheme != "light" && scheme != "dark" {
			return "", fmt.Errorf("unsupported color scheme %q", scheme)
		}
		parts[len(parts)-1] += "-" + scheme
	}
	parts[len(parts)-1] += ".png"
	return filepath.Join(parts...), nil
}

func WriteScreenshotPNG(path string, data []byte, maxBytes int) error {
	if err := validateScreenshotPNG(data, maxBytes); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(temporary, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func validateScreenshotPNG(data []byte, maxBytes int) error {
	limit := maxBytes
	if limit <= 0 {
		limit = DefaultMaxScreenshotBytes
	}
	if len(data) > limit {
		return fmt.Errorf("screenshot is %d bytes and exceeds the %d-byte limit", len(data), limit)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode PNG: %w", err)
	}
	if uint64(config.Width)*uint64(config.Height) > DefaultMaxScreenshotPixels {
		return fmt.Errorf("screenshot declares %dx%d pixels and exceeds the %d-pixel limit", config.Width, config.Height, DefaultMaxScreenshotPixels)
	}
	return nil
}

func (s *Session) writeVisualArtifact(dir, name, scheme, kind string, data []byte, maxBytes int) string {
	filename := strings.Trim(visualFilenameRE.ReplaceAllString(strings.ReplaceAll(name, "/", "_"), "_"), "_")
	if scheme != "" {
		filename += "-" + scheme
	}
	path := filepath.Join(dir, filename+"."+kind+".png")
	if err := WriteScreenshotPNG(path, data, maxBytes); err != nil {
		return ""
	}
	return absolutePath(path)
}

func handlerCallError(operation string, response HandlerResponse, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if response.Err != "" {
		return fmt.Errorf("%s: %s", operation, response.Err)
	}
	return fmt.Errorf("%s failed", operation)
}

func anySlice(value any) []any { values, _ := value.([]any); return values }

func floatValue(value any) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	}
	return 0
}

func maskScreenshotPNG(data []byte, rects []image.Rectangle) ([]byte, error) {
	img, err := decodeScreenshot(data)
	if err != nil {
		return nil, err
	}
	fill := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	for _, rect := range rects {
		for y := rect.Intersect(img.Bounds()).Min.Y; y < rect.Intersect(img.Bounds()).Max.Y; y++ {
			for x := rect.Intersect(img.Bounds()).Min.X; x < rect.Intersect(img.Bounds()).Max.X; x++ {
				img.SetNRGBA(x, y, fill)
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func visualLabel(name, scheme string) string {
	if scheme == "" {
		return name
	}
	return fmt.Sprintf("%s (prefers-color-scheme: %s)", name, scheme)
}
func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
