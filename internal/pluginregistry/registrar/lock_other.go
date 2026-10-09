//go:build !unix

package registrar

import (
	"errors"
	"os"
)

// Windows packages come later (see the design's Scope); until then a change
// to the registry reports that locking it is unsupported.
func tryLock(*os.File) (bool, error) {
	return false, errors.New("locking the registry is not supported on this operating system yet")
}

func unlockFile(*os.File) error { return nil }
