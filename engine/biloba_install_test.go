package engine_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// Chrome is asked to run biloba.js at the start of every document a tab creates, so a navigation or a
// Prepare does not have to send the whole script again.  The first query on a session still sends it
// once, for the document that was open before the registration; these specs start after that.
var _ = Describe("how a tab session gets biloba.js", func() {
	exists := func(ctx SpecContext, session *engine.Session) {
		GinkgoHelper()
		found, err := session.Exists(ctx, engine.CSS("body"))
		Expect(err).NotTo(HaveOccurred())
		Expect(found.Value).To(BeTrue())
	}

	It("does not send the script again after a navigation or a Prepare", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		before := engine.BilobaSendsForTest()

		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		Expect(session.Prepare(ctx)).To(Succeed())
		exists(ctx, session)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)

		Expect(engine.BilobaSendsForTest()).To(Equal(before))
	})

	It("does the same on a tab made with NewTab", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		tab, err := root.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(tab.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, tab)
		before := engine.BilobaSendsForTest()

		Expect(tab.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, tab)

		Expect(engine.BilobaSendsForTest()).To(Equal(before))
	})

	It("does the same on a tab the page opened", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, server.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `window.open(location.href); 1`)
		Expect(err).NotTo(HaveOccurred())

		var spawned *engine.Session
		Eventually(ctx, func() *engine.Session {
			tabs, tabsErr := root.Tabs(ctx)
			Expect(tabsErr).NotTo(HaveOccurred())
			for _, tab := range tabs {
				if tab.OpenerID() == root.TargetID() {
					spawned = tab
				}
			}
			return spawned
		}).ShouldNot(BeNil())
		Eventually(ctx, func() (bool, error) {
			found, existsErr := spawned.Exists(ctx, engine.CSS("body"))
			return found.Value == true, existsErr
		}).Should(BeTrue())
		before := engine.BilobaSendsForTest()

		Expect(spawned.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, spawned)

		Expect(engine.BilobaSendsForTest()).To(Equal(before))
	})

	It("leaves the registration alone when Prepare removes a page clock", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		Expect(session.InstallClock(ctx, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		before := engine.BilobaSendsForTest()

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)

		clock, err := session.Evaluate(ctx, `typeof window._bilobaClock`)
		Expect(err).NotTo(HaveOccurred())
		Expect(clock).To(Equal("undefined"))
		Expect(engine.BilobaSendsForTest()).To(Equal(before))
	})

	It("sends the script again, once, when the page lost it", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		before := engine.BilobaSendsForTest()

		_, err = session.Evaluate(ctx, `delete window._biloba; 1`)
		Expect(err).NotTo(HaveOccurred())
		exists(ctx, session)
		exists(ctx, session)

		Expect(engine.BilobaSendsForTest()).To(Equal(before + 1))
	})

	It("falls back to sending the script after each navigation when Chrome would not take the registration", func(ctx SpecContext) {
		restore := engine.SetBilobaRegistrationForTest(func(context.Context, string) error { return errors.New("registration refused") })
		session, err := browser.OpenSession(ctx)
		restore()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		before := engine.BilobaSendsForTest()

		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		exists(ctx, session)
		Expect(session.Prepare(ctx)).To(Succeed())
		exists(ctx, session)

		Expect(engine.BilobaSendsForTest()).To(Equal(before + 2))
	})
})
