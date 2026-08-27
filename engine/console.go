package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
)

type ConsoleMessage struct {
	Type      string
	Text      string
	Args      []any
	Timestamp time.Time
}

func (s *Session) ConsoleMessages() []ConsoleMessage {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	return append([]ConsoleMessage(nil), s.consoleMessages...)
}

func (s *Session) recordConsoleMessage(event *runtime.EventConsoleAPICalled) {
	message := ConsoleMessage{Type: string(event.Type)}
	if event.Timestamp != nil {
		message.Timestamp = event.Timestamp.Time()
	}
	parts := make([]string, 0, len(event.Args))
	for _, object := range event.Args {
		var value any
		if len(object.Value) > 0 && json.Unmarshal(object.Value, &value) == nil {
			message.Args = append(message.Args, value)
			parts = append(parts, fmt.Sprint(value))
		} else {
			value = object.Description
			message.Args = append(message.Args, value)
			parts = append(parts, fmt.Sprint(value))
		}
	}
	message.Text = strings.Join(parts, " ")
	s.consoleMu.Lock()
	s.consoleMessages = append(s.consoleMessages, message)
	s.consoleMu.Unlock()
}

func (s *Session) clearConsoleMessages() {
	s.consoleMu.Lock()
	s.consoleMessages = nil
	s.consoleMu.Unlock()
}
