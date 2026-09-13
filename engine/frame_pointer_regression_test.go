package engine_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("frame-scoped trusted pointer coordinates", func() {
	for _, scenario := range []struct {
		name  string
		oopif bool
	}{
		{name: "in a shared renderer target"},
		{name: "in an OOPIF target", oopif: true},
	} {
		scenario := scenario
		It("translates a nested same-origin frame point exactly once "+scenario.name, func(ctx SpecContext) {
			isolatedBrowser, err := engine.StartBrowser(ctx, engine.BrowserConfig{
				ExecutablePath: chromePath(),
				Arguments:      []string{"--site-per-process"},
				ArtifactDir:    GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(isolatedBrowser.Close)

			frames := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "text/html")
				if request.URL.Path == "/inner" {
					fmt.Fprint(response, `<!doctype html><title>pointer-inner</title><style>html,body{margin:0}#intended{position:absolute;left:20px;top:15px;width:100px;height:40px}</style><button id="intended" onclick="this.dataset.clicks=String(Number(this.dataset.clicks||0)+1)">intended</button>`)
					return
				}
				fmt.Fprint(response, `<!doctype html><title>pointer-outer</title><style>html,body{margin:0}iframe{position:absolute;left:80px;top:70px;width:300px;height:200px;border:0}button{position:absolute;z-index:2}#center-decoy{left:210px;top:165px;width:40px;height:25px}#offset-decoy{left:175px;top:150px;width:20px;height:20px}</style><iframe src="/inner"></iframe><button id="center-decoy" onclick="this.dataset.clicked='yes'">center</button><button id="offset-decoy" onclick="this.dataset.clicked='yes'">offset</button>`)
			}))
			DeferCleanup(frames.Close)

			root, err := isolatedBrowser.OpenSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(root.Close)
			Expect(root.Navigate(ctx, server.URL)).To(Succeed())
			outerURL := frames.URL
			if scenario.oopif {
				outerURL = strings.Replace(outerURL, "127.0.0.1", "localhost", 1)
			}
			_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe id="outer" style="position:fixed;left:300px;top:200px;width:500px;height:350px;border:0"></iframe>'; document.querySelector('#outer').src = `+fmt.Sprintf("%q", outerURL))
			Expect(err).NotTo(HaveOccurred())

			outer, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "pointer-outer"}}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(outer.Close)
			inner, err := root.WaitForFrame(ctx, engine.FrameQuery{Title: &engine.Expectation{Kind: engine.ExpectEqual, Expected: "pointer-inner"}}, engine.PollPolicy{Timeout: 3 * time.Second, Interval: 5 * time.Millisecond})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(inner.Close)
			if scenario.oopif {
				Expect(outer.TargetID()).NotTo(Equal(root.TargetID()))
			} else {
				Expect(outer.TargetID()).To(Equal(root.TargetID()))
			}
			Expect(inner.TargetID()).To(Equal(outer.TargetID()))

			Expect(inner.RealisticClick(ctx, engine.CSS("#intended"))).To(Succeed())
			Expect(inner.ClickWith(ctx, engine.CSS("#intended"), engine.ClickOptions{Count: 1, Mode: engine.Realistic, Offset: &engine.Point{X: 5, Y: 5}})).To(Succeed())
			clicks, err := inner.Evaluate(ctx, `document.querySelector('#intended').dataset.clicks || ''`)
			Expect(err).NotTo(HaveOccurred())
			Expect(clicks).To(Equal("2"))
			decoyClicks, err := outer.Evaluate(ctx, `[document.querySelector('#center-decoy').dataset.clicked || '', document.querySelector('#offset-decoy').dataset.clicked || '']`)
			Expect(err).NotTo(HaveOccurred())
			Expect(decoyClicks).To(ConsistOf("", ""))
		})
	}

	It("preserves trusted clicks through same-origin iframe piercing on a tab", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `document.body.innerHTML = '<iframe id="same" style="position:fixed;left:320px;top:210px;width:300px;height:180px;border:0"></iframe>'; const doc = document.querySelector('#same').contentDocument; doc.open(); doc.write('<style>html,body{margin:0}button{position:absolute;left:30px;top:25px;width:100px;height:40px}</style><button id="target" onclick="this.dataset.clicked=\'yes\'">target</button>'); doc.close()`)
		Expect(err).NotTo(HaveOccurred())

		Expect(root.RealisticClick(ctx, engine.CSS("#same >>> #target"))).To(Succeed())
		clicked, err := root.Evaluate(ctx, `document.querySelector('#same').contentDocument.querySelector('#target').dataset.clicked || ''`)
		Expect(err).NotTo(HaveOccurred())
		Expect(clicked).To(Equal("yes"))
	})
})
