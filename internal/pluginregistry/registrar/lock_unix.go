//go:build unix

package registrar

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on f without waiting. flock locks belong
// to the open file, so two opens in one process exclude each other too.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EINTR):
		return false, nil
	default:
		return false, err
	}
}

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
