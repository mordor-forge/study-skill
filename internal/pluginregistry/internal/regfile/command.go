package regfile

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// namePattern is the shape of a plugin's name, which is the shape of a
// Topic's id.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedNames cannot name a plugin. They are the kinds of Knowledge base a
// Topic records, "none" and "plugin" (with the plugin's name beside it), so
// a name is never mistaken for a kind.
var reservedNames = []string{"none", "plugin"}

// CheckName checks the name a plugin is registered under. The learner types
// it and a Topic's topic.toml holds it, so it follows the rule of a Topic's
// id, and is none of the reserved names.
func CheckName(name string) error {
	switch {
	case name == "":
		return invalidf("name the Knowledge base plugin: lowercase letters, digits and single hyphens, such as shelf")
	case len(name) > 64 || !namePattern.MatchString(name):
		return invalidf("%q is not a valid name for a Knowledge base plugin: use lowercase letters, digits and single "+
			"hyphens, up to 64 characters", name)
	case slices.Contains(reservedNames, name):
		return invalidf("%q cannot name a Knowledge base plugin: it is a kind of Knowledge base, which a Topic records "+
			"beside the name of its plugin", name)
	}
	return nil
}

// CheckCommand checks a plugin's command as the registry holds it and study
// shows it: the program's path and each argument are text a terminal shows
// as it is, kept otherwise exactly as given, since the program receives
// them as they are. An argument may be empty.
func CheckCommand(command []string) error {
	if len(command)-1 > MaxArgs {
		return invalidf("the command has more than %d arguments", MaxArgs)
	}
	for i, word := range command {
		what := fmt.Sprintf("argument %d of the command", i)
		if i == 0 {
			what = "the program's path"
		}
		if err := checkWord(what, word, MaxWordRunes); err != nil {
			return err
		}
	}
	return nil
}

// checkWord checks text the registry stores and study prints: valid UTF-8
// with no control character, and none of the characters that change the
// direction text is shown in, which are the ones the command line quotes
// before it prints anything. The replacement character is refused too: it
// is what text that could not be read turns into.
func checkWord(what, s string, maxRunes int) error {
	if !utf8.ValidString(s) {
		return invalidf("%s is not valid UTF-8 text", what)
	}
	for _, r := range s {
		switch {
		case unicode.IsControl(r):
			return invalidf("%s contains a control character", what)
		case unicode.Is(unicode.Bidi_Control, r):
			return invalidf("%s contains a bidirectional control character (U+%04X), which can make text display "+
				"differently from what it says", what, r)
		case r == utf8.RuneError:
			return invalidf("%s contains the replacement character (U+FFFD), which stands for text that could not be read", what)
		}
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return invalidf("%s is longer than %d characters", what, maxRunes)
	}
	return nil
}

// CleanURL checks the address of a plugin reached by URL and returns it as
// the registry holds it: an absolute http or https URL with a host, a port
// that can be one, and neither credentials nor a fragment. The scheme and
// host are lowercased, a default port is dropped, and an international host
// stays readable, in Unicode (NFC).
//
// No error repeats the address: it may hold a password or a key.
func CleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := checkWord("the URL", raw, maxURLRunes); err != nil {
		return "", err
	}
	notAnAddress := invalidf("the URL is not a web address: give an absolute http or https URL, such as http://localhost:8765/mcp")
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Opaque != "" || strings.ContainsAny(raw, " \t") {
		return "", notAnAddress
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", notAnAddress
	}
	if u.User != nil {
		return "", invalidf("the URL carries a user name or password: give the address without them")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return "", invalidf("the URL has a #fragment, which means nothing to a plugin: give the address without it")
	}
	host := norm.NFC.String(strings.ToLower(u.Hostname()))
	for _, r := range host {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Bidi_Control, r) || r == '%' {
			return "", notAnAddress
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalidf("the URL's port is not one a service can answer at: give a port from 1 to 65535")
		}
		if !(scheme == "http" && n == 80) && !(scheme == "https" && n == 443) {
			host += ":" + strconv.Itoa(n)
		}
	}
	out := scheme + "://" + host + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out, nil
}

