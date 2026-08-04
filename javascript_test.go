package biloba_test

import (
	"context"
	"time"

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

			It("treats a nil argument as 'discard the result' instead of failing", func() {
				result, err := b.RunErr(`1+2`, nil)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(result).Should(Equal(3.0))
			})

			It("discards the result of a side-effect-only script passed a nil argument", func() {
				_, err := b.RunErr(`window.__sideEffect = true; undefined`, nil)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(b.Run(`window.__sideEffect`)).Should(BeTrue())
			})

			It("returns a directive error when an undefined result is decoded into a non-nil pointer", func() {
				var s string
				_, err := b.RunErr(`undefined`, &s)
				Ω(err).Should(MatchError(ContainSubstring("the script returned undefined")))
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

		It("hints toward RunAsync/IIFE when the script uses a top-level return", func() {
			b.Run(`const x = 1; return x;`)
			ExpectFailures(SatisfyAll(
				ContainSubstring("Illegal return statement"),
				ContainSubstring("b.RunAsync"),
				ContainSubstring("IIFE"),
			))
		})

		It("does not add the return hint for unrelated errors", func() {
			b.Run(`1+`)
			ExpectFailures(Not(ContainSubstring("b.RunAsync")))
		})
	})

	Describe("RunErrAsync", func() {
		Context("when the script resolves", func() {
			It("awaits the result and returns it as an unencoded Go object", func() {
				Ω(b.RunErrAsync(`return await new Promise(resolve => setTimeout(() => resolve(42), 10))`)).Should(Equal(42.0))
				Ω(b.RunErrAsync(`return await Promise.resolve(["a", "b", "c"])`)).Should(HaveExactElements("a", "b", "c"))
			})

			It("also handles plain (non-promise) values", func() {
				Ω(b.RunErrAsync(`return 1 + 2`)).Should(Equal(3.0))
			})

			It("can use await freely in the body", func() {
				b.Run(`window.app = {load: () => Promise.resolve({name: "Biloba"})}`)
				Ω(b.RunErrAsync(`
					const result = await app.load()
					return result.name
				`)).Should(Equal("Biloba"))
			})
		})

		Context("with an argument", func() {
			It("decodes the awaited result into the argument", func() {
				var n int
				Ω(b.RunErrAsync(`return await Promise.resolve(7)`, &n)).Error().ShouldNot(HaveOccurred())
				Ω(n).Should(Equal(7))
			})
		})

		Context("when the awaited promise rejects", func() {
			It("returns an error", func() {
				result, err := b.RunErrAsync(`return await Promise.reject(new Error("boom"))`)
				Ω(result).Should(BeNil())
				Ω(err).Should(MatchError(ContainSubstring("boom")))
			})
		})
	})

	Describe("RunAsync", func() {
		It("runs just like RunErrAsync but fails if an error occurs", func() {
			b.Run(`window.app = {load: () => Promise.resolve(17)}`)
			Ω(b.RunAsync(`return await app.load()`)).Should(Equal(17.0))

			result := b.RunAsync(`return await Promise.reject(new Error("boom"))`)
			Ω(result).Should(BeNil())
			ExpectFailures(SatisfyAll(
				ContainSubstring("Failed to run async script:"),
				ContainSubstring("boom"),
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

		Describe("capturing the evaluated value", func() {
			type foldEntry struct {
				Fidelity string
				Seq      int
			}

			It("captures the value from the winning read, decoded into a typed target", func() {
				// the motivating case: gate on a condition AND keep the value that satisfied it, without
				// a second read of an expression whose value may have moved on in between
				b.Run(`window.__biloba_fold_log = [{fidelity: "shape", seq: 1}]`)
				b.Run(`setTimeout(() => { window.__biloba_fold_log.push({fidelity: "text", seq: 2}) }, 50)`)

				var log []foldEntry
				Eventually(`window.__biloba_fold_log`).Should(b.EvaluateTo(ContainElement(HaveKeyWithValue("fidelity", "text"))).Capture(&log))
				Ω(log).Should(HaveLen(2))
				Ω(log[0]).Should(Equal(foldEntry{Fidelity: "shape", Seq: 1}))
				Ω(log[1]).Should(Equal(foldEntry{Fidelity: "text", Seq: 2}))
			})

			It("decodes a JavaScript number into a typed target, dodging the float64 gotcha", func() {
				b.Run(`window.__biloba_capture_num = 3`)
				var n int
				Eventually(`window.__biloba_capture_num`).Should(b.EvaluateTo(BeNumerically("==", 3)).Capture(&n))
				Ω(n).Should(Equal(3)) //int(3), not float64(3)
			})

			It("reads the expression exactly once - the whole point of Capture", func() {
				//a getter that counts its reads; the trailing null keeps Run's completion value
				//JSON-serializable (defineProperty returns window, which is circular)
				b.Run(`
					window.__biloba_capture_reads = 0
					Object.defineProperty(window, "__biloba_capture_counted", {
						configurable: true,
						get: () => { window.__biloba_capture_reads++; return ["a"] },
					})
					null
				`)

				var values []string
				Eventually(`window.__biloba_capture_counted`).Should(b.EvaluateTo(HaveLen(1)).Capture(&values))
				Ω(values).Should(Equal([]string{"a"}))
				Ω(b.Run(`window.__biloba_capture_reads`)).Should(BeNumerically("==", 1))
			})

			It("does not capture under ShouldNot", func() {
				b.Run(`window.__biloba_capture_str = "hello"`)
				var s string
				Ω(`window.__biloba_capture_str`).ShouldNot(b.EvaluateTo("goodbye").Capture(&s))
				Ω(s).Should(BeEmpty())
			})

			It("leaves the failure message untouched", func() {
				b.Run("var a = 0")
				var n int
				matcher := b.EvaluateTo(5.0).Capture(&n)
				match, err := matcher.Match(`a += 1`)
				Ω(match).Should(BeFalse())
				Ω(err).ShouldNot(HaveOccurred())
				Ω(matcher.FailureMessage(`a += 1`)).Should(Equal("Return value for script:\na += 1\nFailed with:\nExpected\n    <float64>: 1\nto equal\n    <float64>: 5"))
				Ω(n).Should(BeZero())
			})

			It("fails fast - it does not wait out the timeout - when the capture target cannot hold the value", func() {
				b.Run(`window.__biloba_capture_wrong = "not-a-number"`)
				var wrongType int
				failure := ""
				g := NewGomega(func(message string, callerSkip ...int) { failure = message })

				start := time.Now()
				g.Eventually(`window.__biloba_capture_wrong`).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(b.EvaluateTo("not-a-number").Capture(&wrongType))
				Ω(time.Since(start)).Should(BeNumerically("<", time.Second*2))

				Ω(failure).Should(ContainSubstring("Told to stop trying"))
				Ω(failure).Should(ContainSubstring("Capture cannot decode the observed value into *int"))
				Ω(wrongType).Should(BeZero())
			})
		})
	})

	Describe("GetJSValue", func() {
		It("polls until the expression becomes defined", func() {
			b.Run(`setTimeout(() => { window.__biloba_test_value = "hello" }, 50)`)
			Ω(b.GetJSValue("window.__biloba_test_value")).Should(Equal("hello"))
		})

		It("retries through a thrown error (e.g. a ReferenceError) until the identifier is defined", func() {
			// Before the global exists, evaluating a bare undeclared identifier throws a ReferenceError.
			// Immediate() reads once, so it surfaces that error as a spec failure right away.
			b.Immediate().GetJSValue("__biloba_late_var")
			ExpectFailures(ContainSubstring("ReferenceError"))

			// The default polling form treats that same error as "not ready yet" and retries through
			// it until the identifier is declared, rather than failing on the first throw.
			b.Run(`setTimeout(() => { window.__biloba_late_var = 99 }, 50)`)
			Ω(b.GetJSValue("__biloba_late_var")).Should(Equal(99.0))
		})

		It("treats null as a legitimate value that returns immediately, without retrying", func() {
			b.Run(`window.__biloba_null_val = null`)
			start := time.Now()
			result := b.WithTimeout(200 * time.Millisecond).GetJSValue("window.__biloba_null_val")
			Ω(result).Should(BeNil())
			Ω(time.Since(start)).Should(BeNumerically("<", 150*time.Millisecond))
		})

		It("decodes into a typed pointer, dodging the float64 default", func() {
			b.Run(`window.__biloba_num = 3`)

			var n int
			b.GetJSValue("window.__biloba_num", &n)
			Ω(n).Should(Equal(3))

			Ω(b.GetJSValue("window.__biloba_num")).Should(BeNumerically("==", 3))
			Ω(b.GetJSValue("window.__biloba_num")).ShouldNot(Equal(3)) // float64(3) != int(3)
		})

		It("fails fast under Immediate when the expression is not yet defined", func() {
			result := b.Immediate().GetJSValue("window.__biloba_not_defined_yet")
			Ω(result).Should(BeNil())
			ExpectFailures(ContainSubstring(`evaluate "window.__biloba_not_defined_yet" to a defined value`))
		})

		It("names the expression in the timeout failure message", func() {
			b.WithTimeout(60 * time.Millisecond).GetJSValue("window.__biloba_never_defined")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring(`evaluate "window.__biloba_never_defined" to a defined value`),
			))
		})

		It("accepts all four poll-config knobs, since it is a polling getter", func() {
			b.Run(`window.__biloba_knob_test = "ok"`)
			Ω(b.WithTimeout(time.Second).GetJSValue("window.__biloba_knob_test")).Should(Equal("ok"))
			Ω(b.WithPolling(10 * time.Millisecond).GetJSValue("window.__biloba_knob_test")).Should(Equal("ok"))
			Ω(b.WithContext(context.Background()).GetJSValue("window.__biloba_knob_test")).Should(Equal("ok"))
			Ω(b.Immediate().GetJSValue("window.__biloba_knob_test")).Should(Equal("ok"))
		})
	})

	Describe("invoking functions", func() {
		It("provides a convenient mechanism for invoking functions that handles JSON encoding for you", func() {
			b.Run(b.JSFunc("console.log").Invoke(1, []int{2, 3, 4}, "hello", true, nil))
			Ω(string(gt.buffer.Contents())).Should(ContainSubstring(`1 - [2, 3, 4] - "hello" - true - null`))

			adder := b.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
			var result int
			b.Run(adder.Invoke(1, 2, 3, 4, 5, 10), &result)
			Ω(result).Should(Equal(25))

			Ω(adder.Invoke(1, 2, 3.7, 4, 5)).Should(b.EvaluateTo(15.7))

			b.Run("var counter = 17")
			Ω(adder.Invoke(1, 2, 3.7, 4, 5, b.JSVar("counter + 3"))).Should(b.EvaluateTo(35.7))
		})
	})
})
