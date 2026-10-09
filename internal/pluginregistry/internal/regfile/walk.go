package regfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// A Walker finds where a path leads one hop at a time, and refuses a path
// that leads through the Study home.
//
// The registry decides which program study starts, outside the agent's
// sandbox, and the agent writes in the Study home. So nothing the registry
// depends on may be inside the Study home or be reached through it: not the
// folder the registry is in, not a plugin's program. A link in the Study
// home is the agent's to repoint, wherever it leads today, so it is not
// enough to look at where a path ends.
//
// The walk never asks the operating system to resolve a path. It opens the
// root folder, then each folder from the one before it, and follows each
// symbolic link itself, from the folder that really holds the link. Every
// folder it enters is compared with the Study home by file identity, as the
// two open folders are: never by name, which another letter case, another
// link or another mount can change without changing the folder. What it
// returns is the last folder, open. Whoever goes on from there uses that
// folder, and not its name again, so nothing can change between the check
// and the use.
//
// What no walk can see is a hard link: a second name, outside the Study
// home, for a file that also has a name inside it. Both names are the same
// file and neither leads through the Study home. A link count above one says
// nothing, since many system programs have several names. Keeping the agent
// from making such a link is the sandbox's job.
type Walker struct {
	studyHome string
	// home is the Study home, open, and homeInfo what identifies it. Both
	// are nil when there is no Study home yet. Then the Walker looks for it
	// again at every folder it comes to (see identify): another study can
	// make it at any moment.
	home     *os.Root
	homeInfo fs.FileInfo
	// A Study home that is not there yet has no identity. Until it is made,
	// what would be inside it is told by where it will be: comingIn is the
	// deepest folder of its path that exists, and comingAs the names below
	// that folder that do not. That tells of paths that are not there
	// either; what is there by the time a walk reaches it is told by the
	// second look.
	comingIn fs.FileInfo
	comingAs []string
	// blind is why the Study home cannot be told apart from other folders,
	// when it is there and study may not look at it. Every walk then fails.
	blind error
	// beforeEnter, when a test sets it, is called with a folder's path after
	// the folder was looked at and before it is opened, where another
	// program could put something else in its place.
	beforeEnter func(path string)
}

// maxLinks is how many symbolic links one walk follows before it calls them
// a loop, as the operating system does.
const maxLinks = 40

// afterNewWalker, when a test sets it, is called once a Walker has looked for
// the Study home and before anything walks with it, where another study
// could make a Study home that was not there.
var afterNewWalker func()

// NewWalker returns a Walker that refuses the Study home at studyHome. It
// holds the Study home open until Close.
func NewWalker(studyHome string) *Walker {
	w := &Walker{studyHome: studyHome}
	w.identify()
	if w.homeInfo == nil && w.blind == nil && filepath.IsAbs(studyHome) {
		// Not there yet: find where it will be.
		if place, err := (&Walker{}).Walk(studyHome); err == nil {
			if info, err := place.Folder().Stat("."); err == nil && len(place.Missing) > 0 {
				w.comingIn, w.comingAs = info, place.Missing
			}
			place.Close()
		}
	}
	if afterNewWalker != nil {
		afterNewWalker()
	}
	return w
}

