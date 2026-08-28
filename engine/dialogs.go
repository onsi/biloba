package engine

import (
	"context"
	"fmt"

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
	if selected == nil {
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
	s.dialogMu.Unlock()
	go func() {
		action := page.HandleJavaScriptDialog(accept)
		if dialog.Type == DialogPrompt && accept {
			action = action.WithPromptText(dialog.PromptText)
		}
		_ = chromedp.Run(s.ctx, action)
	}()
}
