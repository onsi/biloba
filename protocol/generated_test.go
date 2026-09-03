package protocol_test

import (
	"os"
	"path/filepath"

	"github.com/onsi/biloba/protocol/internal/protocolgen"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The drift guard for the generated TypeScript declarations and golden frames.
//
// This deliberately does not shell out to git.  A `git diff --exit-code` answers "does the tree
// match the last commit?", which is a different question from the one that matters ("do these
// files match the Go wire definitions?") and gets it wrong in both directions: it passes on a
// staged-but-divergent file, and it fails on a working tree where you regenerated correctly and
// have not committed yet - the exact state you are in while making the change. Rendering the
// artifacts and comparing them to what is on disk asks the real question, needs no git, and gives
// the same answer in CI and on a dirty working tree.
var _ = Describe("the generated protocol artifacts", func() {
	It("match what the generator renders from the Go wire definitions", func() {
		for path, expected := range protocolgen.Files() {
			actual, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(path)))
			Expect(err).NotTo(HaveOccurred(), "%s is missing; run `go generate ./protocol`", path)
			Expect(string(actual)).To(Equal(string(expected)),
				"%s is stale: the Go wire definitions have changed since it was generated.\nRun `go generate ./protocol` and commit the result alongside the change that caused it.", path)
		}
	})

	It("covers every artifact the generator writes", func() {
		// A new artifact that nobody compares is a new artifact that can drift, so pin the set
		// itself rather than only the contents of whatever happens to be in it.
		paths := []string{}
		for path := range protocolgen.Files() {
			paths = append(paths, path)
		}
		Expect(paths).To(ConsistOf(
			"typescript/src/generated/protocol.ts",
			"protocol/testdata/golden/handshake-request.json",
			"protocol/testdata/golden/protocol-error-response.json",
			"protocol/testdata/golden/operation-response.json",
		))
	})
})