// identify looks for the Study home and takes what identifies it. A Walker
// asks when it is made, and again at every folder for as long as it found no
// Study home: another study can make the Study home at any moment, a first
// study topic create for one, and what is in it by the time a walk gets
// there must be refused like anything else in it.
func (w *Walker) identify() {
	if w.studyHome == "" {
		// A Walker without a Study home refuses nothing: NewWalker walks
		// with one to find where a Study home will be.
		return
	}
	home, err := os.OpenRoot(w.studyHome)
	switch {
	case err == nil:
		if info, err := home.Stat("."); err == nil {
			w.home, w.homeInfo = home, info
			// It is known by what it is now, not by where it would be.
			w.comingIn, w.comingAs = nil, nil
		} else {
			_ = home.Close()
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		// A Study home study cannot open, such as one it may not list, is
		// still a folder with an identity. One it cannot even look at
		// leaves no way to tell what is inside it, and then nothing is
		// taken for outside.
		info, serr := os.Stat(w.studyHome)
		switch {
		case serr == nil && info.IsDir():
			w.homeInfo = info
			w.comingIn, w.comingAs = nil, nil
		case serr != nil && !errors.Is(serr, fs.ErrNotExist):
			w.blind = fmt.Errorf("study cannot look at the Study home, %s, to tell what is inside it: %w", w.studyHome, serr)
		}
	}
}

// isHome reports whether a folder is the Study home, by what identifies the
// two. While no Study home is known, it looks for one again first.
func (w *Walker) isHome(info fs.FileInfo) (bool, error) {
	if w.homeInfo == nil && w.blind == nil {
		w.identify()
	}
	if w.blind != nil {
		return false, w.blind
	}
	return w.homeInfo != nil && os.SameFile(w.homeInfo, info), nil
}

// Close lets go of the Study home.
func (w *Walker) Close() {
	if w.home != nil {
		_ = w.home.Close()
	}
}

// InsideError says that a path is the Study home, is inside it, or is
// reached through it.
type InsideError struct {
	// StudyHome is the Study home, by the name the walk reached it under.
	StudyHome string
}

func (e *InsideError) Error() string { return "the path leads through the Study home, " + e.StudyHome }

// IsInside reports whether err says that a path leads through the Study
// home.
func IsInside(err error) bool {
	var inside *InsideError
	return errors.As(err, &inside)
}

// Place is where a walk ended: the folders it went through, open, and what
// the path names in the last of them.
type Place struct {
	root    string
	folders []*os.Root
	names   []string

	// Leaf is the name, in the last folder, of what the path ends in, and
	// LeafInfo what that is, when it is not a folder: a file, or something
	// else that is not a symbolic link. Both are empty when the path ends in
	// a folder, which is then the last folder itself.
	Leaf     string
	LeafInfo fs.FileInfo

	// Missing are the names below the last folder that are not there, when
	// the path does not exist as far as it goes.
	Missing []string
	// DanglingLink is the symbolic link whose target the first missing name
	// belongs to: the path is missing because that link leads nowhere.
	DanglingLink string
}

// Folder is the last folder the walk entered, open.
func (p *Place) Folder() *os.Root { return p.folders[len(p.folders)-1] }

// Path is the path of the last folder as the walk went: with every symbolic
// link followed.
func (p *Place) Path() string { return p.root + strings.Join(p.names, string(filepath.Separator)) }

func (p *Place) path(name string) string { return filepath.Join(p.Path(), name) }

// Close closes the folders the walk opened.
func (p *Place) Close() {
	for _, f := range p.folders {
		_ = f.Close()
	}
	p.folders = nil
}

func (p *Place) pop() {
	last := len(p.folders) - 1
	_ = p.folders[last].Close()
	p.folders, p.names = p.folders[:last], p.names[:last-1]
}

// step is one name still to resolve, and the symbolic link it came from when
// it is part of a link's target.
type step struct{ name, link string }

func steps(path, link string) []step {
	names := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == filepath.Separator })
	out := make([]step, len(names))
	for i, name := range names {
		out[i] = step{name, link}
	}
	return out
}

