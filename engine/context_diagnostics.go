package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
)

type DiagnosticsPurpose string

const (
	DiagnosticsPurposeFailure  DiagnosticsPurpose = "failure"
	DiagnosticsPurposeProgress DiagnosticsPurpose = "progress"
	DiagnosticsPurposeOnDemand DiagnosticsPurpose = "on-demand"
)

type ViewportSize struct {
	Width  int
	Height int
}

type DiagnosticsCaptureOptions struct {
	Purpose     DiagnosticsPurpose
	Name        string
	Screenshots bool
	Outlines    bool
	Viewport    *ViewportSize
	MaxBytes    int
	// IncludeScreenshotBytes retains the same bounded PNG bytes used for artifact output.
	IncludeScreenshotBytes bool
}

type DiagnosticsArtifactError struct {
	Artifact string
	Code     ErrorCode
	Message  string
}

type TabDiagnostics struct {
	TargetID       target.ID
	Title          string
	ScreenshotPath string
	Screenshot     []byte
	OutlinePath    string
	DOMOutline     string
	Errors         []DiagnosticsArtifactError
}

type ContextDiagnostics struct {
	Purpose     DiagnosticsPurpose
	ArtifactDir string
	Tabs        []TabDiagnostics
}

// CaptureContextDiagnostics captures the live page tabs associated with this session's isolated
// context. Purpose is metadata for runner integrations; capture behavior is otherwise identical for
// failures, progress reports, and explicit on-demand requests.
func (s *Session) CaptureContextDiagnostics(ctx context.Context, options DiagnosticsCaptureOptions) (ContextDiagnostics, error) {
	result := ContextDiagnostics{Purpose: options.Purpose, ArtifactDir: s.artifactDir, Tabs: []TabDiagnostics{}}
	if options.Purpose != DiagnosticsPurposeFailure && options.Purpose != DiagnosticsPurposeProgress && options.Purpose != DiagnosticsPurposeOnDemand {
		return result, &Error{Code: CodeInvalidArgument, Operation: "capture context diagnostics", Message: "purpose must be failure, progress, or on-demand", Observed: options.Purpose}
	}
	if options.Viewport != nil && (options.Viewport.Width <= 0 || options.Viewport.Height <= 0) {
		return result, &Error{Code: CodeInvalidArgument, Operation: "capture context diagnostics", Message: "viewport width and height must be positive", Observed: *options.Viewport}
	}

	tabs, err := s.contextRoot().Tabs(ctx)
	if err != nil {
		return result, err
	}
	sort.Slice(tabs, func(i, j int) bool {
		if tabs[i] == s.contextRoot() {
			return tabs[j] != s.contextRoot()
		}
		if tabs[j] == s.contextRoot() {
			return false
		}
		return tabs[i].targetID < tabs[j].targetID
	})
	for _, tab := range tabs {
		tabResult := tab.captureContextTabDiagnostics(ctx, options)
		result.Tabs = append(result.Tabs, tabResult)
		if ctx.Err() != nil {
			return result, contextError("capture context diagnostics", ctx.Err())
		}
	}
	return result, nil
}

