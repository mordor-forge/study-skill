package regfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Folder is Lamplight's configuration folder as one command finds it: found
// once, one hop at a time, and from then on reached through the open folder
// and never through its name again. So what is read, locked and written is
// the folder that was checked, whatever happens to its name meanwhile.
type Folder struct {
	// Path is the folder and Registry the registry in it, as they are named.
	// They are for messages and results, never for opening anything.
	Path, Registry string

	env    Env
	walker *Walker
	place  *Place
}

// Open finds Lamplight's configuration folder, lamplight in XDG_CONFIG_HOME
// or in ~/.config, and opens it when it is there. The caller closes it.
//
// It refuses a folder the agent could write, or lead elsewhere: one that is
// the Study home, is inside it, or is reached through it by a symbolic link
// at any hop, and one whose place depends on the folder study started in.
// The registry in such a folder is not read, whatever it holds.
func Open(env Env) (*Folder, error) {
	path, err := configFolder(env)
	if err != nil {
		return nil, err
	}
	f := &Folder{Path: path, Registry: path + string(filepath.Separator) + FileName, env: env, walker: NewWalker(env.StudyHome)}
	if err := f.walk(); err != nil {
		f.walker.Close()
		return nil, err
	}
	return f, nil
}

// configFolder is where Lamplight's configuration folder is by name.
func configFolder(env Env) (string, error) {
	base := env.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home := env.Getenv("HOME")
		if home == "" {
			var err error
			if home, err = os.UserHomeDir(); err != nil {
				return "", &Error{Code: CodeFailedPrecondition, Err: err, Message: "cannot find Lamplight's configuration " +
					"folder, which holds the Knowledge base plugin registry: set HOME or XDG_CONFIG_HOME"}
			}
		}
		base = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(base) {
		return "", notReadyf("Lamplight's configuration folder is in %q, which is not an absolute path, so the Knowledge "+
			"base plugin registry would be wherever study is started, inside a Topic too. It is not read: give "+
			"XDG_CONFIG_HOME, or HOME when XDG_CONFIG_HOME is not set, as an absolute path", base)
	}
	// Not cleaned by name: the walk takes each .. from the folder it is
	// really in, as the operating system does.
	return strings.TrimRight(base, "/"+string(filepath.Separator)) + string(filepath.Separator) + "lamplight", nil
}

// walk finds the folder and keeps where the walk ended.
func (f *Folder) walk() error {
	place, err := f.walker.Walk(f.Path)
	var inside *InsideError
	switch {
	case errors.As(err, &inside):
		return notReadyf("Lamplight's configuration folder, %s, is inside the Study home, %s, or is reached through "+
			"it. Your agent can write there, so the Knowledge base plugin registry in that folder is not read: it "+
			"decides which programs study starts. Keep the two apart: set XDG_CONFIG_HOME to a folder outside the "+
			"Study home that no link in the Study home leads to, or move the Study home", f.Path, f.env.StudyHome)
	case err != nil:
		return &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot reach Lamplight's configuration " +
			"folder, " + f.Path + ", which holds the Knowledge base plugin registry (" + Reason(err) + "): check that " +
			"every folder on the way is one you can read"}
	case place.Leaf != "":
		place.Close()
		return notReadyf("%s is not a folder, so it cannot be Lamplight's configuration folder, which holds the "+
			"Knowledge base plugin registry: move it away, or set XDG_CONFIG_HOME to another folder", f.Path)
	case place.DanglingLink != "":
		link := place.DanglingLink
		place.Close()
		return notReadyf("Lamplight's configuration folder, %s, is reached through a symbolic link that leads to a "+
			"folder that is not there (%s), so there is no registry to read, and study makes none through the link: "+
			"make the folder it leads to, or remove the link", f.Path, link)
	}
	f.place = place
	return nil
}

// Reason is an operating system error without the path it names: the
// message already says which path.
func Reason(err error) string {
	if perr, ok := err.(*fs.PathError); ok {
		return perr.Err.Error()
	}
	return err.Error()
}

// Close closes the folder and lets go of the Study home.
func (f *Folder) Close() {
	if f.place != nil {
		f.place.Close()
	}
	f.walker.Close()
}

// Walker is the Walker that found the folder, for checking a plugin's
// program and arguments against the same Study home.
func (f *Folder) Walker() *Walker { return f.walker }

// Exists reports whether the folder is there.
func (f *Folder) Exists() bool { return len(f.place.Missing) == 0 }

// Dir is the folder, open, or nil when it is not there yet. Everything in
// the folder is read and written through it.
func (f *Folder) Dir() *os.Root {
	if !f.Exists() {
		return nil
	}
	return f.place.Folder()
}

// Nearest is the deepest folder on the way that exists, open, and its path
// as the walk went: the folder itself, or the one study would start making
// it in.
func (f *Folder) Nearest() (*os.Root, string) { return f.place.Folder(), f.place.Path() }

