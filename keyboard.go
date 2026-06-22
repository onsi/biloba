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

It covers the editing, navigation, lock, and function keys you reach for in a browser test.  For an
exotic key not listed here (media, IME, launch keys) drop down to chromedp via b.Context and the
[github.com/chromedp/chromedp/kb] package.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
var Keys = struct {
	// editing & whitespace
	Backspace Key
	Tab       Key
	Enter     Key
	Escape    Key
	Space     Key
	Delete    Key
	Insert    Key

	// navigation
	ArrowDown  Key
	ArrowLeft  Key
	ArrowRight Key
	ArrowUp    Key
	End        Key
	Home       Key
	PageDown   Key
	PageUp     Key

	// locks
	CapsLock   Key
	NumLock    Key
	ScrollLock Key

	// misc control keys
	ContextMenu Key
	PrintScreen Key
	Pause       Key
	Help        Key
	Clear       Key

	// function keys
	F1  Key
	F2  Key
	F3  Key
	F4  Key
	F5  Key
	F6  Key
	F7  Key
	F8  Key
	F9  Key
	F10 Key
	F11 Key
	F12 Key
	F13 Key
	F14 Key
	F15 Key
	F16 Key
	F17 Key
	F18 Key
	F19 Key
	F20 Key
	F21 Key
	F22 Key
	F23 Key
	F24 Key
}{
	Backspace: Key(kb.Backspace),
	Tab:       Key(kb.Tab),
	Enter:     Key(kb.Enter),
	Escape:    Key(kb.Escape),
	Space:     Key(" "),
	Delete:    Key(kb.Delete),
	Insert:    Key(kb.Insert),

	ArrowDown:  Key(kb.ArrowDown),
	ArrowLeft:  Key(kb.ArrowLeft),
	ArrowRight: Key(kb.ArrowRight),
	ArrowUp:    Key(kb.ArrowUp),
	End:        Key(kb.End),
	Home:       Key(kb.Home),
	PageDown:   Key(kb.PageDown),
	PageUp:     Key(kb.PageUp),

	CapsLock:   Key(kb.CapsLock),
	NumLock:    Key(kb.NumLock),
	ScrollLock: Key(kb.ScrollLock),

	ContextMenu: Key(kb.ContextMenu),
	PrintScreen: Key(kb.PrintScreen),
	Pause:       Key(kb.Pause),
	Help:        Key(kb.Help),
	Clear:       Key(kb.Clear),

	F1:  Key(kb.F1),
	F2:  Key(kb.F2),
	F3:  Key(kb.F3),
	F4:  Key(kb.F4),
	F5:  Key(kb.F5),
	F6:  Key(kb.F6),
	F7:  Key(kb.F7),
	F8:  Key(kb.F8),
	F9:  Key(kb.F9),
	F10: Key(kb.F10),
	F11: Key(kb.F11),
	F12: Key(kb.F12),
	F13: Key(kb.F13),
	F14: Key(kb.F14),
	F15: Key(kb.F15),
	F16: Key(kb.F16),
	F17: Key(kb.F17),
	F18: Key(kb.F18),
	F19: Key(kb.F19),
	F20: Key(kb.F20),
	F21: Key(kb.F21),
	F22: Key(kb.F22),
	F23: Key(kb.F23),
	F24: Key(kb.F24),
}

// splitModifiers peels any b.Shift()/b.Ctrl()/b.Alt()/b.Meta() modifiers out of a keyboard method's
// variadic args, returning the remaining (selector/key/text) args alongside the held modifiers.
// Modifiers may appear in any position.
func splitModifiers(args []any) (rest []any, mods []clickModifier) {
	for _, a := range args {
		if m, ok := a.(Modifier); ok {
			mods = append(mods, m.modifier)
		} else {
			rest = append(rest, a)
		}
	}
	return rest, mods
}

