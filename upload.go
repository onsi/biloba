package biloba

import (
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

/*
SetUpload() attaches the file(s) at the given paths to the first file input (<input type="file">) matching selector.

Setting a file input's files is one of the few things that cannot be simulated in JavaScript (the browser forbids it for security), so unlike most Biloba interactions SetUpload reaches through the Chrome DevTools Protocol (DOM.setFileInputFiles) rather than running a snippet in the page.  It fires the input's change event, just as a real selection would:

	b.SetUpload("input[type=file]", "./fixtures/avatar.png")
	b.SetUpload("#attachments", "./a.txt", "./b.txt") // multiple files (the input needs the `multiple` attribute)

SetUpload fails the spec if no element matches selector.

Read https://onsi.github.io/biloba/#uploading-files to learn more about uploading files
*/
func (b *Biloba) SetUpload(selector any, paths ...string) {
	b.gt.Helper()
	b.ensureBiloba()

	encoded, err := encodeSelector(selector)
	if err != nil {
		b.gt.Fatalf("Failed to set upload:\n%s", err.Error())
		return
	}

	var node *runtime.RemoteObject
	script := b.JSFunc("_biloba.node").Invoke(encoded)
	if err := chromedp.Run(b.Context, chromedp.Evaluate(script, &node)); err != nil {
		b.gt.Fatalf("Failed to set upload:\n%s", err.Error())
		return
	}
	if node == nil || node.ObjectID == "" {
		b.gt.Fatalf("Failed to set upload:\ncould not find DOM element matching selector: %s", encoded[1:])
		return
	}

	if err := chromedp.Run(b.Context, dom.SetFileInputFiles(paths).WithObjectID(node.ObjectID)); err != nil {
		b.gt.Fatalf("Failed to set upload:\n%s", err.Error())
	}
}