// CanCreate says, without making anything, why Create would fail for a
// folder that is not there: nothing when it would not. A dry run asks it so
// that it reports what the real run would.
func (f *Folder) CanCreate() error {
	if f.Exists() {
		return nil
	}
	if slices.Contains(f.place.Missing, "..") {
		return notReadyf("Lamplight's configuration folder, %s, is not there, and study does not make a folder through "+
			"a path with .. in it: make the folder yourself", f.Path)
	}
	if dir, path := f.Nearest(); !Writable(dir) {
		return notReadyf("Lamplight's configuration folder, %s, is not there, and study cannot make it: %s is not a "+
			"folder you can write in. Check its permissions, or make the folder yourself", f.Path, path)
	}
	return nil
}

// Create makes the folder when it is not there, for the learner alone to
// read, as the XDG Base Directory Specification asks of a configuration
// folder that has to be made. It then finds the folder again from the
// start, against the Study home as it is by then, so the folder that is
// used is one that was checked whole.
func (f *Folder) Create() error {
	if f.Exists() {
		return nil
	}
	if err := f.CanCreate(); err != nil {
		return err
	}
	dir, path := f.Nearest()
	opened := []*os.Root{}
	defer func() {
		for _, d := range opened {
			_ = d.Close()
		}
	}()
	for _, name := range f.place.Missing {
		path = filepath.Join(path, name)
		if err := dir.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot make " + path + " (" + Reason(err) +
				"), where the Knowledge base plugin registry goes: check that you can write in the folder that holds it"}
		}
		next, err := dir.OpenRoot(name)
		if err != nil {
			return &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot open " + path + " (" + Reason(err) +
				"), where the Knowledge base plugin registry goes"}
		}
		opened = append(opened, next)
		dir = next
	}
	f.place.Close()
	f.walker.Close()
	f.place, f.walker = nil, NewWalker(f.env.StudyHome)
	if err := f.walk(); err != nil {
		return err
	}
	if !f.Exists() {
		return notReadyf("Lamplight's configuration folder, %s, was removed while study was making it: try again", f.Path)
	}
	return nil
}

// Read returns the plugins the registry holds, ordered by name. A registry
// that is not there yet holds none.
//
// The registry is a regular file in the folder: a symbolic link is not
// followed, since study replaces the registry when it changes and a link
// could lead anywhere. Whatever the file holds was checked like a
// registration before it is returned, so nothing from it reaches a terminal
// or a program unchecked.
func (f *Folder) Read() ([]Plugin, error) {
	if !f.Exists() {
		return nil, nil
	}
	data, found, err := ReadRegular(f.Dir(), FileName, MaxBytes)
	switch {
	case errors.Is(err, ErrNotRegular):
		return nil, Errorf(CodeCorrupt, "%s is not a regular file, so study does not read it as the Knowledge base plugin "+
			"registry (a symbolic link is not followed): put the file itself there, or delete it and register your "+
			"Knowledge base plugins again with study knowledge-base add", f.Registry)
	case errors.Is(err, errTooLarge):
		return nil, damaged(f.Registry, "it is larger than %d bytes", MaxBytes)
	case errors.Is(err, ErrChanging):
		return nil, Errorf(CodeBusy, "%s kept being replaced while study was reading it: try again", f.Registry)
	case err != nil:
		return nil, &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot read the Knowledge base plugin " +
			"registry, " + f.Registry + " (" + Reason(err) + "): check that the file is yours to read"}
	case !found:
		return nil, nil
	}
	return Decode(f.Registry, data)
}

// ErrNotRegular marks something that is not a regular file where one is
// expected: a folder, a symbolic link, a device.
var ErrNotRegular = errors.New("not a regular file")

var errTooLarge = errors.New("too large")

// ReadRegular reads the file called name in an open folder, up to max bytes.
// The name must be a regular file's own: a symbolic link is not followed,
// and what is read is the file that was looked at, or nothing is.
func ReadRegular(dir *os.Root, name string, max int64) (data []byte, found bool, err error) {
	file, _, err := OpenRegular(dir, name, os.O_RDONLY)
	if file == nil || err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(data)) > max {
		return nil, true, errTooLarge
	}
	return data, true, nil
}

// ErrChanging marks a file that was replaced again and again while study
// was opening it.
var ErrChanging = errors.New("kept changing")

// afterLook, when a test sets it, is called with a file's name after
// OpenRegular looked at the file and before it opens it, where another
// program could put something else under the name.
var afterLook func(name string)

// OpenRegular opens the regular file called name in an open folder, and
// returns nil when nothing has the name. It follows no symbolic link:
// opening through the folder would follow one that stays in the folder, so
// the file is looked at first and the one that is opened must be the same
// file. Anything but a regular file is ErrNotRegular.
//
// Another study may replace the file between the look and the open, as a
// change to the registry does. Then the two are different files, and it
// looks again; after a few times it gives up with ErrChanging.
func OpenRegular(dir *os.Root, name string, flag int) (*os.File, fs.FileInfo, error) {
	for range 20 {
		info, err := dir.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, nil, ErrNotRegular
		}
		if afterLook != nil {
			afterLook(name)
		}
		file, err := dir.OpenFile(name, flag, 0)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if opened, err := file.Stat(); err == nil && os.SameFile(info, opened) {
			return file, opened, nil
		}
		_ = file.Close()
	}
	return nil, nil, ErrChanging
}