// dispatchKeys sends keys as real keyboard events via chromedp, holding any modifiers down for each
// key (so an app reading e.shiftKey/e.metaKey in a keydown handler sees them).  An empty key string
// is a no-op (callers use it to mean "focus only").
func (b *Biloba) dispatchKeys(keys string, mods []clickModifier) error {
	if keys == "" {
		return nil
	}
	var opts []chromedp.KeyOption
	if len(mods) > 0 {
		opts = append(opts, chromedp.KeyModifiers(modifierMask(mods)))
	}
	return chromedp.Run(b.Context, chromedp.KeyEvent(keys, opts...))
}

// focusAndSendKeys focuses the element matching selector (failing if it is missing, hidden, or
// disabled) then dispatches keys as real keyboard events via chromedp, holding mods down.  The
// element is resolved fresh in the browser so keydown/keypress/keyup all fire.
func (b *Biloba) focusAndSendKeys(selector any, keys string, mods []clickModifier) (bool, error) {
	if b.realistic {
		// realistically bring the element into view before focusing+typing (Playwright focuses
		// inputs via JS too, so the keys path is unchanged - only the scroll is added)
		if sr := b.runBilobaHandler("scrollIntoView", selector); sr.Error() != nil {
			return false, sr.Error()
		}
	}
	r := b.runBilobaHandler("focus", selector)
	if r.Error() != nil {
		return false, r.Error()
	}
	if !r.Success {
		return false, nil
	}
	if err := b.dispatchKeys(keys, mods); err != nil {
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

You can hold keyboard modifiers ([Biloba.Shift], [Biloba.Ctrl], [Biloba.Alt], [Biloba.Meta]) down while typing - handy for hotkeys like Cmd-A (select all):

	b.Type("input.search", "a", b.Meta())

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) Type(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	rest, mods := splitModifiers(args)
	if len(rest) == 2 {
		success, err := b.focusAndSendKeys(rest[0], toString(rest[1]), mods)
		if err != nil {
			b.gt.Fatalf("Failed to type:\n%s", err.Error())
		} else if !success {
			b.gt.Fatalf("Failed to type: element is not visible or enabled")
		}
		return nil
	}
	if len(rest) != 1 {
		b.gt.Fatalf("Type requires text to type")
		return nil
	}
	text := toString(rest[0])
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.focusAndSendKeys(selector, text, mods)
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

Hold keyboard modifiers ([Biloba.Shift], [Biloba.Ctrl], [Biloba.Alt], [Biloba.Meta]) down by passing them alongside the keys (in any position) - so an app reading e.shiftKey/e.metaKey in a keydown handler sees them:

	b.SendKeys("textarea", biloba.Keys.Enter, b.Shift()) // Shift-Enter
	b.SendKeys(biloba.Keys.Enter, b.Meta())              // Cmd-Enter to the focused element

SendKeys fails the spec if a selector is provided but no matching element is found, or if the element is hidden or disabled.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) SendKeys(args ...any) {
	b.gt.Helper()
	rest, mods := splitModifiers(args)
	if len(rest) == 0 {
		b.gt.Fatalf("SendKeys requires at least one key to send")
		return
	}

	var selector any
	keyArgs := rest
	switch rest[0].(type) {
	case Key:
		// no selector - send to the focused element
	case XPath:
		selector = rest[0]
		keyArgs = rest[1:]
	case string:
		selector = rest[0]
		keyArgs = rest[1:]
	default:
		b.gt.Fatalf("SendKeys received an invalid first argument of type %T - it must be a selector or a biloba.Key", rest[0])
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
		success, err := b.focusAndSendKeys(selector, keys, mods)
		if err != nil {
			b.gt.Fatalf("Failed to send keys:\n%s", err.Error())
		} else if !success {
			b.gt.Fatalf("Failed to send keys: element is not visible or enabled")
		}
		return
	}

	if err := b.dispatchKeys(keys, mods); err != nil {
		b.gt.Fatalf("Failed to send keys:\n%s", err.Error())
	}
}
