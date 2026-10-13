package biloba

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/onsi/biloba/engine"
)

func (b *Biloba) handleEventConsoleAPICalled(ev *runtime.EventConsoleAPICalled) {
	b.recordConsoleMessage(ev)
	var color string
	showStackTrace := ev.StackTrace != nil && len(ev.StackTrace.CallFrames) > 1
	switch ev.Type {
	case runtime.APITypeLog:
		color = "{{/}}"
		showStackTrace = false
	case runtime.APITypeDebug:
		color = "{{light-gray}}"
		showStackTrace = false
	case runtime.APITypeInfo:
		color = "{{cyan}}"
		showStackTrace = false
	case runtime.APITypeError:
		color = "{{red}}"
		showStackTrace = showStackTrace && true
	case runtime.APITypeWarning:
		color = "{{coral}}"
		showStackTrace = showStackTrace && true
	case runtime.APITypeAssert:
		color = "{{red}}{{bold}}"
		showStackTrace = showStackTrace && true
	default:
		return
	}
	out := []string{}
	length := 0
	for _, arg := range ev.Args {
		rendition := b.renderRemoteObject(arg)
		out = append(out, rendition)
		length += len(rendition)
	}
	message := ""
	if length > 80 {
		message = out[0] + "\n"
		for _, component := range out[1:] {
			message += b.gt.Fiw(5, 80, "%s\n", component)
		}
	} else {
		message = strings.Join(out, " - ") + "\n"
	}
	b.gt.Printf(b.gt.F("{{gray}}[%s] "+color+"%s{{/}}", ev.Timestamp.Time().Format("15:04"), message))
	if showStackTrace {
		b.gt.Printf(b.gt.Fi(1, b.renderStackTrace(ev.StackTrace)))
	}
	if ev.Type == runtime.APITypeError || ev.Type == runtime.APITypeAssert {
		b.lock.Lock()
		b.state.consoleErrors = append(b.state.consoleErrors, strings.TrimSuffix(message, "\n"))
		b.lock.Unlock()
	}
	if ev.Type == runtime.APITypeAssert {
		defer func() { recover() }()
		b.gt.Fatalf("Detected console.assert failure:\n%s", message)
	}
}

func (b *Biloba) renderRemoteObject(obj *runtime.RemoteObject) string {
	if len(obj.Value) > 0 {
		return string(obj.Value)
	} else {
		out := ""
		if obj.Preview == nil {
			out += "<nil>"
		} else if obj.Preview.Subtype == "array" {
			out += "["
			for i, property := range obj.Preview.Properties {
				out += fmt.Sprintf("%v", property.Value)
				if i < len(obj.Preview.Properties)-1 {
					out += ", "
				}
			}
			out += "]"
		} else {
			out += "{"
			for i, property := range obj.Preview.Properties {
				out += fmt.Sprintf("%s: %s", property.Name, property.Value)
				if i < len(obj.Preview.Properties)-1 {
					out += ", "
				}
			}
			out += "}"
		}
		return out
	}
}

func (b *Biloba) renderStackTrace(stackTrace *runtime.StackTrace) string {
	out := "{{bold}}Stack Trace{{/}}\n"
	for _, frame := range stackTrace.CallFrames {
		out += fmt.Sprintf("%s {{gray}}%s:%d{{/}}\n", frame.FunctionName, frame.URL, frame.LineNumber)
	}
	return out
}

/*
StackFrame is one frame of a page stack trace attached to a [ConsoleMessage] or [PageError].  Line and Column are zero-based, as Chrome reports them.

Read https://onsi.github.io/biloba/#console-messages-and-page-errors to learn more about console messages and page errors
*/
type StackFrame struct {
	URL          string
	FunctionName string
	Line         int
	Column       int
}

func newStack(frames []engine.ConsoleStackFrame) []StackFrame {
	out := make([]StackFrame, len(frames))
	for i, frame := range frames {
		out[i] = StackFrame{URL: frame.URL, FunctionName: frame.FunctionName, Line: int(frame.Line), Column: int(frame.Column)}
	}
	return out
}

/*
ConsoleMessage is one call the page made to the console API.  Type is the console method ("log", "info", "warning", "error", "debug", "assert", ...).  Text joins the previews of the call's arguments with a space; the TypeScript client renders it the same way.

Read https://onsi.github.io/biloba/#console-messages-and-page-errors to learn more about console messages and page errors
*/
type ConsoleMessage struct {
	Type      string
	Text      string
	Timestamp time.Time
	Stack     []StackFrame
}

