//go:build !unix

package regfile

import "os"

// Writable reports whether an open folder looks writable from its
// permission bits.
func Writable(dir *os.Root) bool {
	info, err := dir.Stat(".")
	return err == nil && info.Mode().Perm()&0o200 != 0
}

// Runnable reports whether the file called name in an open folder can be
// run, as far as this operating system's permission bits tell: they do not,
// so a file that is there counts.
func Runnable(dir *os.Root, name string) bool {
	_, err := dir.Lstat(name)
	return err == nil
}
