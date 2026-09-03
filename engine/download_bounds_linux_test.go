package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDownloadContentBoundsAFileThatGrowsAfterMetadataInspection(t *testing.T) {
	dir := t.TempDir()
	id := "growing-download"
	path := filepath.Join(dir, id)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	session := &Session{
		downloadDir: dir,
		downloads: map[string]*Download{
			id: {ID: id, State: DownloadComplete},
		},
	}
	written := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			_, err = file.Write([]byte(strings.Repeat("x", 1025)))
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		written <- err
	}()

	_, err := session.DownloadContent(context.Background(), id, 1024)
	if err == nil || !strings.Contains(err.Error(), "exceeds limit 1024") {
		t.Fatalf("expected a bounded download error, got %v", err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}
