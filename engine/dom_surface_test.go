package engine_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("typed DOM engine surface", func() {
	It("normalizes the rendered text, not the raw DOM tree", func(ctx SpecContext) {
		// Go's HaveText is normalizeWhitespace(innerText) - the rendered, visible text.  Normalizing
		// textContent instead silently changes the answer: hidden children and <script> bodies come
		// back, and CSS text-transform is ignored, which is exactly what docs/index.md warns about.
		session := openDOMSurfaceSession(ctx)

		normalized, err := session.TextByMode(ctx, engine.CSS("#texts"), engine.NormalizedText)
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized.Value).To(Equal("visible SHOUT"), "hidden text must stay hidden and text-transform must be honored")

		raw, err := session.TextByMode(ctx, engine.CSS("#texts"), engine.InnerText)
		Expect(err).NotTo(HaveOccurred())
		Expect(raw.Value).To(ContainSubstring("SHOUT"))

		content, err := session.TextByMode(ctx, engine.CSS("#texts"), engine.TextContent)
		Expect(err).NotTo(HaveOccurred())
		Expect(content.Value).To(ContainSubstring("SECRET"), "textContent keeps its own layout-independent meaning")

		each, err := session.Texts(ctx, engine.CSS("#texts"), engine.NormalizedText)
		Expect(err).NotTo(HaveOccurred())
		Expect(each.Value).To(Equal([]any{"visible SHOUT"}))
	})

	It("encodes extended semantic locators and custom test-id attributes", func() {
		Expect(engine.Label("Email", engine.Contains).Encoded()).To(ContainSubstring(`"by":"label"`))
		Expect(engine.Placeholder("you@", engine.Contains).Description()).To(Equal(`getByPlaceholder("you@", contains)`))
		Expect(engine.AltText("Portrait", engine.Exact).Encoded()).To(ContainSubstring(`"by":"alttext"`))
		Expect(engine.Title("Ada", engine.Contains).Encoded()).To(ContainSubstring(`"by":"title"`))
		Expect(engine.TestIDAttribute("email", "data-qa").Encoded()).To(ContainSubstring(`"attr":"data-qa"`))
	})

	It("reads typed DOM values, collections, state, geometry, and style", func(ctx SpecContext) {
		session := openDOMSurfaceSession(ctx)

		labelValue, err := session.Value(ctx, engine.Label("Email", engine.Contains))
		Expect(err).NotTo(HaveOccurred())
		Expect(labelValue.Value).To(Equal("Ada"))
		placeholderValue, err := session.Value(ctx, engine.Placeholder("you@", engine.Contains))
		Expect(err).NotTo(HaveOccurred())
		Expect(placeholderValue.Value).To(Equal("Ada"))
		customTestIDValue, err := session.Value(ctx, engine.TestIDAttribute("email", "data-qa"))
		Expect(err).NotTo(HaveOccurred())
		Expect(customTestIDValue.Value).To(Equal("Ada"))

		text, err := session.TextContent(ctx, engine.CSS("#target"))
		Expect(err).NotTo(HaveOccurred())
		Expect(text.Value).To(Equal("target text"))
		texts, err := session.Texts(ctx, engine.CSS(".item"), engine.InnerText)
		Expect(err).NotTo(HaveOccurred())
		Expect(texts.Value).To(Equal([]any{"One", "Two"}))
		classes, err := session.Classes(ctx, engine.CSS("#action"))
		Expect(err).NotTo(HaveOccurred())
		Expect(classes.Value).To(Equal([]any{"primary", "wide"}))
		attributes, err := session.Attributes(ctx, engine.CSS("#action"), []engine.NameSpec{engine.RequiredName("data-note"), engine.OptionalName("missing")})
		Expect(err).NotTo(HaveOccurred())
		Expect(attributes.Value).To(Equal(map[string]any{"data-note": "memo", "missing": nil}))
		jsonValue, err := session.JSONAttribute(ctx, engine.CSS("#action"), "data-json")
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonValue.Value).To(Equal(map[string]any{"ready": true}))
		properties, err := session.Properties(ctx, engine.CSS("#email"), []engine.NameSpec{engine.RequiredName("value"), engine.OptionalName("missing")})
		Expect(err).NotTo(HaveOccurred())
		Expect(properties.Value).To(Equal(map[string]any{"value": "Ada", "missing": nil}))
		values, err := session.Values(ctx, engine.CSS("input"))
		Expect(err).NotTo(HaveOccurred())
		Expect(values.Value).To(HaveLen(2))
		checked, err := session.State(ctx, engine.CSS("#check"), engine.StateChecked)
		Expect(err).NotTo(HaveOccurred())
		Expect(checked.Value).To(BeFalse())

		box, err := session.BoundingBox(ctx, engine.CSS("#target"))
		Expect(err).NotTo(HaveOccurred())
		Expect(box.Width).To(Equal(80.0))
		offset, err := session.OffsetWithin(ctx, engine.CSS("#target"), engine.CSS("#scroll"))
		Expect(err).NotTo(HaveOccurred())
		Expect(offset.Top).To(BeNumerically(">", 100))
		style, err := session.ComputedStyle(ctx, engine.CSS("#hover"), "display")
		Expect(err).NotTo(HaveOccurred())
		Expect(style.Value).To(Equal("block"))
		inViewport, err := session.InViewport(ctx, engine.CSS("#target"), false)
		Expect(err).NotTo(HaveOccurred())
		Expect(inViewport.Value).To(BeTrue())
		order, err := session.DocumentOrder(ctx, engine.CSS("#email"), engine.CSS("#action"))
		Expect(err).NotTo(HaveOccurred())
		Expect(order).To(Equal(engine.Before))
	})

	It("performs typed DOM mutations, selection, element JavaScript, and fast input", func(ctx SpecContext) {
		session := openDOMSurfaceSession(ctx)

		Expect(session.SetValue(ctx, engine.CSS("#choice"), engine.OptionLabel("Beta"))).To(Succeed())
		Expect(session.SetProperty(ctx, engine.CSS("#action"), "dataset.changed", "yes", engine.FirstMatch)).To(Succeed())
		changed, err := session.Property(ctx, engine.CSS("#action"), "dataset.changed")
		Expect(err).NotTo(HaveOccurred())
		Expect(changed.Value).To(Equal("yes"))
		Expect(session.Focus(ctx, engine.CSS("#email"))).To(Succeed())
		focused, err := session.State(ctx, engine.CSS("#email"), engine.StateFocused)
		Expect(err).NotTo(HaveOccurred())
		Expect(focused.Value).To(BeTrue())
		Expect(session.Blur(ctx, engine.CSS("#email"))).To(Succeed())
		Expect(session.Hover(ctx, engine.CSS("#hover"), engine.Fast)).To(Succeed())
		Expect(session.Hover(ctx, engine.CSS("#hover"), engine.Realistic)).To(Succeed())
		hoverColor, err := session.ComputedStyle(ctx, engine.CSS("#hover"), "color")
		Expect(err).NotTo(HaveOccurred())
		Expect(hoverColor.Value).To(Equal("rgb(1, 2, 3)"))
		Expect(session.ClickWith(ctx, engine.CSS("#action"), engine.ClickOptions{Button: engine.LeftButton, Count: 2, Modifiers: engine.ShiftModifier, Mode: engine.Fast})).To(Succeed())
		Expect(session.ClickWith(ctx, engine.CSS("#action"), engine.ClickOptions{Button: engine.RightButton, Count: 1, Mode: engine.Fast})).To(Succeed())
		Expect(session.Tap(ctx, engine.CSS("#action"), engine.PointerOptions{Mode: engine.Fast})).To(Succeed())
		Expect(session.ScrollIntoView(ctx, engine.CSS("#target"), engine.ScrollIntoViewOptions{Container: engine.CSS("#scroll"), TopOffset: 5, HasTopOffset: true})).To(Succeed())
		Expect(session.ScrollWheel(ctx, engine.CSS("#scroll"), 0, 10, engine.Fast)).To(Succeed())
		Expect(session.Select(ctx, engine.CSS("#selection"), engine.Selection{Substring: "alpha", Occurrence: 2})).To(Succeed())
		selected, err := session.Evaluate(ctx, `window.getSelection().toString()`)
		Expect(err).NotTo(HaveOccurred())
		Expect(selected).To(Equal("alpha"))
		result, err := session.InvokeMethod(ctx, engine.CSS("#action"), "customEcho", "ok")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Value).To(Equal("action:ok"))
		Expect(session.ClearSelection(ctx)).To(Succeed())
	})

	It("rejects invalid typed options before dispatch", func(ctx SpecContext) {
		session := openDOMSurfaceSession(ctx)
		Expect(session.ClickWith(ctx, engine.CSS("#action"), engine.ClickOptions{Count: 0})).To(MatchError(ContainSubstring("click count must be positive")))
		_, err := session.TextByMode(ctx, engine.CSS("#action"), engine.TextMode("script"))
		Expect(err).To(MatchError(ContainSubstring("unsupported text mode")))
		Expect(session.Select(ctx, engine.CSS("#selection"), engine.Selection{Start: 5, End: 2, Range: true})).To(MatchError(ContainSubstring("selection end")))
	})
})

func openDOMSurfaceSession(ctx context.Context) *engine.Session {
	GinkgoHelper()
	session, err := browser.OpenSession(ctx)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(session.Close)
	Expect(session.Navigate(ctx, server.URL+"/dom-surface")).To(Succeed())
	return session
}
