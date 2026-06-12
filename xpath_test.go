package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Text and TextContains selectors", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/xpath.html")
		Eventually(b.XPath().WithID("hometown-label")).Should(b.Exist())
	})

	Describe("b.WithText", func() {
		It("selects the element whose text matches exactly", func() {
			Ω(b.WithText("Hometown:")).Should(b.HaveProperty("id", "hometown-label"))
		})

		It("composes with matchers like BeVisible and Exist", func() {
			Eventually(b.WithText("Hometown:")).Should(b.Exist())
			Ω(b.WithText("Hometown:")).Should(b.BeVisible())
		})

		It("composes with actions like Click", func() {
			// clicking a label is fine; we just assert no test failure occurs
			b.Click(b.WithText("Hometown:"))
		})

		It("returns the first match when multiple elements share the same text", func() {
			// both age-label and age-label-2 have text "Age:" — first is returned
			Ω(b.WithText("Age:")).Should(b.HaveProperty("id", "age-label"))
		})

		It("does not match on a partial string", func() {
			Ω(b.HasElement(b.WithText("ometow"))).Should(BeFalse())
		})

		It("fails the spec when no element matches", func() {
			b.Click(b.WithText("no such text"))
			ExpectFailures(ContainSubstring("no such text"))
		})
	})

	Describe("b.WithTextContains", func() {
		It("selects an element whose text contains the substring", func() {
			Ω(b.WithTextContains("ometow")).Should(b.HaveProperty("id", "hometown-label"))
		})

		It("composes with matchers like Exist", func() {
			Eventually(b.WithTextContains("ometow")).Should(b.Exist())
		})

		It("does not match when the substring is absent", func() {
			Ω(b.HasElement(b.WithTextContains("zzzzzz"))).Should(BeFalse())
		})

		It("fails the spec when no element matches", func() {
			b.Click(b.WithTextContains("no-such-substring"))
			ExpectFailures(ContainSubstring("no-such-substring"))
		})
	})
})

