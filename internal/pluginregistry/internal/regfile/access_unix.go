//go:build unix

package regfile

import (
	"os"

	"golang.org/x/sys/unix"
)

// Writable reports whether the current user may make and replace files in
// an open folder, without writing anything. It asks about the folder that
// is open, not about a name.
func Writable(dir *os.Root) bool {
	d, err := dir.Open(".")
	if err != nil {
		return false
	}
	defer d.Close()
	return unix.Faccessat(int(d.Fd()), ".", unix.W_OK|unix.X_OK, unix.AT_EACCESS) == nil
}

// Runnable reports whether the user study runs as may run the file called
// name in an open folder. The system is asked, for the effective user, so
// an execute bit that applies to someone else does not count; and it is
// asked through the folder that is open, not through a path.
func Runnable(dir *os.Root, name string) bool {
	d, err := dir.Open(".")
	if err != nil {
		return false
	}
	defer d.Close()
	return unix.Faccessat(int(d.Fd()), name, unix.X_OK, unix.AT_EACCESS) == nil
}
