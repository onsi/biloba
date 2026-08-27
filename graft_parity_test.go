package biloba_test

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TypeScript driver parity contract", func() {
	It("reaches the shared observable outcome through Biloba's Go API", func() {
		b.Navigate(fixtureServer + "/graft-parity.html")
		Eventually(b.ByRole("heading").WithName("Biloba parity")).Should(b.BeVisible())
		Eventually("#delayed").Should(b.HaveInnerText("ready"))

		b.SetValue(b.ByTestID("name"), "Ada")
		b.Click(b.ByRole("button").WithName("Increment"))

		actual := b.Run(`({
			count: document.querySelector('#count').innerText,
			delayed: document.querySelector('#delayed').innerText,
			echo: document.querySelector('#echo').innerText,
			heading: document.querySelector('h1').innerText,
			value: document.querySelector('[data-testid="name"]').value,
		})`)
		expectedJSON, err := os.ReadFile("fixtures/graft-parity-expected.json")
		Expect(err).NotTo(HaveOccurred())
		var expected any
		Expect(json.Unmarshal(expectedJSON, &expected)).To(Succeed())
		Expect(actual).To(Equal(expected))
	})

	It("treats a non-200 page as navigable when the spec asks for that status", func() {
		// The 200 check is an assertion, not a transport rule, and both APIs have to offer the same
		// way out of it - otherwise an error page is testable from Go and unreachable from
		// TypeScript.  The TypeScript half of this pair lives in typescript/test/parity.test.ts.
		b.NavigateWithStatus(fixtureServer+"/non-existing", http.StatusNotFound)

		b.Navigate(fixtureServer + "/non-existing")
		ExpectFailures(ContainSubstring("expected status code 200, got 404"))
	})

	It("preserves Biloba's failure semantics for a missing element", func() {
		b.Navigate(fixtureServer + "/graft-parity.html")

		b.WithTimeout(40 * time.Millisecond).GetInnerText("#never")

		ExpectFailures(SatisfyAll(
			ContainSubstring("Timed out after"),
			ContainSubstring(`to have property "innerText"`),
		))
	})
})
