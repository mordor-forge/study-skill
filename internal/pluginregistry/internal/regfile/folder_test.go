package regfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regtest"
)

// lookThenSwap makes the next file OpenRegular looks at called name be
// swapped, by swap, before it is opened.
func lookThenSwap(t *testing.T, name string, swap func()) (swapped *bool) {
	t.Helper()
	swapped = new(bool)
	afterLook = func(looked string) {
		if looked == name && !*swapped {
			*swapped = true
			swap()
		}
	}
	t.Cleanup(func() { afterLook = nil })
	return swapped
}

// Opening a name in a folder follows a link that stays in the folder. So a
// file is looked at first, and what is opened is that file or nothing: a
// link put there in between is not followed to what it leads to.
func TestOpenRegularOpensTheFileItLookedAt(t *testing.T) {
	c := regtest.New(t)
	dir := regtest.Mkdir(t, c.ConfigDir())
	lock := filepath.Join(dir, LockName)
	regtest.WriteFile(t, lock, "")
	regtest.WriteFile(t, filepath.Join(dir, FileName), `{"format": 1, "plugins": []}`)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// The lock becomes a link to the registry: opened through it, the
	// "lock" would be the registry itself.
	swapped := lookThenSwap(t, LockName, func() {
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		regtest.Symlink(t, FileName, lock)
	})
	file, _, err := OpenRegular(root, LockName, os.O_RDWR)
	if !*swapped {
		t.Fatal("the file was never about to be opened, so this test proves nothing")
	}
	if !errors.Is(err, ErrNotRegular) || file != nil {
		t.Fatalf("OpenRegular = %v, %v; want the link refused, not followed", file, err)
	}

	// The registry is replaced by a new file, as a change replaces it: then
	// it is the new one that is opened, whole.
	replaced := lookThenSwap(t, FileName, func() {
		regtest.WriteFile(t, filepath.Join(dir, "new"), `{"format": 1, "plugins": [], "new": true}`)
		if err := os.Rename(filepath.Join(dir, "new"), filepath.Join(dir, FileName)); err != nil {
			t.Fatal(err)
		}
	})
	data, found, err := ReadRegular(root, FileName, MaxBytes)
	if !*replaced || err != nil || !found || !strings.Contains(string(data), `"new": true`) {
		t.Errorf("ReadRegular after the file was replaced = %q, %v, %v; want the new file", data, found, err)
	}

	// Nothing under the name is no file, and no error.
	if file, _, err := OpenRegular(root, "missing", os.O_RDONLY); file != nil || err != nil {
		t.Errorf("OpenRegular of nothing = %v, %v", file, err)
	}
	if _, _, err := OpenRegular(root, ".", os.O_RDONLY); !errors.Is(err, ErrNotRegular) {
		t.Errorf("OpenRegular of a folder = %v; want it refused", err)
	}
}

