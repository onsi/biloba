package biloba

import (
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Key represents a single named key (e.g. Enter, Tab, Escape) that can be sent to the browser via [Biloba.SendKeys].  Use the [Keys] namespace to access the available keys:

	b.SendKeys("input", biloba.Keys.Enter)

A Key is distinct from a selector so [Biloba.SendKeys] can tell whether its first argument targets an element or is itself a key to send to the focused element.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
type Key string

/*
Keys is a namespace of the named keys you can send with [Biloba.SendKeys].  The values mirror chromedp's keyboard runes (the [github.com/chromedp/chromedp/kb] package):

	b.SendKeys("input.search", biloba.Keys.Enter)
	b.SendKeys(biloba.Keys.Escape) // sent to the currently focused element

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
var Keys = struct {
	Backspace  Key
	Tab        Key
	Enter      Key
	Escape     Key
	Delete     Key
	ArrowDown  Key
	ArrowLeft  Key
	ArrowRight Key
	ArrowUp    Key
	End        Key
	Home       Key
	PageDown   Key
	PageUp     Key
}{
	Backspace:  Key(kb.Backspace),
	Tab:        Key(kb.Tab),
	Enter:      Key(kb.Enter),
	Escape:     Key(kb.Escape),
	Delete:     Key(kb.Delete),
	ArrowDown:  Key(kb.ArrowDown),
	ArrowLeft:  Key(kb.ArrowLeft),
	ArrowRight: Key(kb.ArrowRight),
	ArrowUp:    Key(kb.ArrowUp),
	End:        Key(kb.End),
	Home:       Key(kb.Home),
	PageDown:   Key(kb.PageDown),
	PageUp:     Key(kb.PageUp),
}

// focusAndSendKeys focuses the element matching selector (failing if it is missing, hidden, or
// disabled) then dispatches keys as real keyboard events via chromedp.  The element is resolved
// fresh in the browser so keydown/keypress/keyup all fire.
func (b *Biloba) focusAndSendKeys(selector any, keys string) (bool, error) {
	r := b.runBilobaHandler("focus", selector)
	if r.Error() != nil {
		return false, r.Error()
	}
	if !r.Success {
		return false, nil
	}
	if keys == "" {
		return true, nil
	}
	if err := chromedp.Run(b.Context, chromedp.KeyEvent(keys)); err != nil {
		return false, err
	}
	return true, nil
}

/*
Type() sends text to an element as real keyboard input.  Unlike [Biloba.SetValue] (which sets the value directly and dispatches input/change events) Type focuses the element and dispatches genuine keydown/keypress/keyup events for each character - exercising apps that are wired to real key events (search-as-you-type, editors, hotkeys).

Type() has two modes of operation:

When invoked with a selector and text:

	b.Type("input.search", "hello")

it immediately focuses the first element matching selector and types text into it.  The element must exist, be visible, and be enabled - otherwise the spec fails.

When invoked with just text, Type returns a Gomega matcher that succeeds once an element is found, focusable, and the text has been typed:

	Eventually("input.search").Should(b.Type("hello"))

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) Type(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 2 {
		success, err := b.focusAndSendKeys(args[0], toString(args[1]))
		if err != nil {
			b.gt.Fatalf("Failed to type:\n%s", err.Error())
		} else if !success {
			b.gt.Fatalf("Failed to type: element is not visible or enabled")
		}
		return nil
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.focusAndSendKeys(selector, toString(args[0]))
	}).WithMessage("be typable")
}

/*
SendKeys() sends one or more named keys (such as Enter, Tab, or Escape) as real keyboard events.  Use the [Keys] namespace for the available keys.

SendKeys() has two modes of operation:

When the first argument is a selector it focuses that element and then sends the remaining keys to it:

	b.SendKeys("input.search", biloba.Keys.Enter)        // type Enter into the search box (e.g. to submit a form)
	b.SendKeys("textarea", "x", biloba.Keys.Backspace)   // mix text and named keys

When called with only keys (no selector) the keys are sent to whichever element currently has focus:

	b.Click("#editor")
	b.SendKeys(biloba.Keys.Escape) // send Escape to the focused element

SendKeys fails the spec if a selector is provided but no matching element is found, or if the element is hidden or disabled.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) SendKeys(args ...any) {
	b.gt.Helper()
	if len(args) == 0 {
		b.gt.Fatalf("SendKeys requires at least one key to send")
		return
	}

	var selector any
	keyArgs := args
	switch args[0].(type) {
	case Key:
		// no selector - send to the focused element
	case XPath:
		selector = args[0]
		keyArgs = args[1:]
	case string:
		selector = args[0]
		keyArgs = args[1:]
	default:
		b.gt.Fatalf("SendKeys received an invalid first argument of type %T - it must be a selector or a biloba.Key", args[0])
		return
	}

	keys := ""
	for _, k := range keyArgs {
		switch v := k.(type) {
		case Key:
			keys += string(v)
		case string:
			keys += v
		default:
			b.gt.Fatalf("SendKeys received an invalid key of type %T - keys must be strings or biloba.Keys", k)
			return
		}
	}

	if selector != nil {
		success, err := b.focusAndSendKeys(selector, keys)
		if err != nil {
			b.gt.Fatalf("Failed to send keys:\n%s", err.Error())
		} else if !success {
			b.gt.Fatalf("Failed to send keys: element is not visible or enabled")
		}
		return
	}

	if err := chromedp.Run(b.Context, chromedp.KeyEvent(keys)); err != nil {
		b.gt.Fatalf("Failed to send keys:\n%s", err.Error())
	}
}
