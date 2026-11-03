//go:build !darwin && !linux

package engine

// killCommandLinesContaining does nothing where Biloba has no way to read other processes' command
// lines.
func killCommandLinesContaining(string) int { return 0 }
