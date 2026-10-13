package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	// Generation is the session event generation the error was recorded in (see Session.Prepare).
	Generation uint64
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

// PageErrorHistory is a bounded snapshot of a session's page errors with the number evicted.
type PageErrorHistory struct {
	Errors  []PageError
	Dropped uint64
}

type pageErrorSubscriber struct {
	events  chan PageError
	dropped atomic.Uint64
}

// PageErrorSubscription delivers page errors without blocking Chrome's event listener.
type PageErrorSubscription struct {
	session *Session
	id      uint64
	events  <-chan PageError
	dropped *atomic.Uint64
	once    sync.Once
}

// Events returns the bounded event channel.
func (s *PageErrorSubscription) Events() <-chan PageError { return s.events }

// Dropped reports events discarded because the subscriber's buffer was full.
func (s *PageErrorSubscription) Dropped() uint64 { return s.dropped.Load() }

// Close removes the subscription and closes its event channel. It is idempotent.
func (s *PageErrorSubscription) Close() error {
	s.once.Do(func() { s.session.removePageErrorSubscription(s.id) })
	return nil
}

// SubscribePageErrors registers a session-isolated bounded page-error stream.
func (s *Session) SubscribePageErrors(buffer int) (*PageErrorSubscription, error) {
	if buffer <= 0 {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "subscribe page errors", Message: "buffer must be positive", Observed: buffer}
	}
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if !s.eventsEnabled.Load() {
		return nil, &Error{Code: CodeSessionClosed, Operation: "subscribe page errors", Message: "session is closed"}
	}
	if s.pageErrorSubs == nil {
		s.pageErrorSubs = map[uint64]*pageErrorSubscriber{}
	}
	s.consoleSubSeq++
	subscriber := &pageErrorSubscriber{events: make(chan PageError, buffer)}
	s.pageErrorSubs[s.consoleSubSeq] = subscriber
	return &PageErrorSubscription{session: s, id: s.consoleSubSeq, events: subscriber.events, dropped: &subscriber.dropped}, nil
}

func (s *Session) removePageErrorSubscription(id uint64) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if subscriber, ok := s.pageErrorSubs[id]; ok {
		delete(s.pageErrorSubs, id)
		close(subscriber.events)
	}
}

// PageErrors returns the uncaught exceptions and unhandled rejections the session has seen since its
// last Prepare, oldest first.  A frame handle keeps only its own document's.
func (s *Session) PageErrors() []PageError { return s.PageErrorSnapshot().Errors }

// PageErrorSnapshot returns retained page errors and their eviction count.
func (s *Session) PageErrorSnapshot() PageErrorHistory {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	out := make([]PageError, len(s.pageErrors))
	for i, pageError := range s.pageErrors {
		out[i] = pageError
		out[i].Stack = append([]ConsoleStackFrame{}, pageError.Stack...)
	}
	return PageErrorHistory{Errors: out, Dropped: s.pageErrorsDropped}
}

// recordPageError keeps a Runtime.exceptionThrown event.  It runs on the event listener and makes no
// CDP calls.
func (s *Session) recordPageError(event *runtime.EventExceptionThrown) {
	generation := s.eventGeneration.Load()
	pageError := NewPageError(event)
	pageError.Generation = generation

	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if !s.eventsEnabled.Load() || generation != s.eventGeneration.Load() {
		return
	}
	if len(s.pageErrors) == DefaultEventHistoryLimit {
		copy(s.pageErrors, s.pageErrors[1:])
		s.pageErrors[len(s.pageErrors)-1] = pageError
		s.pageErrorsDropped++
	} else {
		s.pageErrors = append(s.pageErrors, pageError)
	}
	for _, subscriber := range s.pageErrorSubs {
		select {
		case subscriber.events <- pageError:
		default:
			subscriber.dropped.Add(1)
		}
	}
}

func (s *Session) clearPageErrors() {
	s.consoleMu.Lock()
	s.pageErrors, s.pageErrorsDropped = nil, 0
	s.consoleMu.Unlock()
}