func (s *Session) captureContextTabDiagnostics(ctx context.Context, options DiagnosticsCaptureOptions) TabDiagnostics {
	result := TabDiagnostics{TargetID: s.targetID, Errors: []DiagnosticsArtifactError{}}
	if !options.Screenshots && !options.Outlines {
		return result
	}
	err := s.serial(ctx, "capture context diagnostics tab", func(opCtx context.Context) error {
		var originalWidth, originalHeight int64
		if options.Viewport != nil {
			var readErr error
			originalWidth, originalHeight, readErr = ViewportDimensionsContext(opCtx)
			if readErr != nil {
				return readErr
			}
			s.restoreViewport = &ViewportSize{Width: int(originalWidth), Height: int(originalHeight)}
			defer func() {
				restoreCtx, cancel := context.WithTimeout(s.ctx, time.Second)
				defer cancel()
				if restoreErr := s.applyViewport(restoreCtx, int(originalWidth), int(originalHeight)); restoreErr != nil {
					result.Errors = append(result.Errors, diagnosticsError("viewport-restore", restoreErr))
				} else {
					s.restoreViewport = nil
				}
			}()
			if resizeErr := s.applyViewport(opCtx, options.Viewport.Width, options.Viewport.Height); resizeErr != nil {
				return resizeErr
			}
		}

		if title, titleErr := TitleContext(opCtx); titleErr == nil {
			result.Title = title
		} else {
			result.Errors = append(result.Errors, diagnosticsError("title", titleErr))
		}
		if options.Outlines {
			if installErr := s.ensureBiloba(opCtx); installErr != nil {
				result.Errors = append(result.Errors, diagnosticsError("outline", installErr))
			} else if response, outlineErr := s.runHandler(opCtx, "outline", Selector{}); outlineErr != nil {
				result.Errors = append(result.Errors, diagnosticsError("outline", outlineErr))
			} else {
				result.DOMOutline, _ = response.Result.(string)
				result.DOMOutline = capOutline(result.DOMOutline)
				path, writeErr := writeDiagnosticsArtifact(s.artifactDir, options, s.targetID, "outline.txt", []byte(result.DOMOutline))
				if writeErr != nil {
					result.Errors = append(result.Errors, diagnosticsError("outline-write", writeErr))
				} else {
					result.OutlinePath = path
				}
			}
		}
		if options.Screenshots {
			image, captureErr := CapturePageContext(opCtx, nil)
			if captureErr != nil {
				result.Errors = append(result.Errors, diagnosticsError("screenshot", captureErr))
			} else if validationErr := validateScreenshotPNG(image, options.MaxBytes); validationErr != nil {
				result.Errors = append(result.Errors, diagnosticsError("screenshot", validationErr))
			} else {
				if options.IncludeScreenshotBytes {
					result.Screenshot = append([]byte(nil), image...)
				}
				if s.artifactDir != "" {
					path, writeErr := writeDiagnosticsArtifact(s.artifactDir, options, s.targetID, "png", image)
					if writeErr != nil {
						result.Errors = append(result.Errors, diagnosticsError("screenshot-write", writeErr))
					} else {
						result.ScreenshotPath = path
					}
				} else if !options.IncludeScreenshotBytes {
					_, writeErr := writeDiagnosticsArtifact(s.artifactDir, options, s.targetID, "png", image)
					result.Errors = append(result.Errors, diagnosticsError("screenshot-write", writeErr))
				}
			}
		}
		return nil
	})
	if err != nil {
		result.Errors = append(result.Errors, diagnosticsError("tab", err))
	}
	return result
}

func diagnosticsError(artifact string, err error) DiagnosticsArtifactError {
	code := CodeActionFailed
	var engineErr *Error
	if errors.As(err, &engineErr) {
		code = engineErr.Code
	} else {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			code = CodeIO
		} else if errors.Is(err, context.Canceled) {
			code = CodeCanceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			code = CodeDeadline
		}
	}
	return DiagnosticsArtifactError{Artifact: artifact, Code: code, Message: err.Error()}
}

var unsafeDiagnosticsName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func writeDiagnosticsArtifact(dir string, options DiagnosticsCaptureOptions, targetID target.ID, suffix string, data []byte) (string, error) {
	if dir == "" {
		return "", errors.New("artifact directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := strings.Trim(unsafeDiagnosticsName.ReplaceAllString(options.Name, "-"), "-_")
	if name == "" {
		name = "capture"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	targetName := string(targetID)
	if len(targetName) > 12 {
		targetName = targetName[:12]
	}
	pattern := fmt.Sprintf("diagnostics-%s-%s-%s-*.", options.Purpose, name, targetName)
	file, err := os.CreateTemp(dir, pattern+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	keep = true
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}