var _ = DescribeTable("Xpath DSL",
	func(path biloba.XPath, expectedId string) {
		b.Navigate(fixtureServer + "/xpath.html")
		Ω(path).Should(b.HaveProperty("id", expectedId))
	},
	func(path biloba.XPath, _ string) string {
		return path.String()
	},
	//empty and specific tag variants
	Entry(nil, b.XPath().WithAttr("type", "number"), "age-input"),
	Entry(nil, b.XPath("input").WithAttr("type", "number"), "age-input"),
	Entry(nil, b.XPath("input").HasAttr("disabled"), "phone-input"),

	//WithClass combined with different tags
	Entry(nil, b.XPath().WithClass("highlight"), "age-label"),
	Entry(nil, b.XPath("label").WithClass("highlight"), "age-label"),
	Entry(nil, b.XPath("input").WithClass("highlight"), "age-input"),

	//WithID
	Entry(nil, b.XPath("input").WithID("hometown-input"), "hometown-input"),

	//attr start/contain
	Entry(nil, b.XPath("input").WithAttrStartsWith("id", "hometown"), "hometown-input"),
	Entry(nil, b.XPath("input").WithAttrContains("id", "wn-in"), "hometown-input"),

	//text start/contain
	Entry(nil, b.XPath().WithText("Hometown:"), "hometown-label"),
	Entry(nil, b.XPath().WithTextStartsWith("Hometown"), "hometown-label"),
	Entry(nil, b.XPath().WithTextContains("ometow"), "hometown-label"),

	//with class
	Entry(nil, b.XPath("div").WithClass("fish"), "aquarium"),
	Entry(nil, b.XPath("div").WithClass("otter"), "aquarium"),
	Entry(nil, b.XPath("div").WithClass("octopus"), "aquarium"),

	//boolean logic
	Entry(nil, b.XPath("div").WithAttr("name", "habitat").WithClass("tiger"), "zoo"),
	Entry(nil, b.XPath("div").WithAttr("name", "habitat"), "aquarium"),
	Entry(nil, b.XPath("div").WithAttr("name", "habitat").Not(
		b.XPredicate().WithClass("otter"),
	), "zoo"),
	Entry(nil, b.XPath("div").And(
		b.XPredicate().WithAttr("name", "habitat"),
		b.XPredicate().WithClass("bear"),
	), "zoo"),
	Entry(nil, b.XPath("div").And(
		b.XPredicate().WithAttr("name", "habitat"),
	).Or(
		b.XPredicate().WithClass("octopus"),
		b.XPredicate().WithClass("bear"),
	), "aquarium"),
	Entry(nil, b.XPath("div").Or(
		b.XPredicate().And(
			b.XPredicate().WithClass("red"),
			b.XPredicate().WithText("Error"),
		),
		b.XPredicate().And(
			b.XPredicate().WithClass("orange"),
			b.XPredicate().WithText("Warning"),
		),
	).Not(b.XPredicate().HasAttr("fire-drill")), "orange-flag"),

	//indexing
	Entry(nil, b.XPath("div").WithAttr("name", "habitat").First(), "aquarium"),
	Entry(nil, b.XPath("div").WithAttr("name", "habitat").Nth(2), "zoo"),
	Entry(nil, b.XPath("div").WithAttr("name", "habitat").Last(), "rainforest"),
	Entry(nil, b.XPath("div").WithClass("habitats").Descendant().First(), "common-habitats"),
	Entry(nil, b.XPath("div").WithClass("habitats").Descendant().Nth(4), "obscure-habitats"),
	Entry(nil, b.XPath("div").WithClass("habitats").Descendant().Last(), "all-microbiota"),

	// navigating the tree
	// - descendant
	Entry(nil, b.XPath("div").WithID("reference").Descendant(), "all-habitats"),
	Entry(nil, b.XPath("div").WithID("reference").Descendant().WithAttr("name", "habitat"), "aquarium"),
	Entry(nil, b.XPath("div").WithID("reference").Descendant("ul"), "all-microbiota"),
	Entry(nil, b.XPath("div").WithID("reference").Descendant().WithAttr("color", "blue"), "common-habitats"),

	// - child
	Entry(nil, b.XPath("div").WithID("reference").Child(), "all-habitats"),
	Entry(nil, b.XPath("div").WithID("reference").Child("ul"), "all-languages"),

	// - parent
	Entry(nil, b.XPath("li").WithID("critters").Parent(), "all-microbiota"),
	Entry(nil, b.XPath("li").WithText("Engli\"sh").Parent(), "all-languages"),

	// - ancestor
	Entry(nil, b.XPath("li").WithID("critters").Ancestor(), "critters"),
	Entry(nil, b.XPath("li").WithID("critters").AncestorNotSelf(), "all-microbiota"),
	Entry(nil, b.XPath("li").WithID("critters").Ancestor("div"), "all-habitats"),
	Entry(nil, b.XPath("li").WithID("critters").Ancestor("div").Nth(2), "reference"),

	// - siblings
	Entry(nil, b.XPath().WithID("zoo").FollowingSibling(), "obscure-habitats"),
	Entry(nil, b.XPath().WithID("zoo").FollowingSibling("ul"), "all-microbiota"),
	Entry(nil, b.XPath("li").WithID("english").FollowingSibling("li"), "spanish"),
	Entry(nil, b.XPath("li").WithID("english").FollowingSibling("li").Last(), "arabic"),
	Entry(nil, b.XPath("li").WithID("english").PrecedingSibling("li"), "french"),
	Entry(nil, b.XPath("li").WithID("english").PrecedingSibling("li").Last(), "swedish"),

	// - whith child matching
	Entry(nil, b.XPath("ul").WithChildMatching(b.RelativeXPath("li").WithText("Francais")), "all-languages"),
	Entry(nil, b.XPath("ul").WithChildMatching(
		b.RelativeXPath("li").Or(
			b.XPredicate().WithText("Francais"),
			b.XPredicate().WithText("Germs"),
		)), "all-microbiota"),
)
