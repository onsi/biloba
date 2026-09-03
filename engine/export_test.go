package engine

import "context"

// SessionContextForTest exposes a session's chromedp target context so engine_test.go can
// exercise the context-level primitives (NavigateContext and friends) against a live tab.
func SessionContextForTest(session *Session) context.Context {
	return session.ctx
}

// HTTPStatusFailureForTest exposes the classifier NavigateContext uses to tell Chrome's
// loading failure for a 4xx/5xx document from a genuine transport/target failure.  Which
// responses Chrome reports that way varies by Chrome version, so the classification is pinned
// here rather than by racing a browser that may or may not produce the error.
func HTTPStatusFailureForTest(err error) bool {
	return httpStatusFailure(err)
}

// MarkSessionCrashedForTest records the crash signal without killing a renderer, allowing the
// recovery state machine to be tested without disturbing other specs sharing the test browser.
func MarkSessionCrashedForTest(session *Session) {
	session.markCrashed()
}
