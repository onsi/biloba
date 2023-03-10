package biloba

import (
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/types"
)

/*
DialogType is used to distinguish between different types of Dialogs
*/
type DialogType = page.DialogType

const DialogTypeAlert = page.DialogTypeAlert
const DialogTypeBeforeunload = page.DialogTypeBeforeunload
const DialogTypeConfirm = page.DialogTypeConfirm
const DialogTypePrompt = page.DialogTypePrompt

var handlerCounter uint

/*
DialogHandler is returned by Biloba's Handle*Dialogs methods

Use DialogHandler's methods to configure how you want Biloba to handle the response.

Read https://onsi.github.io/biloba/#handling-dialogs to learn more
*/
type DialogHandler struct {
	dialogType     DialogType
	messageMatcher types.GomegaMatcher
	response       bool
	text           *string
	id             uint
}

/*
Set MatchingMessage to only handle dialogs whose message matches

You can pass in a string or Gomega matcher
*/
func (d *DialogHandler) MatchingMessage(message any) *DialogHandler {
	d.messageMatcher = matcherOrEqual(message)
	return d
}

/*
WithResponse controls whether Biloba should accept or decline the dialog
*/
func (d *DialogHandler) WithResponse(r bool) *DialogHandler {
	d.response = r
	return d
}

/*
WithText controls what text Biloba should provide to prompt dialogs

If none i provided the default prompt given by the browser is used
*/
func (d *DialogHandler) WithText(text string) *DialogHandler {
	d.text = &text
	d.response = true
	return d
}

func (d *DialogHandler) match(dialog *Dialog) bool {
	if dialog.Type != d.dialogType {
		return false
	}
	if d.messageMatcher == nil {
		return true
	}
	match, err := d.messageMatcher.Match(dialog.Message)
	return match && (err == nil)
}

/*
Dialog represents a Dialog handled by Biloba

Read https://onsi.github.io/biloba/#inspecting-handled-dialogs to learn more
*/
type Dialog struct {
	Type           page.DialogType
	Message        string
	DefaultPrompt  string
	HandleResponse bool
	HandleText     string
	//Autohandled is true if Biloba's default handlers handled this dialog
	Autohandled bool
}

type Dialogs []*Dialog

/*
MostRecent() returns the last element of Dialogs
*/
func (d Dialogs) MostRecent() *Dialog {
	if len(d) == 0 {
		return nil
	}
	return d[len(d)-1]
}

/*
OfType() filters Dialogs by DialogType
*/
func (d Dialogs) OfType(t DialogType) Dialogs {
	out := Dialogs{}
	for _, dialog := range d {
		if dialog.Type == t {
			out = append(out, dialog)
		}
	}
	return out
}

/*
OfType() filters Dialogs by message.  You may pass in a string or Gomega matcher
*/
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
		if b.dialogHandlers[i].match(d) {
			handler = b.dialogHandlers[i]
			break
		}
	}
	if handler != nil {
		d.HandleResponse = handler.response
		if d.HandleResponse {
			if handler.text != nil {
				d.HandleText = *(handler.text)
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

/*
HandleAlertDialogs() registers an alert DialogHandler

Read https://onsi.github.io/biloba/#handling-dialogs to learn more about handling dialogs
*/
func (b *Biloba) HandleAlertDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{dialogType: DialogTypeAlert})
}

/*
HandleBeforeunloadDialogs() registers an beforeunload DialogHandler

Read https://onsi.github.io/biloba/#handling-dialogs to learn more about handling dialogs
*/
func (b *Biloba) HandleBeforeunloadDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{dialogType: DialogTypeBeforeunload})
}

/*
HandleConfirmDialogs() registers a confirm DialogHandler

Read https://onsi.github.io/biloba/#handling-dialogs to learn more about handling dialogs
*/
func (b *Biloba) HandleConfirmDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{dialogType: DialogTypeConfirm})
}

/*
HandlePromptDialogs() registers a prompt Dialoghandler

Read https://onsi.github.io/biloba/#handling-dialogs to learn more about handling dialogs
*/
func (b *Biloba) HandlePromptDialogs() *DialogHandler {
	return b.addDialogHandler(&DialogHandler{dialogType: DialogTypePrompt})
}

func (b *Biloba) addDialogHandler(handler *DialogHandler) *DialogHandler {
	b.lock.Lock()
	handlerCounter += 1
	handler.id = handlerCounter
	b.dialogHandlers = append(b.dialogHandlers, handler)
	b.lock.Unlock()
	return handler
}

/*
Pass RemoveDialogHandler() a handler returned by one of the Handle*Dialogs methods to unregister it

Read https://onsi.github.io/biloba/#handling-dialogs to learn more about handling dialogs
*/
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

/*
Dialogs() returns all dialogs handled by this Biloba tab in this spec

Read https://onsi.github.io/biloba/#inspecting-handled-dialogs to learn more about inspecting handled dialogs
*/
func (b *Biloba) Dialogs() Dialogs {
	b.lock.Lock()
	defer b.lock.Unlock()
	return append(Dialogs{}, b.dialogs...)
}
