package engine

import (
	"sync"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("a Handoff", ginkgo.Label("no-browser"), func() {
	ginkgo.It("handles what it was handed in order, and Drain waits for what came before it", func() {
		release := make(chan struct{})
		var lock sync.Mutex
		var handled []int
		h := NewHandoff(func(v int) {
			if v == 1 {
				<-release // the first value holds up the rest, as a slow writer would
			}
			lock.Lock()
			defer lock.Unlock()
			handled = append(handled, v)
		})
		for v := range 5 {
			h.Push(v) // never waits, though the handler is held up
		}
		drained := make(chan struct{})
		go func() { h.Drain(); close(drained) }()
		gomega.Consistently(drained, "50ms").ShouldNot(gomega.BeClosed())
		close(release)
		gomega.Eventually(drained).Should(gomega.BeClosed())
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(handled).To(gomega.Equal([]int{0, 1, 2, 3, 4}))
	})

	ginkgo.It("once closed, takes nothing more and lets go of its handler", func() {
		var handled []string
		h := NewHandoff(func(v string) { handled = append(handled, v) })
		h.Push("before")
		h.Close()
		h.Push("after")
		h.Drain()
		gomega.Expect(handled).To(gomega.Equal([]string{"before"}))
		h.mu.Lock()
		defer h.mu.Unlock()
		gomega.Expect(h.handle).To(gomega.BeNil(), "a listener still holding the Handoff holds nothing through it")
		gomega.Expect(h.running).To(gomega.BeFalse())
	})
})
