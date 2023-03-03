package biloba

import (
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/types"
)

type DialogType = page.DialogType

var DialogTypeAlert = page.DialogTypeAlert
var DialogTypeBeforeunload = page.DialogTypeBeforeunload
var DialogTypeConfirm = page.DialogTypeConfirm
var DialogTypePrompt = page.DialogTypePrompt

// has to be on b
var handlerCounter uint

type DialogHandler struct {
	Type           DialogType
	MessageMatcher types.GomegaMatcher
	Response       bool
	Text           *string
	id             uint
}

func (d *DialogHandler) MatchingMessage(message any) *DialogHandler {
	d.MessageMatcher = matcherOrEqual(message)
	return d
}

func (d *DialogHandler) WithResponse(r bool) *DialogHandler {
	d.Response = r
	return d
}

func (d *DialogHandler) WithText(text string) *DialogHandler {
	d.Text = &text
	d.Response = true
	return d
}

func (d *DialogHandler) Match(dialog *Dialog) bool {
	if dialog.Type != d.Type {
		return false
	}
	if d.MessageMatcher == nil {
		return true
	}
	match, err := d.MessageMatcher.Match(dialog.Message)
	return match && (err == nil)
}

type Dialog struct {
	Type           page.DialogType
	Message        string
	DefaultPrompt  string
	HandleResponse bool
	HandleText     string
	Autohandled    bool
}

type Dialogs []*Dialog

func (d Dialogs) MostRecent() *Dialog {
	if len(d) == 0 {
		return nil
	}
	return d[len(d)-1]
}
func (d Dialogs) OfType(t DialogType) Dialogs {
	out := Dialogs{}
	for _, dialog := range d {
		if dialog.Type == t {
			out = append(out, dialog)
		}
	}
	return out
}
func (d Dialogs) MatchingMessage(message any) Dialogs {
	matcher := matcherOrEqual(message)
	out := Dialogs{}
	for _, dialog := range d {
		match, err := matcher.Match(dialog.Message)
		if match && err == nil {
			out = append(out, dialog)
		}
	}
	return out
}

func (b *Biloba) handleEventJavascriptDialogOpening(ev *page.EventJavascriptDialogOpening) {
	defer b.gt.GinkgoRecover()
	d := &Dialog{
		Type:          ev.Type,
		Message:       ev.Message,
		DefaultPrompt: ev.DefaultPrompt,
	}
	response := d.Type == DialogTypeBeforeunload
	text := ""
	var handler *DialogHandler
	b.lock.Lock()
	for i := len(b.dialogHandlers) - 1; i >= 0; i-- {
		if b.dialogHandlers[i].Match(d) {
			handler = b.dialogHandlers[i]
			break
		}
	}
	if handler != nil {
		d.HandleResponse = handler.Response
		if d.HandleResponse {
			if handler.Text != nil {
				d.HandleText = *(handler.Text)
			} else {
				d.HandleText = d.DefaultPrompt
			}
		}
		response = d.HandleResponse
		text = d.HandleText
	} else {
		d.HandleResponse = response
		d.HandleText = text
		d.Autohandled = true
	}
	b.dialogs = append(b.dialogs, d)
	b.lock.Unlock()
	if handler == nil {
		b.gt.Printf(b.gt.F("Biloba automatically handled an {{red}}unhandled dialog{{/}} - you should add an explicit dialog handler: %s - %s", d.Type, d.Message))
	}
	go func() {
		action := page.HandleJavaScriptDialog(response)
		if text != "" {
			action = action.WithPromptText(text)
		}
		chromedp.Run(b.Context, action)
	}()
}

func (b *Biloba) HandleAlertDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{Type: DialogTypeAlert})
}

func (b *Biloba) HandleBeforeunloadDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{Type: DialogTypeBeforeunload})
}

func (b *Biloba) HandleConfirmDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{Type: DialogTypeConfirm})
}

func (b *Biloba) HandlePromptDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{Type: DialogTypePrompt})
}

func (b *Biloba) addDialogHandler(handler *DialogHandler) *DialogHandler {
	b.lock.Lock()
	handlerCounter += 1
	handler.id = handlerCounter
	b.dialogHandlers = append(b.dialogHandlers, handler)
	b.lock.Unlock()
	return handler
}

func (b *Biloba) RemoveDialogHandler(handler *DialogHandler) {
	handlers := []*DialogHandler{}
	b.lock.Lock()
	for _, h := range b.dialogHandlers {
		if h.id != handler.id {
			handlers = append(handlers, h)
		}
	}
	b.dialogHandlers = handlers
	b.lock.Unlock()
}

func (b *Biloba) Dialogs() Dialogs {
	b.lock.Lock()
	defer b.lock.Unlock()
	return append(Dialogs{}, b.dialogs...)
}
