package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/runtime"
)

const (
	// DefaultEventHistoryLimit bounds retained console messages and warnings per session.
	DefaultEventHistoryLimit = 1000
	// DefaultConsolePreviewBytes bounds each console value and stack string retained by the engine.
	DefaultConsolePreviewBytes = 4096
	defaultConsoleStackFrames  = 64
)

// ConsoleStackFrame is one bounded page stack frame attached to a console event.
type ConsoleStackFrame struct {
	URL          string
	FunctionName string
	Line         int64
	Column       int64
}

type ConsoleMessage struct {
	Type       string
	Text       string
	Args       []any
	Timestamp  time.Time
	Stack      []ConsoleStackFrame
	Generation uint64
}

// ConsoleHistory is a bounded snapshot with the number of evicted messages.
type ConsoleHistory struct {
	Messages []ConsoleMessage
	Dropped  uint64
}

type consoleSubscriber struct {
	events  chan ConsoleMessage
	dropped atomic.Uint64
}

// ConsoleSubscription delivers console events without blocking Chrome's event listener.
type ConsoleSubscription struct {
	session *Session
	id      uint64
	events  <-chan ConsoleMessage
	dropped *atomic.Uint64
	once    sync.Once
}

// Events returns the bounded event channel.
func (s *ConsoleSubscription) Events() <-chan ConsoleMessage { return s.events }

// Dropped reports events discarded because the subscriber's buffer was full.
func (s *ConsoleSubscription) Dropped() uint64 { return s.dropped.Load() }

// Close removes the subscription and closes its event channel. It is idempotent.
func (s *ConsoleSubscription) Close() error {
	s.once.Do(func() {
		if s.session != nil {
			s.session.removeConsoleSubscription(s.id)
		}
	})
	return nil
}

// SubscribeConsole registers a session-isolated bounded console event stream.
func (s *Session) SubscribeConsole(buffer int) (*ConsoleSubscription, error) {
	if buffer <= 0 {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "subscribe console", Message: "buffer must be positive", Observed: buffer}
	}
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if !s.eventsEnabled.Load() {
		return nil, &Error{Code: CodeSessionClosed, Operation: "subscribe console", Message: "session is closed"}
	}
	if s.consoleSubs == nil {
		s.consoleSubs = map[uint64]*consoleSubscriber{}
	}
	s.consoleSubSeq++
	subscriber := &consoleSubscriber{events: make(chan ConsoleMessage, buffer)}
	s.consoleSubs[s.consoleSubSeq] = subscriber
	return &ConsoleSubscription{session: s, id: s.consoleSubSeq, events: subscriber.events, dropped: &subscriber.dropped}, nil
}

func (s *Session) removeConsoleSubscription(id uint64) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if subscriber, ok := s.consoleSubs[id]; ok {
		delete(s.consoleSubs, id)
		close(subscriber.events)
	}
}

func (s *Session) ConsoleMessages() []ConsoleMessage { return s.ConsoleSnapshot().Messages }

// ConsoleSnapshot returns retained messages and their eviction count.
func (s *Session) ConsoleSnapshot() ConsoleHistory {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	return ConsoleHistory{Messages: append([]ConsoleMessage(nil), s.consoleMessages...), Dropped: s.consoleDropped}
}

func (s *Session) recordConsoleMessage(event *runtime.EventConsoleAPICalled) {
	message := ConsoleMessage{Type: string(event.Type), Generation: s.eventGeneration.Load(), Stack: []ConsoleStackFrame{}}
	if event.Timestamp != nil {
		message.Timestamp = event.Timestamp.Time()
	}
	parts := make([]string, 0, len(event.Args))
	for index, object := range event.Args {
		if index >= 64 {
			message.Args = append(message.Args, "… [arguments truncated]")
			parts = append(parts, "… [arguments truncated]")
			break
		}
		value := safeConsolePreview(object)
		message.Args = append(message.Args, value)
		parts = append(parts, fmt.Sprint(value))
	}
	message.Text = strings.Join(parts, " ")
	if event.StackTrace != nil {
		for index, frame := range event.StackTrace.CallFrames {
			if index == defaultConsoleStackFrames {
				break
			}
			message.Stack = append(message.Stack, ConsoleStackFrame{
				URL:          truncateUTF8(frame.URL, DefaultConsolePreviewBytes),
				FunctionName: truncateUTF8(frame.FunctionName, DefaultConsolePreviewBytes),
				Line:         frame.LineNumber, Column: frame.ColumnNumber,
			})
		}
	}

	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if !s.eventsEnabled.Load() || message.Generation != s.eventGeneration.Load() {
		return
	}
	if len(s.consoleMessages) == DefaultEventHistoryLimit {
		copy(s.consoleMessages, s.consoleMessages[1:])
		s.consoleMessages[len(s.consoleMessages)-1] = message
		s.consoleDropped++
	} else {
		s.consoleMessages = append(s.consoleMessages, message)
	}
	for _, subscriber := range s.consoleSubs {
		select {
		case subscriber.events <- message:
		default:
			subscriber.dropped.Add(1)
		}
	}
}

func safeConsolePreview(object *runtime.RemoteObject) any {
	if len(object.Value) > 0 && len(object.Value) <= DefaultConsolePreviewBytes {
		var value any
		if json.Unmarshal(object.Value, &value) == nil {
			return value
		}
	}
	if object.Type == runtime.TypeString && len(object.Value) > DefaultConsolePreviewBytes {
		value := string(object.Value)
		value = strings.TrimPrefix(value, `"`)
		value = strings.TrimSuffix(value, `"`)
		return truncateUTF8(value, DefaultConsolePreviewBytes)
	}
	description := object.Description
	if description == "" {
		description = string(object.Type)
	}
	return truncateUTF8(description, DefaultConsolePreviewBytes)
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "… [truncated]"
}

func (s *Session) clearConsoleMessages() {
	s.consoleMu.Lock()
	s.consoleMessages = nil
	s.consoleDropped = 0
	s.consoleMu.Unlock()
}

func (s *Session) closeConsoleSubscriptions() {
	s.consoleMu.Lock()
	for id, subscriber := range s.consoleSubs {
		delete(s.consoleSubs, id)
		close(subscriber.events)
	}
	s.consoleMu.Unlock()
}
