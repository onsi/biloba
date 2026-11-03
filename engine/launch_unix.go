//go:build darwin || linux

package engine

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// killCommandLinesContaining SIGKILLs every other process whose command line has want as a whole
// argument, and returns how many it found.  Linux reads /proc; macOS has no /proc, so it asks ps.
func killCommandLinesContaining(want string) int {
	self := os.Getpid()
	killed := 0
	for pid, commandLine := range commandLines() {
		if pid != self && namesArgument(commandLine, want) && syscall.Kill(pid, syscall.SIGKILL) == nil {
			killed++
		}
	}
	return killed
}

// commandLines maps each running process to its command line, arguments joined by spaces.
func commandLines() map[int]string {
	lines := map[int]string{}
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return lines
		}
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
			if err != nil {
				continue
			}
			lines[pid] = strings.TrimRight(strings.ReplaceAll(string(cmdline), "\x00", " "), " ")
		}
		return lines
	}
	out, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return lines
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		pidText, commandLine, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if pid, err := strconv.Atoi(pidText); err == nil {
			lines[pid] = strings.TrimSpace(commandLine)
		}
	}
	return lines
}
