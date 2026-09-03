package engine_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("runner-neutral chrome-headless-shell installation", func() {
	It("atomically publishes one complete cache entry under concurrent installers", func() {
		platform := "test-platform"
		archivePath := filepath.Join(GinkgoT().TempDir(), "shell.zip")
		file, err := os.Create(archivePath)
		Expect(err).NotTo(HaveOccurred())
		writer := zip.NewWriter(file)
		binary, err := writer.Create(filepath.Join("chrome-headless-shell-"+platform, engine.ChromeBinaryName()))
		Expect(err).NotTo(HaveOccurred())
		_, err = binary.Write([]byte("complete-binary"))
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Expect(file.Close()).To(Succeed())

		destination := filepath.Join(GinkgoT().TempDir(), "stable-version")
		errors := make(chan error, 2)
		var installers sync.WaitGroup
		for range 2 {
			installers.Add(1)
			go func() {
				defer installers.Done()
				errors <- engine.InstallHeadlessShellArchiveForTest(archivePath, destination, platform)
			}()
		}
		installers.Wait()
		close(errors)
		for installErr := range errors {
			Expect(installErr).NotTo(HaveOccurred())
		}

		contents, err := os.ReadFile(filepath.Join(destination, "chrome-headless-shell-"+platform, engine.ChromeBinaryName()))
		Expect(err).NotTo(HaveOccurred())
		Expect(contents).To(Equal([]byte("complete-binary")))
		partials, err := filepath.Glob(destination + ".partial-*")
		Expect(err).NotTo(HaveOccurred())
		Expect(partials).To(BeEmpty())
	})
})
