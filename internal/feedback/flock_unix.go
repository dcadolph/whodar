//go:build unix

package feedback

import (
	"fmt"
	"os"
	"syscall"
)

// lockSuffix names the sibling advisory-lock file that guards writes to the
// store. It is locked in place of the data file, whose atomic rename would
// otherwise detach a lock held on it.
const lockSuffix = ".lock"

// lockFile takes an exclusive advisory lock on path, creating it if needed, and
// returns a function that releases the lock and closes the file.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("feedback: open lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("feedback: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
