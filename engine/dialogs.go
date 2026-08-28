package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type DialogType string

const (
	DialogAlert        DialogType = "alert"
	DialogBeforeUnload DialogType = "beforeunload"
	DialogConfirm      DialogType = "confirm"
	DialogPrompt       DialogType = "prompt"
)

type DialogHandlerOptions struct {
	Type       DialogType
	Message    *Expectation
	Accept     bool
	PromptText *string
}
type DialogHandler struct{ ID string }
type Dialog struct {
	Type                   DialogType
	Message, DefaultPrompt string
	Accepted               bool
	PromptText             string
	AutoHandled            bool
}

type WarningCode string

const WarningDialogAutoHandled WarningCode = "dialog_auto_handled"

// DefaultWarningPreviewBytes bounds each warning and dialog string retained by the engine.
const DefaultWarningPreviewBytes = 4096

// Warning is a structured, runner-neutral session warning.
type Warning struct {
	Code       WarningCode
	Message    string
	Dialog     Dialog
	Generation uint64
}

// WarningHistory is a bounded snapshot with the number of evicted warnings.
type WarningHistory struct {
	Warnings []Warning
	Dropped  uint64
}

type warningSubscriber struct {
	events  chan Warning
	dropped atomic.Uint64
}

// WarningSubscription delivers warnings without blocking Chrome's event listener.
type WarningSubscription struct {
	session *Session
	id      uint64
	events  <-chan Warning
	dropped *atomic.Uint64
	once    sync.Once
}

// Events returns the bounded event channel.
func (s *WarningSubscription) Events() <-chan Warning { return s.events }

// Dropped reports events discarded because the subscriber's buffer was full.
func (s *WarningSubscription) Dropped() uint64 { return s.dropped.Load() }

// Close removes the subscription and closes its event channel. It is idempotent.
func (s *WarningSubscription) Close() error {
	s.once.Do(func() {
		if s.session != nil {
			s.session.removeWarningSubscription(s.id)
		}
	})
	return nil
}

type DialogQuery struct {
	Type    DialogType
	Message *Expectation
}
type dialogHandlerEntry struct {
	id      string
	options DialogHandlerOptions
}

func (s *Session) RegisterDialogHandler(ctx context.Context, options DialogHandlerOptions) (DialogHandler, error) {
	if options.Type != DialogAlert && options.Type != DialogBeforeUnload && options.Type != DialogConfirm && options.Type != DialogPrompt {
		return DialogHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register dialog handler", Message: "unsupported dialog type", Observed: options.Type}
	}
	if options.Message != nil {
		if _, err := MatchExpectation("", *options.Message); err != nil {
			return DialogHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register dialog handler", Message: err.Error(), Cause: err}
		}
	}
	var result DialogHandler
	err := s.serial(ctx, "register dialog handler", func(context.Context) error {
		s.dialogMu.Lock()
		defer s.dialogMu.Unlock()
		s.dialogSequence++
		id := fmt.Sprintf("%s-dialog-%d", s.targetID, s.dialogSequence)
		s.dialogHandlers = append(s.dialogHandlers, dialogHandlerEntry{id: id, options: options})
		result = DialogHandler{ID: id}
		return nil
	})
	return result, err
}

func (s *Session) RemoveDialogHandler(ctx context.Context, id string) error {
	return s.serial(ctx, "remove dialog handler", func(context.Context) error {
		s.dialogMu.Lock()
		defer s.dialogMu.Unlock()
		for i, handler := range s.dialogHandlers {
			if handler.id == id {
				s.dialogHandlers = append(s.dialogHandlers[:i], s.dialogHandlers[i+1:]...)
				return nil
			}
		}
		return &Error{Code: CodeInvalidArgument, Operation: "remove dialog handler", Message: "dialog handler not found", Observed: id}
	})
}
func (s *Session) Dialogs() []Dialog {
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()
	return append([]Dialog(nil), s.dialogHistory...)
}

// Warnings returns structured warnings in emission order.
func (s *Session) Warnings() []Warning {
	return s.WarningSnapshot().Warnings
}

// WarningSnapshot returns retained warnings and their eviction count.
func (s *Session) WarningSnapshot() WarningHistory {
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()
	return WarningHistory{Warnings: append([]Warning(nil), s.warnings...), Dropped: s.warningsDropped}
}

// SubscribeWarnings registers a session-isolated bounded warning event stream.
func (s *Session) SubscribeWarnings(buffer int) (*WarningSubscription, error) {
	if buffer <= 0 {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "subscribe warnings", Message: "buffer must be positive", Observed: buffer}
	}
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()
	if !s.eventsEnabled.Load() {
		return nil, &Error{Code: CodeSessionClosed, Operation: "subscribe warnings", Message: "session is closed"}
	}
	if s.warningSubs == nil {
		s.warningSubs = map[uint64]*warningSubscriber{}
	}
	s.warningSubSeq++
	subscriber := &warningSubscriber{events: make(chan Warning, buffer)}
	s.warningSubs[s.warningSubSeq] = subscriber
	return &WarningSubscription{session: s, id: s.warningSubSeq, events: subscriber.events, dropped: &subscriber.dropped}, nil
}

