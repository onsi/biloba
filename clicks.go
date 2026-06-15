package biloba

/*
ClickModifier represents a keyboard modifier held down during a click (e.g. shift-click, ctrl-click).  Use the exported [ModShift], [ModControl], [ModAlt], and [ModMeta] constants with [Biloba.ClickWith]:

	b.ClickWith("#row", biloba.ModShift)
	b.ClickWith("#row", biloba.ModControl, biloba.ModMeta)

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
type ClickModifier string

const (
	ModShift   ClickModifier = "shift"
	ModControl ClickModifier = "control"
	ModAlt     ClickModifier = "alt"
	ModMeta    ClickModifier = "meta" // Command on macOS, the Windows key elsewhere
)

/*
ClickWith() clicks the first element matching selector with the given keyboard modifiers held down (e.g. shift-click, ctrl-click).  The modifiers come from the [ModShift], [ModControl], [ModAlt], and [ModMeta] constants ([ModMeta] is Command on macOS, the Windows key elsewhere):

	tab.ClickWith("#row", biloba.ModShift)
	tab.ClickWith("#row", biloba.ModControl, biloba.ModMeta)

it immediately clicks (fast mode dispatches mousedown/mouseup/click events carrying the modifier flags; realistic mode dispatches a real click with the modifiers held down).  It fails if no element is found, or if the element is hidden or disabled.

Unlike Click, ClickWith has no matcher variant.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) ClickWith(selector any, modifiers ...ClickModifier) {
	b.gt.Helper()
	if b.realistic {
		if ok, err := b.realisticClickWith(selector, modifiers); err != nil {
			b.gt.Fatalf("Failed to click:\n%s", err.Error())
		} else if !ok {
			b.gt.Fatalf("Failed to click: element is not clickable (it is disabled, off-screen, or obscured by another element)")
		}
		return
	}
	mods := make([]string, len(modifiers))
	for i, m := range modifiers {
		mods[i] = string(m)
	}
	r := b.runBilobaHandler("clickWith", selector, mods)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to click:\n%s", r.Error())
	}
}
