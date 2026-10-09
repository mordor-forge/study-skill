// Package regtest is what the tests of the Knowledge base plugin registry's
// packages share: a learner's computer made of temporary folders. It holds
// no test of its own and nothing but tests import it.
package regtest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// FileName is the registry's name in Lamplight's configuration folder, and
// LockName its lock's. They are spelled out here so that a test fails when
// the registry is renamed without anyone meaning to.
const (
	FileName = "knowledge-base-plugins.json"
	LockName = "knowledge-base-plugins.lock"
)

// Computer is a learner's computer as the registry sees it: a home folder,
// which holds Lamplight's configuration folder and a bin folder on PATH, and
// a Study home apart from it. Base holds both, and nothing else at first.
type Computer struct {
	Base, Home, Study, Bin string
	// Vars is the environment: HOME, STUDY_HOME and PATH. A test may change
	// it and add XDG_CONFIG_HOME.
	Vars map[string]string
}

// New returns a computer with an empty home folder and an empty Study home.
func New(t *testing.T) *Computer {
	t.Helper()
	base := t.TempDir()
	// The temporary folder is itself reached through a link on some systems.
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	c := &Computer{Base: base, Home: filepath.Join(base, "home"), Study: filepath.Join(base, "study")}
	c.Bin = filepath.Join(c.Home, "bin")
	for _, dir := range []string{c.Bin, c.Study} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c.Vars = map[string]string{"HOME": c.Home, "STUDY_HOME": c.Study, "PATH": c.Bin}
	return c
}

// With returns a copy of the computer with other variables in its
// environment. The folders are the same ones.
func (c *Computer) With(vars map[string]string) *Computer {
	other := *c
	other.Vars = maps.Clone(c.Vars)
	maps.Copy(other.Vars, vars)
	return &other
}

// Getenv reads the computer's environment.
func (c *Computer) Getenv(key string) string { return c.Vars[key] }

// StudyHome is the Study home the environment names.
func (c *Computer) StudyHome() string { return c.Vars["STUDY_HOME"] }

// ConfigDir is Lamplight's configuration folder in the home folder, and
// Registry the registry in it: where they are unless a test moves them.
func (c *Computer) ConfigDir() string { return filepath.Join(c.Home, ".config", "lamplight") }

// Registry is the registry in ConfigDir.
func (c *Computer) Registry() string { return filepath.Join(c.ConfigDir(), FileName) }

// WriteRegistry puts content where the registry is.
func (c *Computer) WriteRegistry(t *testing.T, content string) {
	t.Helper()
	WriteFile(t, c.Registry(), content)
}

// WriteFile writes a file, making its folder.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Program puts a program a learner can run at path, and returns the path.
func Program(t *testing.T, path string) string {
	t.Helper()
	WriteFile(t, path, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Mkdir makes a folder and returns its path.
func Mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Symlink makes link a symbolic link to target, making the link's folder.
func Symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// Text is a file's content, or "" when it is not there.
func Text(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Exists reports whether anything has the name, a link that leads nowhere
// included.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Snapshot is a fingerprint of everything below dir: each name, what it is,
// when it was last written, and what a file holds or a link says. Two
// snapshots are equal when nothing was made, removed or written between
// them.
func Snapshot(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00" + info.ModTime().String() + "\x00"))
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			h.Write([]byte(target))
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil && !os.IsPermission(err) {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