// ResolveProgram finds the program a command names when it is registered,
// and returns its absolute path. A bare name is looked up in the folders of
// PATH, and a path is taken from the folder study started in, with a leading
// ~ for the home folder. The path is kept as it was found, symbolic links
// unresolved, so a link that a package manager moves to each new version
// keeps working.
//
// The program is then checked as CheckProgram checks it.
func (w *Walker) ResolveProgram(env Env, program string) (string, error) {
	if program == "" {
		return "", invalidf("the command is empty: give the program that starts the Knowledge base plugin")
	}
	if err := checkWord("the program", program, MaxWordRunes); err != nil {
		return "", err
	}
	if program != "~" && !strings.ContainsRune(program, '/') && !strings.ContainsRune(program, filepath.Separator) {
		for _, dir := range filepath.SplitList(env.Getenv("PATH")) {
			// Only the absolute folders of PATH: one relative to the folder
			// study started in, as "." and an empty entry are, would let a
			// file in a Topic stand for the program. And none with .. in
			// it, which a name cannot follow (see below).
			if !filepath.IsAbs(dir) || hasDotDot(dir) {
				continue
			}
			path := filepath.Join(dir, program)
			switch err := w.CheckProgram(path); {
			case err == nil:
				if err := checkWord("the program's path", path, MaxWordRunes); err != nil {
					return "", err
				}
				return path, nil
			case IsInside(err):
				// The first one PATH has is the one a shell would run.
				return "", err
			}
		}
		return "", Errorf(CodeNotFound, "there is no program named %q in the folders of your PATH: install it, or give "+
			"the path to it", program)
	}
	// A name cannot say where .. leads: the operating system goes up from
	// the folder a link leads to, and a path cleaned by name goes up from
	// the link. Rather than register another file than would run, refuse.
	if hasDotDot(program) {
		return "", invalidf("the program's path, %q, has .. in it, which leads to different places by name and through a "+
			"symbolic link: give the path without it", program)
	}
	path := expand(env, program)
	if err := checkWord("the program's path", path, MaxWordRunes); err != nil {
		return "", err
	}
	if err := w.CheckProgram(path); err != nil {
		return "", err
	}
	return path, nil
}

// hasDotDot reports whether a path has a .. among its names.
func hasDotDot(path string) bool {
	return slices.Contains(strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == filepath.Separator }), "..")
}

// expand makes a path the learner gave absolute: a leading ~ is the home
// folder, and a relative path is relative to the folder study started in.
func expand(env Env, path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home := env.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.Dir, path)
	}
	return filepath.Clean(path)
}

// errNotAProgram marks a path that is something other than a file to run.
var errNotAProgram = errors.New("not a program")

// CheckProgram checks that the absolute path names a program study may
// start: a regular file that the user study runs as may run, reached without
// going through the Study home at any hop. The agent could replace a program in the Study
// home, or repoint a link there that leads to it, and study would then start
// the agent's program outside the agent's sandbox.
func (w *Walker) CheckProgram(path string) error {
	file, err := w.openProgram(path, false)
	if file != nil {
		_ = file.Close()
	}
	return err
}

// OpenProgram checks the program as CheckProgram does and returns it open.
// It is for starting the program: whoever starts it runs the open file, and
// must not resolve the path again after this check, since the name can lead
// elsewhere by then.
func (w *Walker) OpenProgram(path string) (*os.File, error) {
	return w.openProgram(path, true)
}

