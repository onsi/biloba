package biloba

import (
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
SetUpload() attaches the file(s) at the given paths to the first file input (<input type="file">) matching selector.

Setting a file input's files is one of the few things that cannot be simulated in JavaScript (the browser forbids it for security), so unlike most Biloba interactions SetUpload reaches through the Chrome DevTools Protocol (DOM.setFileInputFiles) rather than running a snippet in the page.  It fires the input's change event, just as a real selection would:

	b.SetUpload("input[type=file]", "./fixtures/avatar.png")
	b.SetUpload("#attachments", "./a.txt", "./b.txt") // multiple files (the input needs the `multiple` attribute)

SetUpload fails the spec if no element matches selector.

When invoked with just the path(s) (no selector) SetUpload returns a Gomega matcher so you can poll until the file input is present:

	Eventually("input[type=file]").Should(b.SetUpload("./fixtures/avatar.png"))
	Eventually("#attachments").Should(b.SetUpload([]string{"./a.txt", "./b.txt"})) // multiple files: pass a []string

(In the matcher form multiple files must be passed as a single []string - bare variadic paths would be indistinguishable from the immediate selector+paths form.)

Read https://onsi.github.io/biloba/#uploading-files to learn more about uploading files
*/
func (b *Biloba) SetUpload(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	// Under-applied (matcher) form: just the path(s), with the file input supplied by Eventually.
	// This is unambiguous because the immediate form always needs a selector *and* at least one
	// path (>=2 args), so a lone path string - or a single []string of paths - can only be the
	// matcher form.
	if len(args) == 1 {
		if paths, ok := uploadPaths(args); ok {
			return gcustom.MakeMatcher(func(selector any) (bool, error) {
				return b.performSetUpload(selector, paths)
			}).WithMessage("be uploadable to")
		}
	}

	if len(args) < 2 {
		b.gt.Fatalf("SetUpload requires a selector and at least one path")
		return nil
	}
	paths, ok := uploadPaths(args[1:])
	if !ok {
		b.gt.Fatalf("SetUpload paths must be strings (or a single []string)")
		return nil
	}
	success, err := b.performSetUpload(args[0], paths)
	if err != nil {
		b.gt.Fatalf("Failed to set upload:\n%s", err.Error())
	} else if !success {
		encoded, _ := encodeSelector(args[0])
		b.gt.Fatalf("Failed to set upload:\ncould not find DOM element matching selector: %s", encoded[1:])
	}
	return nil
}

// uploadPaths normalizes SetUpload's variadic any arguments into a slice of file paths.  It accepts
// either a single []string or a run of string arguments; anything else (e.g. an XPath/Locator) is
// reported as not-paths so the caller can treat the first argument as a selector instead.
func uploadPaths(args []any) ([]string, bool) {
	if len(args) == 1 {
		if slice, ok := args[0].([]string); ok {
			return slice, true
		}
	}
	paths := make([]string, 0, len(args))
	for _, a := range args {
		s, ok := a.(string)
		if !ok {
			return nil, false
		}
		paths = append(paths, s)
	}
	return paths, true
}

// performSetUpload is the shared engine behind SetUpload's immediate and matcher forms.  It returns
// (false, nil) when no element matches yet - so the matcher form polls and the immediate form can
// report a missing element - and a non-nil error only for genuine failures.
func (b *Biloba) performSetUpload(selector any, paths []string) (bool, error) {
	b.ensureBiloba()

	encoded, err := encodeSelector(selector)
	if err != nil {
		return false, err
	}

	var node *runtime.RemoteObject
	script := b.JSFunc("_biloba.node").Invoke(encoded)
	if err := chromedp.Run(b.Context, chromedp.Evaluate(script, &node)); err != nil {
		return false, err
	}
	if node == nil || node.ObjectID == "" {
		return false, nil
	}

	if err := chromedp.Run(b.Context, dom.SetFileInputFiles(paths).WithObjectID(node.ObjectID)); err != nil {
		return false, err
	}
	return true, nil
}
