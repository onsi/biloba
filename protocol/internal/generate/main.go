// Command generate writes the generated protocol artifacts.  Run it with `go generate ./protocol`.
// The check that they are up to date lives in protocol/generated_test.go, not here.
package main

import (
	"os"
	"path/filepath"

	"github.com/onsi/biloba/protocol/internal/protocolgen"
)

func main() {
	root, err := filepath.Abs("..")
	must(err)
	for path, contents := range protocolgen.Files() {
		destination := filepath.Join(root, filepath.FromSlash(path))
		must(os.MkdirAll(filepath.Dir(destination), 0o755))
		must(os.WriteFile(destination, contents, 0o644))
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
