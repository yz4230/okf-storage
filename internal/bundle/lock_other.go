//go:build !unix

package bundle

import (
	"errors"
	"os"
)

var errLocked = errors.New("locked by another process")

// Without flock, a second process on the same directory goes undetected.
func tryLockFile(*os.File) error { return nil }
