//go:build !windows

package engine

import "os"

func atomicReplaceScreenshotFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