func (s *Session) removeWarningSubscription(id uint64) {
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()
	if subscriber, ok := s.warningSubs[id]; ok {
		delete(s.warningSubs, id)
		close(subscriber.events)
	}
}

// DialogsMatching returns dialog history in arrival order after applying query.
func (s *Session) DialogsMatching(query DialogQuery) []Dialog {
	dialogs := s.Dialogs()
	matched := make([]Dialog, 0, len(dialogs))
	for _, dialog := range dialogs {
		if query.Type != "" && dialog.Type != query.Type {
			continue
		}
		if query.Message != nil {
			ok, _ := MatchExpectation(dialog.Message, *query.Message)
			if !ok {
				continue
			}
		}
		matched = append(matched, dialog)
	}
	return matched
}

// MostRecentDialog returns the newest dialog matching query.
func (s *Session) MostRecentDialog(query DialogQuery) (Dialog, bool) {
	dialogs := s.DialogsMatching(query)
	if len(dialogs) == 0 {
		return Dialog{}, false
	}
	return dialogs[len(dialogs)-1], true
}
func (s *Session) clearDialogs() {
	s.dialogMu.Lock()
	s.dialogHandlers = nil
	s.dialogHistory = nil
	s.warnings = nil
	s.warningsDropped = 0
	s.dialogMu.Unlock()
}
func (s *Session) handleDialog(event *page.EventJavascriptDialogOpening) {
	dialog := Dialog{Type: DialogType(event.Type), Message: event.Message, DefaultPrompt: event.DefaultPrompt}
	accept := dialog.Type == DialogBeforeUnload
	if !s.eventsEnabled.Load() {
		go func() { _ = chromedp.Run(s.ctx, page.HandleJavaScriptDialog(accept)) }()
		return
	}
	s.dialogMu.Lock()
	if !s.eventsEnabled.Load() {
		s.dialogMu.Unlock()
		go func() { _ = chromedp.Run(s.ctx, page.HandleJavaScriptDialog(accept)) }()
		return
	}
	var selected *dialogHandlerEntry
	for i := len(s.dialogHandlers) - 1; i >= 0; i-- {
		h := &s.dialogHandlers[i]
		if h.options.Type != dialog.Type {
			continue
		}
		if h.options.Message != nil {
			matched, err := MatchExpectation(dialog.Message, *h.options.Message)
			if err != nil || !matched {
				continue
			}
		}
		selected = h
		break
	}
	autoHandled := selected == nil
	if autoHandled {
		dialog.AutoHandled = true
		dialog.Accepted = accept
	} else {
		dialog.Accepted = selected.options.Accept
		accept = dialog.Accepted
		if accept {
			if selected.options.PromptText != nil {
				dialog.PromptText = *selected.options.PromptText
			} else {
				dialog.PromptText = dialog.DefaultPrompt
			}
		}
	}
	s.dialogHistory = append(s.dialogHistory, dialog)
	if autoHandled {
		s.appendWarningLocked(Warning{
			Code:    WarningDialogAutoHandled,
			Message: fmt.Sprintf("auto-handled %s dialog %q", dialog.Type, dialog.Message),
			Dialog:  dialog,
		})
	}
	s.dialogMu.Unlock()
	go func() {
		action := page.HandleJavaScriptDialog(accept)
		if dialog.Type == DialogPrompt && accept {
			action = action.WithPromptText(dialog.PromptText)
		}
		_ = chromedp.Run(s.ctx, action)
	}()
}

func (s *Session) recordWarning(warning Warning) {
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()
	if !s.eventsEnabled.Load() {
		return
	}
	s.appendWarningLocked(warning)
}

func (s *Session) appendWarningLocked(warning Warning) {
	warning.Message = truncateUTF8(warning.Message, DefaultWarningPreviewBytes)
	warning.Dialog.Message = truncateUTF8(warning.Dialog.Message, DefaultWarningPreviewBytes)
	warning.Dialog.DefaultPrompt = truncateUTF8(warning.Dialog.DefaultPrompt, DefaultWarningPreviewBytes)
	warning.Dialog.PromptText = truncateUTF8(warning.Dialog.PromptText, DefaultWarningPreviewBytes)
	warning.Generation = s.eventGeneration.Load()
	if len(s.warnings) == DefaultEventHistoryLimit {
		copy(s.warnings, s.warnings[1:])
		s.warnings[len(s.warnings)-1] = warning
		s.warningsDropped++
	} else {
		s.warnings = append(s.warnings, warning)
	}
	for _, subscriber := range s.warningSubs {
		select {
		case subscriber.events <- warning:
		default:
			subscriber.dropped.Add(1)
		}
	}
}

func (s *Session) closeWarningSubscriptions() {
	s.dialogMu.Lock()
	for id, subscriber := range s.warningSubs {
		delete(s.warningSubs, id)
		close(subscriber.events)
	}
	s.dialogMu.Unlock()
}

func (s *Session) closeEventSubscriptions() {
	s.closeConsoleSubscriptions()
	s.closeWarningSubscriptions()
}
