package engine_test

import (
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("event conversions", Label("no-browser"), func() {
	Describe("NewPageError", func() {
		It("reads an Error the way DevTools prints it, and leaves its frames to Stack", func() {
			pageError := engine.NewPageError(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
				Text:       "Uncaught",
				URL:        "http://example.com/app.js",
				LineNumber: 4, ColumnNumber: 7,
				Exception:  &runtime.RemoteObject{Type: runtime.TypeObject, Description: "TypeError: x is not a function\n    at go (http://example.com/app.js:5:8)"},
				StackTrace: &runtime.StackTrace{CallFrames: []*runtime.CallFrame{{FunctionName: "go", URL: "http://example.com/app.js", LineNumber: 4, ColumnNumber: 7}}},
			}})
			Expect(pageError.Message).To(Equal("Uncaught TypeError: x is not a function"))
			Expect(pageError.URL).To(Equal("http://example.com/app.js"))
			Expect(pageError.Line).To(Equal(int64(4)))
			Expect(pageError.Column).To(Equal(int64(7)))
			Expect(pageError.Stack).To(Equal([]engine.ConsoleStackFrame{{URL: "http://example.com/app.js", FunctionName: "go", Line: 4, Column: 7}}))
		})

		It("renders a thrown primitive, and does not repeat a description the text already carries", func() {
			thrown := engine.NewPageError(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
				Text: "Uncaught (in promise)", Exception: &runtime.RemoteObject{Type: runtime.TypeNumber, Value: []byte("42")},
			}})
			Expect(thrown.Message).To(Equal("Uncaught (in promise) 42"))

			repeated := engine.NewPageError(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
				Text: "Uncaught SyntaxError: Unexpected token '}'", Exception: &runtime.RemoteObject{Description: "SyntaxError: Unexpected token '}'"},
			}})
			Expect(repeated.Message).To(Equal("Uncaught SyntaxError: Unexpected token '}'"))
		})

		It("takes the location from the top of the stack when Chrome gives no URL", func() {
			pageError := engine.NewPageError(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
				Text:       "Uncaught",
				StackTrace: &runtime.StackTrace{CallFrames: []*runtime.CallFrame{{URL: "http://example.com/app.js", LineNumber: 2, ColumnNumber: 3}}},
			}})
			Expect(pageError.URL).To(Equal("http://example.com/app.js"))
			Expect(pageError.Line).To(Equal(int64(2)))
			Expect(engine.NewPageError(&runtime.EventExceptionThrown{}).Stack).To(BeEmpty())
		})
	})

	It("renders a request's failure with the reason Chrome gives", func() {
		Expect(engine.RequestFailureText(&network.EventLoadingFailed{ErrorText: "net::ERR_CONNECTION_REFUSED"})).To(Equal("net::ERR_CONNECTION_REFUSED"))
		Expect(engine.RequestFailureText(&network.EventLoadingFailed{ErrorText: "net::ERR_BLOCKED_BY_CLIENT", BlockedReason: network.BlockedReasonInspector})).To(Equal("net::ERR_BLOCKED_BY_CLIENT (blocked: inspector)"))
		Expect(engine.RequestFailureText(&network.EventLoadingFailed{ErrorText: "net::ERR_FAILED", CorsErrorStatus: &network.CorsErrorStatus{CorsError: network.CorsErrorMissingAllowOriginHeader}})).To(Equal("net::ERR_FAILED (CORS: MissingAllowOriginHeader)"))
	})

	It("renders a console message's text from its arguments", func() {
		message := engine.NewConsoleMessage(&runtime.EventConsoleAPICalled{Type: runtime.APITypeWarning, Args: []*runtime.RemoteObject{
			{Type: runtime.TypeString, Value: []byte(`"careful"`)},
			{Type: runtime.TypeNumber, Value: []byte("3")},
			{Type: runtime.TypeObject, Description: "Object"},
		}})
		Expect(message.Type).To(Equal("warning"))
		Expect(message.Text).To(Equal("careful 3 Object"))
		Expect(message.Stack).To(BeEmpty())
	})
})