// The program that is opened to be started is the one that was checked,
// even when its path comes to name another file between the check and the
// open: it is opened in the folder the walk ended in, not by its path.
func TestOpenProgramOpensTheProgramThatWasChecked(t *testing.T) {
	c := regtest.New(t)
	bin := regtest.Mkdir(t, filepath.Join(c.Home, "opt", "bin"))
	checked := regtest.Program(t, filepath.Join(bin, "kb"))
	want, err := os.Stat(checked)
	if err != nil {
		t.Fatal(err)
	}
	// What the agent would like to have run instead.
	other := regtest.Mkdir(t, filepath.Join(c.Study, "topic", "bin"))
	regtest.WriteFile(t, filepath.Join(other, "kb"), "#!/bin/sh\necho the agent's\n")
	if err := os.Chmod(filepath.Join(other, "kb"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := NewWalker(c.Study)
	defer w.Close()
	swapped := lookThenSwap(t, "kb", func() {
		if err := os.Rename(bin, bin+".moved"); err != nil {
			t.Fatal(err)
		}
		regtest.Symlink(t, other, bin)
	})
	file, err := w.OpenProgram(checked)
	if !*swapped {
		t.Fatal("the program was never about to be opened, so this test proves nothing")
	}
	if err != nil {
		t.Fatalf("OpenProgram: %v", err)
	}
	defer file.Close()
	if got, err := file.Stat(); err != nil || !os.SameFile(want, got) {
		t.Errorf("the open file is not the program that was checked (%v)", err)
	}
	if data, _ := io.ReadAll(file); strings.Contains(string(data), "the agent's") {
		t.Errorf("the program at the path was opened in place of the one checked: %q", data)
	}
	// Asked again, the path leads through the Study home now, and is refused.
	afterLook = nil
	if again, err := w.OpenProgram(checked); !IsInside(err) || again != nil {
		t.Errorf("OpenProgram of the path afterwards = %v, %v; want it refused", again, err)
	}
}

// A program replaced by another file, in its own folder, after it was
// checked and before it is opened, is not opened: the other file is one
// nobody checked, and whether this user may run it was never asked.
func TestOpenProgramRefusesAProgramReplacedWhileItWasChecked(t *testing.T) {
	c := regtest.New(t)
	checked := regtest.Program(t, filepath.Join(c.Bin, "kb"))
	other := regtest.Program(t, filepath.Join(c.Bin, "other"))
	w := NewWalker(c.Study)
	defer w.Close()
	swapped := lookThenSwap(t, "kb", func() {
		if err := os.Rename(other, checked); err != nil {
			t.Fatal(err)
		}
	})
	file, err := w.OpenProgram(checked)
	if !*swapped {
		t.Fatal("the program was never about to be opened, so this test proves nothing")
	}
	if CodeOf(err) != CodeFailedPrecondition || !strings.Contains(err.Error(), "changed while study was checking it") || file != nil {
		t.Fatalf("OpenProgram = %v, %v; want it to stop because the program changed", file, err)
	}
	// Asked again, it is the file that is there now that is checked, and
	// opened.
	afterLook = nil
	again, err := w.OpenProgram(checked)
	if err != nil {
		t.Fatalf("OpenProgram of the program that is there now: %v", err)
	}
	again.Close()
}

func TestCreateMakesTheFolderOrSaysWhyNot(t *testing.T) {
	c := regtest.New(t)
	folder, err := Open(envOf(c, c.Home))
	if err != nil {
		t.Fatal(err)
	}
	defer folder.Close()
	if folder.Exists() || folder.Dir() != nil || folder.Path != c.ConfigDir() || folder.Registry != c.Registry() {
		t.Fatalf("Open on a new computer = %+v, exists %v", folder, folder.Exists())
	}
	if nearest, path := folder.Nearest(); nearest == nil || path != c.Home {
		t.Errorf("Nearest = %v, %s; want the home folder", nearest, path)
	}
	if err := folder.CanCreate(); err != nil {
		t.Fatalf("CanCreate: %v", err)
	}
	if regtest.Exists(filepath.Dir(c.ConfigDir())) {
		t.Fatal("asking made a folder")
	}
	if err := folder.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !folder.Exists() || folder.Dir() == nil {
		t.Fatal("the folder is not there after Create")
	}
	for _, dir := range []string{c.ConfigDir(), filepath.Dir(c.ConfigDir())} {
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v, %v; want a folder for the learner alone", dir, info, err)
		}
	}
	want, _ := os.Stat(c.ConfigDir())
	if got, err := folder.Dir().Stat("."); err != nil || !os.SameFile(want, got) {
		t.Errorf("the open folder is not %s (%v)", c.ConfigDir(), err)
	}
	if err := folder.Create(); err != nil {
		t.Errorf("Create of a folder that is there: %v", err)
	}

	// Through a path with .. below a folder that is not there, there is no
	// telling where the folder would be made: study makes none.
	odd := c.With(map[string]string{"XDG_CONFIG_HOME": c.Home + "/missing/../cfg"})
	before := regtest.Snapshot(t, c.Base)
	missing, err := Open(envOf(odd, c.Home))
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	for what, err := range map[string]error{"CanCreate": missing.CanCreate(), "Create": missing.Create()} {
		if CodeOf(err) != CodeFailedPrecondition || !strings.Contains(err.Error(), "with .. in it") {
			t.Errorf("%s = %v; want failed_precondition, a path with .. in it", what, err)
		}
	}
	if regtest.Snapshot(t, c.Base) != before {
		t.Error("a folder was made through a path with .. in it")
	}
}
