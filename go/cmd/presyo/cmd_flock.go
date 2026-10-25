package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// withStateLock runs fn while holding an exclusive flock on
// statePath+".lock" (pure Go, static binary). Both fbpost and fbweekly
// serialize their read-check-post-write sections through it.
func withStateLock(statePath string, fn func() int) int {
	lockPath := statePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: lock %s: %s\n", lockPath, err.Error())
		return 1
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: lock %s: %s\n", lockPath, err.Error())
		return 1
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		fmt.Fprintf(os.Stderr, "error: flock %s: %s\n", lockPath, err.Error())
		return 1
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// parseNowFlag parses an RFC3339 -now override, falling back to time.Now
// with an error message (empty string means time.Now).
func parseNowFlag(s string) func() time.Time {
	return func() time.Time {
		if s == "" {
			return time.Now()
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: bad -now %q, using time.Now\n", s)
			return time.Now()
		}
		return t
	}
}