func (w *Walker) openProgram(path string, open bool) (*os.File, error) {
	place, err := w.Walk(path)
	var inside *InsideError
	switch {
	case errors.As(err, &inside):
		return nil, &Error{Code: CodeInvalidArgument, Err: err, Message: path + " is inside the Study home, " + w.studyHome +
			", or is reached through it, where your agent can write. study starts a Knowledge base plugin outside the " +
			"agent's sandbox, so its program must be one the agent cannot change, by a path no link in the Study home " +
			"is part of: install it outside the Study home"}
	case err != nil:
		return nil, &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot follow " + path + " to check the " +
			"program (" + Reason(err) + "): give a path through folders you can read"}
	}
	defer place.Close()
	notRunnable := &Error{Code: CodeInvalidArgument, Err: errNotAProgram, Message: fmt.Sprintf("%q is not a program you can "+
		"run: make it executable for you, with chmod u+x, or name another", path)}
	switch {
	case len(place.Missing) > 0:
		return nil, &Error{Code: CodeNotFound, Err: fs.ErrNotExist, Message: fmt.Sprintf("there is no program at %q", path)}
	case place.Leaf == "":
		return nil, &Error{Code: CodeInvalidArgument, Err: errNotAProgram, Message: fmt.Sprintf("%q is a folder, not a program", path)}
	case !place.LeafInfo.Mode().IsRegular():
		return nil, notRunnable
	}
	// Whether this user may run the file is the system's to say: an execute
	// bit that is there for someone else does not make it runnable. It is
	// asked through the folder the walk ended in, about the name there, not
	// through the path again.
	if !Runnable(place.Folder(), place.Leaf) {
		return nil, notRunnable
	}
	if !open {
		return nil, nil
	}
	file, info, err := OpenRegular(place.Folder(), place.Leaf, os.O_RDONLY)
	switch {
	case err != nil:
		return nil, &Error{Code: CodeFailedPrecondition, Err: err, Message: "study cannot open " + path + " to start it (" +
			Reason(err) + "): it must be a program you can read"}
	case file == nil:
		return nil, &Error{Code: CodeNotFound, Err: fs.ErrNotExist, Message: fmt.Sprintf("there is no program at %q", path)}
	case !os.SameFile(place.LeafInfo, info):
		// What is opened is the file the walk looked at and the system
		// was asked about, or nothing is: another file put under the name
		// meanwhile is one nobody checked.
		_ = file.Close()
		return nil, notReadyf("%s changed while study was checking it: try again", path)
	}
	return file, nil
}

// An argument names a path as a whole, or after the = of an option or of a
// setting: --index=/path, DATA=/path.
func candidates(arg string) []string {
	out := []string{arg}
	if i := strings.IndexByte(arg, '='); i >= 0 && i+1 < len(arg) {
		out = append(out, arg[i+1:])
	}
	return out
}

// InsideArgument returns the first argument of a command that names
// something inside the Study home, or reached through it, and its number
// from 1; 0 when none does.
//
// An argument is taken for a path when it is absolute or starts with ~/, as
// a whole or after an =. It catches the honest mistake, a script or a
// setting of the plugin's kept in a Topic, and is no guarantee: a command
// such as sh -c, env or npx resolves more when it starts than any reading of
// its arguments can see.
func (w *Walker) InsideArgument(env Env, command []string) (int, string) {
	for i, arg := range command {
		if i == 0 {
			continue
		}
		for _, c := range candidates(arg) {
			if c != "~" && !strings.HasPrefix(c, "~/") && !filepath.IsAbs(c) {
				continue
			}
			if w.Inside(expandKeeping(env, c)) {
				return i, arg
			}
		}
	}
	return 0, ""
}

// expandKeeping makes ~ the home folder and leaves the rest of the path as
// it is, .. included: the walk takes each from the folder it is really in.
func expandKeeping(env Env, path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home := env.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return home + strings.TrimPrefix(path, "~")
}

// RelativeArgument returns the first argument of a command that names an
// existing file by a path relative to the folder study started in, and its
// number from 1; 0 when none does. A plugin is started from the registry's
// folder, not from where its command was typed, so it would not find the
// file.
//
// A word is taken for such a path when a regular file has that name, or
// when it is written as a path, with a /, and anything has that name. A
// bare word that only a folder shares its name with, as a subcommand such
// as build or test easily does, is left alone.
func RelativeArgument(env Env, command []string) (int, string) {
	for i, arg := range command {
		if i == 0 {
			continue
		}
		for _, c := range candidates(arg) {
			if c == "" || c == "~" || strings.HasPrefix(c, "~/") || filepath.IsAbs(c) || strings.HasPrefix(c, "-") {
				continue
			}
			info, err := os.Stat(filepath.Join(env.Dir, c))
			if err != nil {
				continue
			}
			if info.Mode().IsRegular() || strings.ContainsRune(c, '/') || strings.ContainsRune(c, filepath.Separator) {
				return i, arg
			}
		}
	}
	return 0, ""
}