// Walk follows an absolute path from the root folder and returns where it
// ends: a folder, something in a folder, or the folder below which the rest
// of the path is not there. The caller closes the Place.
//
// It fails with an InsideError as soon as the path enters the Study home,
// whether it would stay there or come out again. Any other error means that
// the path cannot be followed: a folder that cannot be opened (study opens
// each to list it, so one it may only pass through stops the walk), a file
// where a folder should be, links without end, or a folder that was replaced
// while the walk looked at it.
func (w *Walker) Walk(path string) (_ *Place, err error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%q is not an absolute path", path)
	}
	if w.blind != nil {
		return nil, w.blind
	}
	volume := filepath.VolumeName(path)
	p := &Place{root: volume + string(filepath.Separator)}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	root, err := os.OpenRoot(p.root)
	if err != nil {
		return nil, err
	}
	p.folders = []*os.Root{root}
	if info, err := root.Stat("."); err != nil {
		return nil, err
	} else if err := w.entered(p, info); err != nil {
		return nil, err
	}

	todo := steps(path[len(volume):], "")
	links := 0
	for len(todo) > 0 {
		s := todo[0]
		todo = todo[1:]
		switch s.name {
		case ".":
			continue
		case "..":
			// Up from the folder the walk is really in, never from a name.
			if len(p.folders) > 1 {
				p.pop()
			}
			continue
		}
		info, err := p.Folder().Lstat(s.name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			p.Missing, p.DanglingLink = []string{s.name}, s.link
			for _, rest := range todo {
				p.Missing = append(p.Missing, rest.name)
			}
			if err := w.coming(p); err != nil {
				return nil, err
			}
			return p, nil
		case err != nil:
			return nil, err
		case info.Mode()&fs.ModeSymlink != 0:
			if links++; links > maxLinks {
				return nil, &fs.PathError{Op: "walk", Path: path, Err: syscall.ELOOP}
			}
			target, err := p.Folder().Readlink(s.name)
			if err != nil {
				return nil, err
			}
			link := p.path(s.name)
			if filepath.IsAbs(target) {
				for len(p.folders) > 1 {
					p.pop()
				}
				target = target[len(filepath.VolumeName(target)):]
			}
			// A relative target starts from the folder that holds the link,
			// which is the folder the walk is in.
			todo = append(steps(target, link), todo...)
		case info.IsDir():
			// The Study home is known before it is opened, which study may
			// not be allowed to do.
			if home, err := w.isHome(info); err != nil {
				return nil, err
			} else if home {
				return nil, &InsideError{StudyHome: p.path(s.name)}
			}
			if w.beforeEnter != nil {
				w.beforeEnter(p.path(s.name))
			}
			changed := fmt.Errorf("%s changed while study was reading it", p.path(s.name))
			next, err := p.Folder().OpenRoot(s.name)
			if err != nil {
				if now, lerr := p.Folder().Lstat(s.name); lerr != nil || !os.SameFile(info, now) {
					return nil, changed
				}
				return nil, err
			}
			// What was opened is what was looked at: opening follows a link
			// put there in between, when it stays in the folder, and this
			// is how it shows.
			opened, err := next.Stat(".")
			if err != nil || !os.SameFile(info, opened) {
				_ = next.Close()
				return nil, changed
			}
			p.folders, p.names = append(p.folders, next), append(p.names, s.name)
			if err := w.entered(p, opened); err != nil {
				return nil, err
			}
		default:
			if len(todo) > 0 {
				return nil, &fs.PathError{Op: "walk", Path: p.path(s.name), Err: syscall.ENOTDIR}
			}
			p.Leaf, p.LeafInfo = s.name, info
		}
	}
	return p, nil
}

// entered checks the folder the walk just went into, by what identifies it.
func (w *Walker) entered(p *Place, info fs.FileInfo) error {
	if home, err := w.isHome(info); err != nil {
		return err
	} else if home {
		return &InsideError{StudyHome: p.Path()}
	}
	return nil
}

// coming checks a path that is not there yet against a Study home that is
// not there yet either: it will be inside the Study home if it starts below
// the same folder with the same names.
func (w *Walker) coming(p *Place) error {
	if w.comingIn == nil || len(p.Missing) < len(w.comingAs) {
		return nil
	}
	if info, err := p.Folder().Stat("."); err != nil || !os.SameFile(w.comingIn, info) {
		return nil
	}
	for i, name := range w.comingAs {
		// Whatever the file system makes of letter case, so does this.
		if !strings.EqualFold(name, p.Missing[i]) {
			return nil
		}
	}
	return &InsideError{StudyHome: w.studyHome}
}

// Inside reports whether an absolute path leads through the Study home. A
// path that cannot be followed for another reason does not: what stops the
// walk stops whoever uses the path too. When the Study home cannot be
// looked at, every path counts as inside it.
func (w *Walker) Inside(path string) bool {
	if w.blind != nil {
		return true
	}
	place, err := w.Walk(path)
	if err != nil {
		return IsInside(err)
	}
	place.Close()
	return false
}
