//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock (flock) on path, creating it;
// without block it fails with errLocked when another process holds it.
// The lock ends with the returned function or with the process.
func lockFile(path string, block bool) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if !block {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
