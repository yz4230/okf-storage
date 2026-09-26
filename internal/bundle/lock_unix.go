//go:build unix

package bundle

import (
	"errors"
	"os"
	"syscall"
)

// errLocked reports that another process holds the lock.
var errLocked = errors.New("locked by another process")

// tryLockFile takes an exclusive advisory lock on f, held until f is closed,
// or fails with errLocked if another process holds it.
func tryLockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case errors.Is(err, syscall.EWOULDBLOCK):
			return errLocked
		case !errors.Is(err, syscall.EINTR):
			return err
		}
	}
}