/*
PageError is an uncaught exception or unhandled promise rejection.  Message reads the way DevTools prints it ("Uncaught TypeError: x is not a function", "Uncaught (in promise) Error: boom"); URL, Line, and Column locate where it was thrown (Line and Column are zero-based, as Chrome reports them).

Read https://onsi.github.io/biloba/#console-messages-and-page-errors to learn more about console messages and page errors
*/
type PageError struct {
	Message   string
	URL       string
	Line      int
	Column    int
	Stack     []StackFrame
	Timestamp time.Time
}

/*
AllConsoleMessages() returns every console message this tab (or frame) has logged since the last call to Prepare(), oldest first.  It is a snapshot and does not poll.  A frame handle returns only its own document's messages; a tab returns those of its frames too.  Biloba keeps the most recent 1000.

Read https://onsi.github.io/biloba/#console-messages-and-page-errors to learn more about console messages and page errors
*/
func (b *Biloba) AllConsoleMessages() []ConsoleMessage {
	b.gt.Helper()
	b.guardConfig("AllConsoleMessages")
	b.lock.Lock()
	defer b.lock.Unlock()
	out := make([]ConsoleMessage, len(b.state.consoleMessages))
	for i, message := range b.state.consoleMessages {
		out[i] = message
		out[i].Stack = slices.Clone(message.Stack)
	}
	return out
}

/*
AllPageErrors() returns every uncaught exception and unhandled promise rejection this tab (or frame) has seen since the last call to Prepare(), oldest first.  It is a snapshot and does not poll.  A frame handle returns only its own document's errors; a tab returns those of its frames too.  Biloba keeps the most recent 1000.

Page errors also stream to the GinkgoWriter, and are replayed with the console errors when a spec fails.

Read https://onsi.github.io/biloba/#console-messages-and-page-errors to learn more about console messages and page errors
*/
func (b *Biloba) AllPageErrors() []PageError {
	b.gt.Helper()
	b.guardConfig("AllPageErrors")
	b.lock.Lock()
	defer b.lock.Unlock()
	out := make([]PageError, len(b.state.pageErrors))
	for i, pageError := range b.state.pageErrors {
		out[i] = pageError
		out[i].Stack = slices.Clone(pageError.Stack)
	}
	return out
}

// appendBounded appends v, dropping the oldest entries so the history stays within
// engine.DefaultEventHistoryLimit - the bound the engine keeps for its own console history.
func appendBounded[T any](history []T, v T) []T {
	if len(history) >= engine.DefaultEventHistoryLimit {
		history = slices.Delete(history, 0, len(history)-engine.DefaultEventHistoryLimit+1)
	}
	return append(history, v)
}

// recordConsoleMessage adds a console message to this tab's (or frame's) history without streaming it.
func (b *Biloba) recordConsoleMessage(ev *runtime.EventConsoleAPICalled) {
	message := engine.NewConsoleMessage(ev)
	b.lock.Lock()
	defer b.lock.Unlock()
	b.state.consoleMessages = appendBounded(b.state.consoleMessages, ConsoleMessage{
		Type: message.Type, Text: message.Text, Timestamp: message.Timestamp, Stack: newStack(message.Stack),
	})
}

// recordPageError adds an uncaught exception to this tab's (or frame's) history without streaming it.
func (b *Biloba) recordPageError(ev *runtime.EventExceptionThrown) PageError {
	pageError := engine.NewPageError(ev)
	out := PageError{
		Message: pageError.Message, URL: pageError.URL, Line: int(pageError.Line), Column: int(pageError.Column),
		Stack: newStack(pageError.Stack), Timestamp: pageError.Timestamp,
	}
	b.lock.Lock()
	defer b.lock.Unlock()
	b.state.pageErrors = appendBounded(b.state.pageErrors, out)
	return out
}

// handleEventExceptionThrown records an uncaught exception, streams it to the GinkgoWriter in red the
// way console.error streams, and adds it to the console errors replayed when the spec fails.
func (b *Biloba) handleEventExceptionThrown(ev *runtime.EventExceptionThrown) {
	pageError := b.recordPageError(ev)
	message := pageError.Message
	if pageError.URL != "" {
		message += fmt.Sprintf(" (%s:%d:%d)", pageError.URL, pageError.Line, pageError.Column)
	}
	timestamp := pageError.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	b.gt.Printf(b.gt.F("{{gray}}[%s] {{red}}%s{{/}}\n", timestamp.Format("15:04"), message))
	// like console.error, a one-frame stack adds nothing to the location already printed
	if ev.ExceptionDetails != nil && ev.ExceptionDetails.StackTrace != nil && len(ev.ExceptionDetails.StackTrace.CallFrames) > 1 {
		b.gt.Printf(b.gt.Fi(1, b.renderStackTrace(ev.ExceptionDetails.StackTrace)))
	}
	b.lock.Lock()
	b.state.consoleErrors = append(b.state.consoleErrors, message)
	b.lock.Unlock()
}
