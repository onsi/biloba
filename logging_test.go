package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Logging", func() {
	It("redirects logs to the GinkgoWriter", func() {
		b.Run("console.log('hello log', 3, [1,2,3])")
		Ω(gt.buffer).Should(gbytes.Say("\"hello log\" - 3 - \\[1, 2, 3\\]"))
		b.Run("console.log('hello log', 3, 'this is very very long', ['a', 3, 'b'], {dog: 'woof', cat: 'meow'}, 100, ['a', 'b', 'c', 'd', 'e'], 'still very very very long', 100, true, 'come on!')")
		Ω(gt.buffer).Should(gbytes.Say("hello log"))
		Ω(gt.buffer).Should(gbytes.Say("3\n"))
		Ω(gt.buffer).Should(gbytes.Say("\"this is very very long\"\n"))
		Ω(gt.buffer).Should(gbytes.Say("\\[a, 3, b\\]\n"))
		Ω(gt.buffer).Should(gbytes.Say("\\{dog: woof, cat: meow\\}\n"))
		b.Run("console.debug('hello debug')")
		Ω(gt.buffer).Should(gbytes.Say("hello debug"))
		b.Run("console.info('hello info')")
		Ω(gt.buffer).Should(gbytes.Say("hello info"))
		b.Run("console.error('hello error')")
		Ω(gt.buffer).Should(gbytes.Say("hello error"))
		b.Run("console.warn('hello warning')")
		Ω(gt.buffer).Should(gbytes.Say("hello warning"))
		b.Run("console.assert(false, 'hello assert')")
		Ω(gt.buffer).Should(gbytes.Say("hello assert"))

		ExpectFailures("Detected console.assert failure:\n\"hello assert\"\n")
	})
})
