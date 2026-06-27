package biloba

import (
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Key represents a single named key (e.g. Enter, Tab, Escape) that can be sent to the browser via [Biloba.Type] or [Biloba.SendKeysToWindowImmediately].  Use the [Keys] namespace to access the available keys:

	b.Type("input", biloba.Keys.Enter)

A Key is distinct from a selector so [Biloba.Type] can tell whether its first argument targets an element or is itself a key to type into the focused element.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
type Key string

/*
Keys is a namespace of the named keys you can send with [Biloba.Type] or [Biloba.SendKeysToWindowImmediately].  The values mirror chromedp's keyboard runes (the [github.com/chromedp/chromedp/kb] package):

	b.Type("input.search", biloba.Keys.Enter)         // type Enter into the search box
	b.SendKeysToWindowImmediately(biloba.Keys.Escape) // send Escape to whatever is focused (or the window)

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

// buildKeys concatenates a keyboard payload - strings and named [Keys] mixed in any order - into the
// single key string that chromedp dispatches.  It is the shared payload builder for [Biloba.Type] and
// [Biloba.SendKeysToWindowImmediately] and fails the spec (returning ok==false) on any other type.
func (b *Biloba) buildKeys(method string, args []any) (string, bool) {
	b.gt.Helper()
	keys := ""
	for _, k := range args {
		switch v := k.(type) {
		case Key:
			keys += string(v)
		case string:
			keys += v
		default:
			b.gt.Fatalf("%s received an invalid key of type %T - keys must be strings or biloba.Keys", method, k)
			return "", false
		}
	}
	return keys, true
}

/*
Type() sends text - and named [Keys] - to an element as real keyboard input.  Unlike [Biloba.SetValue] (which sets the value directly and dispatches input/change events) Type focuses the element and dispatches genuine keydown/keypress/keyup events for each character - exercising apps that are wired to real key events (search-as-you-type, editors, hotkeys).  Type is the element-targeted keyboard method: pass plain strings, named [Keys], and held modifiers in any mix.

Type() has two modes of operation, chosen by its arguments (after held modifiers are stripped out):

When the first argument is a selector followed by a payload (two or more arguments) Type targets that element - focusing the first element matching selector and typing the payload into it:

	b.Type("input.search", "hello")                    // type "hello"
	b.Type("input.search", "hello", biloba.Keys.Enter) // type "hello" then press Enter
	b.Type("input.search", biloba.Keys.Enter)          // press Enter into the search box (e.g. submit a form)

Like Biloba's other action methods Type polls by default: it keeps trying until the element exists, is visible, and is enabled (use [Biloba.Immediate] to act once and fail fast, or [Biloba.WithTimeout] to bound the wait).

When called with just a payload - a single string, or one or more named [Keys] - Type returns a Gomega matcher you poll yourself:

	Eventually("input.search").Should(b.Type("hello"))
	Eventually("#editor").Should(b.Type(biloba.Keys.Enter))

You can hold keyboard modifiers ([Biloba.Shift], [Biloba.Ctrl], [Biloba.Alt], [Biloba.Meta]) down while typing - handy for hotkeys like Cmd-A (select all):

	b.Type("input.search", "a", b.Meta())
	Eventually("input.search").Should(b.Type("a", b.Meta()))

Note: the matcher form cannot mix leading text with trailing keys - b.Type("hello", biloba.Keys.Enter) is read as the immediate form with selector "hello".  That's fine: the immediate form now polls, so it already covers that case; reach for the matcher form only when you need a custom Consistently or composition.

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) Type(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	rest, mods := splitModifiers(args)
	if len(rest) == 0 {
		b.gt.Fatalf("Type requires text or keys to type")
		return nil
	}

	firstIsSelector := false
	switch rest[0].(type) {
	case string, XPath:
		firstIsSelector = true
	}

	// immediate form: a selector followed by a payload (selector + two-or-more args)
	if firstIsSelector && len(rest) >= 2 {
		selector := rest[0]
		keys, ok := b.buildKeys("Type", rest[1:])
		if !ok {
			return nil
		}
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.focusAndSendKeys(selector, keys, mods)
		}).WithMessage("be typable")
		b.pollOrImmediate(selector, matcher)
		return nil
	}

	// matcher form: just a payload (one string, or one-or-more named Keys)
	keys, ok := b.buildKeys("Type", rest)
	if !ok {
		return nil
	}
	b.guardBareMatcher("Type")
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.focusAndSendKeys(selector, keys, mods)
	}).WithMessage("be typable")
}

/*
SendKeysToWindowImmediately() sends one or more named [Keys] (such as Enter, Escape, or a function key) - and plain text - as real keyboard events, focus-free: the keys land on whichever element currently has focus, or, if nothing is focused, fire against document/window (the path for global hotkeys).  There is no selector form - to type into a specific element use [Biloba.Type], which focuses it first.

	b.Click("#editor")
	b.SendKeysToWindowImmediately(biloba.Keys.Escape) // Escape to the focused #editor
	b.SendKeysToWindowImmediately("/")                // a "/" hotkey handled at the document level

Hold keyboard modifiers ([Biloba.Shift], [Biloba.Ctrl], [Biloba.Alt], [Biloba.Meta]) down by passing them alongside the keys (in any position) - so an app reading e.shiftKey/e.metaKey in a keydown handler sees them:

	b.SendKeysToWindowImmediately(biloba.Keys.Enter, b.Meta()) // Cmd-Enter to the focused element

As the name says, SendKeysToWindowImmediately acts immediately and never polls - only you know what should be focused when it fires.  When the target appears asynchronously, gate it on a readiness anchor first:

	Eventually("input.search").Should(b.BeFocused()) // wait until it really has focus
	b.SendKeysToWindowImmediately(biloba.Keys.Enter) // then send once

Read https://onsi.github.io/biloba/#keyboard-input to learn more about keyboard input
*/
func (b *Biloba) SendKeysToWindowImmediately(args ...any) {
	b.gt.Helper()
	b.guardConfig("SendKeysToWindowImmediately")
	rest, mods := splitModifiers(args)
	if len(rest) == 0 {
		b.gt.Fatalf("SendKeysToWindowImmediately requires at least one key to send")
		return
	}
	keys, ok := b.buildKeys("SendKeysToWindowImmediately", rest)
	if !ok {
		return
	}
	if err := b.dispatchKeys(keys, mods); err != nil {
		b.gt.Fatalf("Failed to send keys:\n%s", err.Error())
	}
}
