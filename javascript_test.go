package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Javascript", func() {
	Describe("RunErr", func() {
		Context("when the script succeeds", func() {
			It("returns the result as an unencoded Go object", func() {
				Ω(b.RunErr(`1+2`)).Should(Equal(3.0))
				Ω(b.RunErr(`["a","b","c","d"].map((c, i) => c + (i+1))`)).Should(HaveExactElements("a1", "b2", "c3", "d4"))
				Ω(b.RunErr(`var a = {foo:1, bar:true, baz:"ibbl"}; a`)).Should(SatisfyAll(
					HaveKeyWithValue("foo", 1.0),
					HaveKeyWithValue("bar", BeTrue()),
					HaveKeyWithValue("baz", ContainSubstring("ibbl")),
				))
			})
		})

		Context("when the script fails", func() {
			It("returns an error", func() {
				result, err := b.RunErr(`1+`)
				Ω(result).Should(BeNil())
				Ω(err).Should(MatchError(ContainSubstring("SyntaxError: Unexpected end of input")))
			})
		})

		Context("with an argument", func() {
			It("decodes the result into the argument", func() {
				var f float64
				Ω(b.RunErr(`1+2`, &f)).Error().ShouldNot(HaveOccurred())
				Ω(f).Should(Equal(3.0))

				var s []string
				Ω(b.RunErr(`["a","b","c","d"].map((c, i) => c + (i+1))`, &s)).Error().ShouldNot(HaveOccurred())
				Ω(s).Should(Equal([]string{"a1", "b2", "c3", "d4"}))
			})
		})
	})

	Describe("Run", func() {
		It("runs just like RunErr but fails if an error occurs", func() {
			result := b.Run(`1+`)
			Ω(result).Should(BeNil())
			ExpectFailures(SatisfyAll(
				ContainSubstring("Failed to run script:\n1+"),
				ContainSubstring("SyntaxError: Unexpected end of input"),
			))
		})
	})

	Describe("EvaluateTo", func() {
		It("compares the result of the actual script with the passed in matcher", func() {
			b.Run("var a = 0")
			Eventually(`a += 1`).Should(b.EvaluateTo(5.0))
		})

		It("fails with a meaningful error if there is no match", func() {
			b.Run("var a = 0")
			matcher := b.EvaluateTo(5.0)
			match, err := matcher.Match(`a += 1`)
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			Ω(matcher.FailureMessage(`a += 1`)).Should(Equal("Return value for script:\na += 1\nFailed with:\nExpected\n    <float64>: 1\nto equal\n    <float64>: 5"))

		})

		It("fails with a meaningful error if the script does not compile", func() {
			matcher := b.EvaluateTo(1.0)
			match, err := matcher.Match(`1+`)
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("Failed to run script:\n1+\n\nexception \"Uncaught\" (0:2): SyntaxError: Unexpected end of input"))
		})
	})
})
