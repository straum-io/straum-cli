package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock (LockFileEx) on path, creating it;
// without block it fails with errLocked when another process holds it.
// The lock ends with the returned function or with the process.
func lockFile(path string, block bool) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !block {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped)); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
