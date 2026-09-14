package engine

import (
	"github.com/chromedp/cdproto/dom"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("frame coordinate mapping", ginkgo.Label("no-browser"), func() {
	ginkgo.It("maps offsets and unequal scales", func() {
		points, err := mapPointsToQuad([]actionPoint{{x: 0, y: 0}, {x: 50, y: 25}, {x: 100, y: 100}}, 100, 100,
			dom.Quad{300, 200, 500, 200, 500, 500, 300, 500})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(points).To(gomega.Equal([]actionPoint{{x: 300, y: 200}, {x: 400, y: 275}, {x: 500, y: 500}}))
	})

	ginkgo.It("maps a rotated frame", func() {
		points, err := mapPointsToQuad([]actionPoint{{x: 25, y: 75}}, 100, 100,
			dom.Quad{200, 100, 200, 200, 100, 200, 100, 100})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(points).To(gomega.Equal([]actionPoint{{x: 125, y: 125}}))
	})

	ginkgo.It("maps corners and the center under perspective", func() {
		points, err := mapPointsToQuad([]actionPoint{{x: 0, y: 0}, {x: 100, y: 0}, {x: 100, y: 100}, {x: 0, y: 100}, {x: 50, y: 50}}, 100, 100,
			dom.Quad{0, 0, 200, 0, 150, 100, 50, 100})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(points[:4]).To(gomega.Equal([]actionPoint{{x: 0, y: 0}, {x: 200, y: 0}, {x: 150, y: 100}, {x: 50, y: 100}}))
		// The source center lies at the intersection of the quad's diagonals.
		gomega.Expect(points[4].x).To(gomega.BeNumerically("~", 100, 1e-9))
		gomega.Expect(points[4].y).To(gomega.BeNumerically("~", 200.0/3, 1e-9))
	})

	ginkgo.It("rejects an incomplete content quad", func() {
		_, err := mapPointsToQuad([]actionPoint{{}}, 100, 100, dom.Quad{0, 0})
		gomega.Expect(err).To(gomega.MatchError(&Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame owner has no content quad"}))
	})

	ginkgo.It("rejects a degenerate perspective transform", func() {
		_, err := mapPointsToQuad([]actionPoint{{}}, 100, 100, dom.Quad{0, 0, 100, 0, 100, 100, 100, 200})
		gomega.Expect(err).To(gomega.MatchError(&Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame owner transform is degenerate"}))
	})
})
