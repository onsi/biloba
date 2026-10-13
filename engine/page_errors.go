package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
)

// PageError is an uncaught exception or unhandled promise rejection, as Runtime.exceptionThrown
// reports it.  Line and Column are zero-based, as Chrome reports them.
type PageError struct {
	Message   string
	URL       string
	Line      int64
	Column    int64
	Stack     []ConsoleStackFrame
	Timestamp time.Time
}

// NewPageError converts a Runtime.exceptionThrown event into a bounded PageError.  Message reads the
// way DevTools prints it - "Uncaught TypeError: x is not a function", "Uncaught (in promise) Error:
// boom" - and leaves the stack to Stack.  It needs no session, so every client renders a page error the
// same way.
func NewPageError(event *runtime.EventExceptionThrown) PageError {
	pageError := PageError{Stack: []ConsoleStackFrame{}}
	if event.Timestamp != nil {
		pageError.Timestamp = event.Timestamp.Time()
	}
	details := event.ExceptionDetails
	if details == nil {
		return pageError
	}
	pageError.Message = truncateUTF8(pageErrorMessage(details), DefaultConsolePreviewBytes)
	pageError.Stack = ConsoleStack(details.StackTrace)
	pageError.URL = truncateUTF8(details.URL, DefaultConsolePreviewBytes)
	pageError.Line, pageError.Column = details.LineNumber, details.ColumnNumber
	if pageError.URL == "" && len(pageError.Stack) > 0 {
		top := pageError.Stack[0]
		pageError.URL, pageError.Line, pageError.Column = top.URL, top.Line, top.Column
	}
	return pageError
}

func pageErrorMessage(details *runtime.ExceptionDetails) string {
	text := strings.TrimSpace(details.Text)
	description := ""
	if details.Exception != nil {
		description = details.Exception.Description
		if description == "" {
			description = fmt.Sprint(safeConsolePreview(details.Exception))
		}
	}
	// An Error's description is its stack: the message, then one "    at ..." line per frame.  The
	// frames are in Stack already.
	description, _, _ = strings.Cut(description, "\n    at ")
	description = strings.TrimSpace(description)
	switch {
	case description == "" || strings.HasSuffix(text, description):
		return text
	case text == "":
		return description
	default:
		return text + " " + description
	}
}
