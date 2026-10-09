package regfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regtest"
)

func TestCheckName(t *testing.T) {
	for _, name := range []string{"shelf", "a", "kb-2", "study-shelf-nightly", "9", strings.Repeat("a", 64)} {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	for name, want := range map[string]string{
		"":                      "name the Knowledge base plugin",
		"Shelf":                 "not a valid name",
		"my_kb":                 "not a valid name",
		"my kb":                 "not a valid name",
		"../kb":                 "not a valid name",
		"-kb":                   "not a valid name",
		"kb-":                   "not a valid name",
		"a--b":                  "not a valid name",
		"kb\n":                  "not a valid name",
		"shélf":                 "not a valid name",
		strings.Repeat("a", 65): "up to 64 characters",
		"none":                  "kind of Knowledge base",
		"plugin":                "kind of Knowledge base",
	} {
		if err := CheckName(name); CodeOf(err) != CodeInvalidArgument || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckName(%q) = %v; want invalid_argument with %q", name, err, want)
		}
	}
}

func TestCheckCommandTakesTextATerminalShowsAsItIs(t *testing.T) {
	ok := [][]string{
		{"/bin/sh"},
		{"/bin/sh", "", "-c", "echo 'a b' \"c\" $HOME", "--flag=value", "a\\b", "ünïcödé 語 🙂"},
		append([]string{"/bin/sh"}, make([]string, MaxArgs)...),
		{"/bin/sh", strings.Repeat("語", MaxWordRunes)},
		// A zero-width joiner is part of how some scripts are written.
		{"/bin/sh", "क्\u200dष"},
	}
	for _, command := range ok {
		if err := CheckCommand(command); err != nil {
			t.Errorf("CheckCommand(%.60q) = %v", command, err)
		}
	}
	for _, tc := range []struct {
		command []string
		want    string
	}{
		{[]string{"/bin/sh", "\xff"}, "argument 1 of the command is not valid UTF-8"},
		{[]string{"/bin/sh", "ok", "a\nb"}, "argument 2 of the command contains a control character"},
		{[]string{"/bin/sh", "a\tb"}, "control character"},
		{[]string{"/bin/sh", "a\x00b"}, "control character"},
		{[]string{"/bin/sh", "a\x1b[2Jb"}, "control character"},
		{[]string{"/bin/sh", "a\u0085b"}, "control character"},
		{[]string{"/bin/s\x1bh"}, "the program's path contains a control character"},
		// Every character that changes the direction text is shown in: the
		// ones the command line quotes before it prints anything.
		{[]string{"/bin/sh", "a\u202eb"}, "bidirectional control character (U+202E)"},
		{[]string{"/bin/sh", "a\u202ab"}, "bidirectional control character (U+202A)"},
		{[]string{"/bin/sh", "a\u2066b"}, "bidirectional control character (U+2066)"},
		{[]string{"/bin/sh", "a\u2069b"}, "bidirectional control character (U+2069)"},
		{[]string{"/bin/sh", "a\u200eb"}, "bidirectional control character (U+200E)"},
		{[]string{"/bin/sh", "a\u200fb"}, "bidirectional control character (U+200F)"},
		{[]string{"/bin/sh", "a\u061cb"}, "bidirectional control character (U+061C)"},
		{[]string{"/bin/sh", "a�b"}, "replacement character"},
		{[]string{"/bin/sh", strings.Repeat("a", MaxWordRunes+1)}, "longer than 4096"},
		{append([]string{"/bin/sh"}, make([]string, MaxArgs+1)...), "more than 100 arguments"},
	} {
		err := CheckCommand(tc.command)
		if CodeOf(err) != CodeInvalidArgument || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CheckCommand(%.40q) = %v; want invalid_argument with %q", tc.command, err, tc.want)
		}
	}
}

func TestCleanURL(t *testing.T) {
	for raw, want := range map[string]string{
		"http://localhost:8765/mcp":               "http://localhost:8765/mcp",
		" HTTP://LocalHost:8765/mcp?key=a&b=<c> ": "http://localhost:8765/mcp?key=a&b=<c>",
		"http://localhost:80/":                    "http://localhost/",
		"https://kb.example:443/mcp":              "https://kb.example/mcp",
		"https://kb.example:80/mcp":               "https://kb.example:80/mcp",
		"http://localhost:1":                      "http://localhost:1",
		"http://localhost:65535/":                 "http://localhost:65535/",
		"http://localhost:08765/mcp":              "http://localhost:8765/mcp",
		"http://[::1]:8765/mcp":                   "http://[::1]:8765/mcp",
		"http://127.0.0.1/mcp/":                   "http://127.0.0.1/mcp/",
		"https://BÜCHER.example/mcp":              "https://bücher.example/mcp",
		"http://localhost/a%20b?q=%23":            "http://localhost/a%20b?q=%23",
		"http://localhost/mcp?token=abc@def":      "http://localhost/mcp?token=abc@def",
	} {
		got, err := CleanURL(raw)
		if err != nil || got != want {
			t.Errorf("CleanURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
		// What it gives is what it takes: cleaning again changes nothing.
		if again, err := CleanURL(got); err != nil || again != got {
			t.Errorf("CleanURL(%q), cleaned already, = %q, %v", got, again, err)
		}
	}
	for raw, want := range map[string]string{
		"":                                              "not a web address",
		"localhost:8765/mcp":                            "not a web address",
		"//localhost/mcp":                               "not a web address",
		"/mcp":                                          "not a web address",
		"http:///mcp":                                   "not a web address",
		"http://:8765/mcp":                              "not a web address",
		"http://local host/mcp":                         "not a web address",
		"ftp://localhost/kb":                            "not a web address",
		"file:///usr/bin/study-shelf":                   "not a web address",
		"unix:///run/kb.sock":                           "not a web address",
		"javascript:alert(1)":                           "not a web address",
		"mailto:ada@example.com":                        "not a web address",
		"http://ada:secret@localhost/mcp":               "user name or password",
		"http://ada@localhost/mcp":                      "user name or password",
		"http://:secret@localhost/mcp":                  "user name or password",
		"http://localhost/mcp#frag":                     "#fragment",
		"http://localhost/mcp#":                         "#fragment",
		"http://localhost:0/mcp":                        "port from 1 to 65535",
		"http://localhost:65536/mcp":                    "port from 1 to 65535",
		"http://localhost:99999999999999/mcp":           "port from 1 to 65535",
		"http://localhost/\x1b[2J":                      "control character",
		"http://localhost/a\u202eb":                     "bidirectional control character",
		"http://localhost/" + strings.Repeat("a", 2000): "longer than 2000",
		// A port that is no number is refused by the parser itself, whose
		// error would repeat the address.
		"http://ada:secret@localhost:port/mcp": "not a web address",
	} {
		got, err := CleanURL(raw)
		if CodeOf(err) != CodeInvalidArgument || !strings.Contains(err.Error(), want) || got != "" {
			t.Errorf("CleanURL(%.50q) = %q, %v; want invalid_argument with %q", raw, got, err, want)
		}
		// No error repeats the address: it may hold a password or a key.
		if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "ada")) {
			t.Errorf("CleanURL(%q) repeats what it was given: %v", raw, err)
		}
	}
}

func TestResolveProgramFindsItWhereTheLearnerNamesIt(t *testing.T) {
	c := regtest.New(t)
	tools := filepath.Join(c.Home, "tools")
	kb := regtest.Program(t, filepath.Join(tools, "kb"))
	// A link, as a package manager keeps one to the newest version.
	versioned := regtest.Program(t, filepath.Join(c.Home, "opt", "shelf-2.0.0", "study-shelf"))
	link := regtest.Symlink(t, versioned, filepath.Join(c.Bin, "study-shelf"))
	// A folder named like the program, a file that cannot be run, and a
	// folder that is not there, all earlier on PATH, are passed over.
	early := regtest.Mkdir(t, filepath.Join(c.Home, "early"))
	regtest.Mkdir(t, filepath.Join(early, "study-shelf"))
	notRunnable := filepath.Join(c.Home, "plain")
	regtest.WriteFile(t, filepath.Join(notRunnable, "study-shelf"), "notes")
	c.Vars["PATH"] = strings.Join([]string{filepath.Join(c.Home, "gone"), early, notRunnable, c.Bin}, string(os.PathListSeparator))

	w := NewWalker(c.Study)
	defer w.Close()
	for _, tc := range []struct{ name, dir, program, want string }{
		{"a path relative to the folder study started in", tools, "./kb", kb},
		{"a path below that folder", c.Home, "tools/kb", kb},
		{"a path from the home folder", c.Study, "~/tools/kb", kb},
		{"an absolute path", c.Study, kb, kb},
		{"an absolute path with a . in it", c.Study, tools + "/./kb", kb},
		{"a name on PATH, kept as the link it is", c.Study, "study-shelf", link},
	} {
		got, err := w.ResolveProgram(envOf(c, tc.dir), tc.program)
		if err != nil || got != tc.want {
			t.Errorf("%s: ResolveProgram(%q) from %s = %q, %v; want %s", tc.name, tc.program, tc.dir, got, err, tc.want)
		}
	}
}

func TestResolveProgramRefusals(t *testing.T) {
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	// A program in the folder study is started in, which is this process's
	// folder too: a relative entry of PATH leads there, however it is read.
	started := regtest.Mkdir(t, filepath.Join(c.Home, "started"))
	regtest.Program(t, filepath.Join(started, "here"))
	t.Chdir(started)
	plain := filepath.Join(c.Home, "notes.txt")
	regtest.WriteFile(t, plain, "notes")
	// Through a link, .. leads somewhere a name does not say.
	deep := regtest.Mkdir(t, filepath.Join(c.Home, "a", "b"))
	regtest.Program(t, filepath.Join(c.Home, "a", "kb"))
	regtest.Program(t, filepath.Join(c.Home, "kb"))
	jump := regtest.Symlink(t, deep, filepath.Join(c.Home, "jump"))

	for _, tc := range []struct {
		name, program string
		path          string // PATH, when not the computer's
		code          string
		want          string
	}{
		{name: "an empty program", program: "", code: CodeInvalidArgument, want: "the command is empty"},
		{name: "a program that is not on PATH", program: "study-shelf-nightly", code: CodeNotFound, want: "PATH"},
		{name: "a program in the folder study started in, PATH naming it as .", program: "here", path: ".", code: CodeNotFound, want: "PATH"},
		{name: "the same with an empty entry in PATH", program: "here", path: string(os.PathListSeparator) + c.Bin, code: CodeNotFound, want: "PATH"},
		{name: "the same with a relative entry", program: "here", path: "../started", code: CodeNotFound, want: "PATH"},
		{name: "a folder of PATH named with ..", program: "study-shelf", path: c.Home + "/tools/../bin", code: CodeNotFound, want: "PATH"},
		{name: "a program that is not there", program: filepath.Join(c.Bin, "gone"), code: CodeNotFound, want: "no program at"},
		{name: "a folder", program: c.Bin, code: CodeInvalidArgument, want: "a folder, not a program"},
		{name: "the home folder", program: "~", code: CodeInvalidArgument, want: "a folder, not a program"},
		{name: "a file that cannot be run", program: plain, code: CodeInvalidArgument, want: "not a program you can run"},
		{name: "a program with a control character", program: "study\x1bshelf", code: CodeInvalidArgument, want: "control character"},
		{name: "a file below a file", program: shelf + "/x", code: CodeFailedPrecondition, want: "cannot follow"},
		// By name this is ~/kb; the operating system would run ~/a/kb.
		{name: "a path with .. after a link", program: jump + "/../kb", code: CodeInvalidArgument, want: "has .. in it"},
		{name: "a relative path with ..", program: "../bin/study-shelf", code: CodeInvalidArgument, want: "has .. in it"},
		{name: "a path that ends in ..", program: c.Bin + "/..", code: CodeInvalidArgument, want: "has .. in it"},
		{name: "a path from the home folder with ..", program: "~/../x", code: CodeInvalidArgument, want: "has .. in it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := c
			if tc.path != "" {
				q = c.With(map[string]string{"PATH": tc.path})
			}
			w := NewWalker(q.StudyHome())
			defer w.Close()
			got, err := w.ResolveProgram(envOf(q, started), tc.program)
			if CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.want) || got != "" {
				t.Errorf("ResolveProgram(%q) = %q, %v (%s); want %s with %q", tc.program, got, err, CodeOf(err), tc.code, tc.want)
			}
			if err != nil && strings.ContainsFunc(err.Error(), func(r rune) bool { return r < ' ' || r == 0x7f }) {
				t.Errorf("the error carries a control character: %q", err.Error())
			}
		})
	}
}

// A program is refused when its path leads through the Study home at any
// hop: where it is, where a link on the way is, and by any name the Study
// home has. CheckProgram is what a registration and a listing ask;
// OpenProgram is what starting the program asks, and gives the file itself.
func TestProgramsThroughTheStudyHomeAreRefused(t *testing.T) {
	c := regtest.New(t)
	topic := regtest.Mkdir(t, filepath.Join(c.Study, "go-concurrency"))
	inside := regtest.Program(t, filepath.Join(topic, "tools", "kb"))
	outside := regtest.Program(t, filepath.Join(c.Home, "opt", "kb"))
	// A link outside the Study home to a program inside it, and a link
	// inside to a program outside: the agent can change what either runs.
	linkOut := regtest.Symlink(t, inside, filepath.Join(c.Bin, "kb-link"))
	linkIn := regtest.Symlink(t, outside, filepath.Join(c.Study, "kb-link"))
	// A link outside to that link: the first hop and the last are outside
	// the Study home, and the one between is the agent's to repoint.
	through := regtest.Symlink(t, linkIn, filepath.Join(c.Bin, "kb-through"))
	// A folder outside that leads inside, and a relative link there that
	// is resolved from where the folder really is.
	dirLink := regtest.Symlink(t, topic, filepath.Join(c.Home, "topic"))
	regtest.Mkdir(t, filepath.Join(topic, "sub"))
	regtest.Symlink(t, filepath.Join("..", "tools", "kb"), filepath.Join(topic, "sub", "up"))
	viaSub := regtest.Symlink(t, filepath.Join(topic, "sub"), filepath.Join(c.Home, "sub"))
	// The Study home under another name.
	alias := regtest.Symlink(t, c.Study, filepath.Join(c.Base, "alias"))

	for _, tc := range []struct {
		name    string
		vars    map[string]string
		dir     string
		program string
	}{
		{name: "by its path", program: inside},
		{name: "by a path relative to a Topic", dir: topic, program: "tools/kb"},
		{name: "through a link that leads into the Study home", program: linkOut},
		{name: "through that link, found on PATH", program: "kb-link"},
		{name: "through a link in the Study home", program: linkIn},
		{name: "through a link to a link in the Study home, which leads out again", program: through},
		{name: "through that chain, found on PATH", program: "kb-through"},
		{name: "through a folder that leads into the Study home", program: filepath.Join(dirLink, "tools", "kb")},
		{name: "through a relative link in a folder reached by another name", program: filepath.Join(viaSub, "up")},
		{name: "on a PATH that holds a folder of the Study home", program: "kb", vars: map[string]string{"PATH": filepath.Join(topic, "tools")}},
		{name: "in a Study home named through a link", program: inside, vars: map[string]string{"STUDY_HOME": alias}},
		{name: "by another name of the Study home", program: filepath.Join(alias, "go-concurrency", "tools", "kb")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := c.With(tc.vars)
			dir := tc.dir
			if dir == "" {
				dir = c.Home
			}
			w := NewWalker(q.StudyHome())
			defer w.Close()
			got, err := w.ResolveProgram(envOf(q, dir), tc.program)
			if CodeOf(err) != CodeInvalidArgument || !IsInside(err) || !strings.Contains(err.Error(), "inside the Study home") || got != "" {
				t.Fatalf("ResolveProgram(%q) = %q, %v; want invalid_argument, inside the Study home", tc.program, got, err)
			}
			if !filepath.IsAbs(tc.program) {
				return
			}
			if err := w.CheckProgram(tc.program); !IsInside(err) {
				t.Errorf("CheckProgram = %v; want it refused as inside the Study home", err)
			}
			if file, err := w.OpenProgram(tc.program); !IsInside(err) || file != nil {
				t.Errorf("OpenProgram = %v, %v; want it refused as inside the Study home", file, err)
			}
		})
	}

	// The same program is taken from where the agent cannot write, and
	// OpenProgram gives that very file.
	w := NewWalker(c.Study)
	defer w.Close()
	if err := w.CheckProgram(outside); err != nil {
		t.Fatalf("a program outside the Study home: %v", err)
	}
	file, err := w.OpenProgram(outside)
	if err != nil {
		t.Fatalf("OpenProgram: %v", err)
	}
	defer file.Close()
	want, _ := os.Stat(outside)
	if got, err := file.Stat(); err != nil || !os.SameFile(want, got) {
		t.Errorf("OpenProgram opened another file than %s (%v)", outside, err)
	}
	if data, err := io.ReadAll(file); err != nil || !strings.HasPrefix(string(data), "#!/bin/sh") {
		t.Errorf("the open program reads %q, %v", data, err)
	}

	// The agent repoints its link after the check: the open file is still
	// the one that was checked.
	checked := regtest.Symlink(t, outside, filepath.Join(c.Home, "current"))
	open, err := w.OpenProgram(checked)
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	if err := os.Remove(checked); err != nil {
		t.Fatal(err)
	}
	regtest.Symlink(t, inside, checked)
	if got, err := open.Stat(); err != nil || !os.SameFile(want, got) {
		t.Errorf("the open file changed with its name (%v)", err)
	}
	if file, err := w.OpenProgram(checked); !IsInside(err) || file != nil {
		t.Errorf("OpenProgram after the link was repointed = %v, %v; want it refused", file, err)
	}
}

func TestOpenProgramRefusesWhatIsNoProgram(t *testing.T) {
	c := regtest.New(t)
	plain := filepath.Join(c.Home, "notes.txt")
	regtest.WriteFile(t, plain, "notes")
	w := NewWalker(c.Study)
	defer w.Close()
	for path, code := range map[string]string{
		filepath.Join(c.Bin, "gone"): CodeNotFound,
		c.Bin:                        CodeInvalidArgument,
		plain:                        CodeInvalidArgument,
	} {
		if file, err := w.OpenProgram(path); CodeOf(err) != code || file != nil {
			t.Errorf("OpenProgram(%s) = %v, %v; want %s", path, file, err, code)
		}
	}
}

// A program is one the user study runs as may run. An execute bit that is
// there for someone else, for others or for the group on a file the user
// owns, does not make it one: the system is asked, not the bits.
func TestAProgramIsOneThisUserMayRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may run any file with an execute bit, whoever the bit is for")
	}
	c := regtest.New(t)
	w := NewWalker(c.Study)
	defer w.Close()
	for _, tc := range []struct {
		mode     os.FileMode
		runnable bool
	}{
		{0o001, false}, // for others, and the user owns the file
		{0o010, false}, // for the group, and the user owns the file
		{0o011, false},
		{0o655, false},
		{0o644, false},
		{0o000, false},
		{0o100, true},
		{0o500, true},
		{0o744, true},
		{0o755, true},
	} {
		name := fmt.Sprintf("kb-%03o", tc.mode)
		path := regtest.Program(t, filepath.Join(c.Bin, name))
		if err := os.Chmod(path, tc.mode); err != nil {
			t.Fatal(err)
		}
		err := w.CheckProgram(path)
		found, perr := w.ResolveProgram(envOf(c, c.Home), name)
		byPath, rerr := w.ResolveProgram(envOf(c, c.Home), path)
		if tc.runnable {
			if err != nil || perr != nil || found != path || rerr != nil || byPath != path {
				t.Errorf("mode %v: CheckProgram = %v; found on PATH %q, %v; by its path %q, %v; want it taken", tc.mode, err, found, perr, byPath, rerr)
			}
			continue
		}
		if CodeOf(err) != CodeInvalidArgument || !strings.Contains(err.Error(), "not a program you can run") || !strings.Contains(err.Error(), "chmod u+x") {
			t.Errorf("mode %v: CheckProgram = %v; want invalid_argument, not a program you can run", tc.mode, err)
		}
		// On PATH it is passed over, as a shell passes it over.
		if CodeOf(perr) != CodeNotFound || found != "" {
			t.Errorf("mode %v: found on PATH %q, %v; want not_found", tc.mode, found, perr)
		}
		if CodeOf(rerr) != CodeInvalidArgument || byPath != "" {
			t.Errorf("mode %v: by its path %q, %v; want invalid_argument", tc.mode, byPath, rerr)
		}
		if file, err := w.OpenProgram(path); CodeOf(err) != CodeInvalidArgument || !strings.Contains(err.Error(), "not a program you can run") || file != nil {
			t.Errorf("mode %v: OpenProgram = %v, %v; want invalid_argument, not a program you can run", tc.mode, file, err)
		}
	}

	// To be started, a program is opened, and so must be one the user may
	// read as well.
	readable := filepath.Join(c.Bin, "kb-500")
	file, err := w.OpenProgram(readable)
	if err != nil {
		t.Fatalf("OpenProgram of a program the user may read and run: %v", err)
	}
	file.Close()
	if file, err := w.OpenProgram(filepath.Join(c.Bin, "kb-100")); CodeOf(err) != CodeFailedPrecondition || file != nil {
		t.Errorf("OpenProgram of a program the user may run and not read = %v, %v; want failed_precondition", file, err)
	}
}

func TestInsideArgument(t *testing.T) {
	c := regtest.New(t)
	script := regtest.Program(t, filepath.Join(c.Study, "topic", "kb.py"))
	data := regtest.Mkdir(t, filepath.Join(c.Home, "data"))
	regtest.Symlink(t, script, filepath.Join(c.Home, "linked.py"))
	// The Study home below the home folder, to name it with ~.
	near := c.With(map[string]string{"STUDY_HOME": regtest.Mkdir(t, filepath.Join(c.Home, "study"))})
	regtest.WriteFile(t, filepath.Join(c.Home, "study", "kb.toml"), "x")

	for _, tc := range []struct {
		name    string
		on      *regtest.Computer
		args    []string
		want    int
		wantArg string
	}{
		{name: "no arguments", on: c},
		{name: "arguments outside", on: c, args: []string{"serve", "--stdio", data, "--data=" + data, "PORT=8765", "~/data"}},
		{name: "a script in a Topic", on: c, args: []string{"-u", script}, want: 2, wantArg: script},
		{name: "after the = of an option", on: c, args: []string{"--script=" + script}, want: 1, wantArg: "--script=" + script},
		{name: "after the = of a setting", on: c, args: []string{"serve", "CONF=" + script}, want: 2, wantArg: "CONF=" + script},
		{name: "after the first = only", on: c, args: []string{"--env=CONF=" + script}},
		{name: "the Study home itself", on: c, args: []string{"--allow", c.Study}, want: 2, wantArg: c.Study},
		{name: "something not there yet in the Study home", on: c, args: []string{filepath.Join(c.Study, "new", "index.db")}, want: 1,
			wantArg: filepath.Join(c.Study, "new", "index.db")},
		{name: "through a link outside", on: c, args: []string{filepath.Join(c.Home, "linked.py")}, want: 1, wantArg: filepath.Join(c.Home, "linked.py")},
		{name: "into the Study home and out again", on: c, args: []string{c.Study + "/../home/data"}, want: 1, wantArg: c.Study + "/../home/data"},
		{name: "from the home folder", on: near, args: []string{"~/study/kb.toml"}, want: 1, wantArg: "~/study/kb.toml"},
		{name: "from the home folder, after an =", on: near, args: []string{"ok", "--config=~/study/kb.toml"}, want: 2, wantArg: "--config=~/study/kb.toml"},
		{name: "the first of two", on: c, args: []string{script, c.Study}, want: 1, wantArg: script},
		// Relative to nothing study knows: the plugin is not started here.
		{name: "a relative path", on: c, args: []string{"study/topic/kb.py", "../study"}},
		{name: "a path in text", on: c, args: []string{"-c", "python3 " + script}},
	} {
		w := NewWalker(tc.on.StudyHome())
		got, arg := w.InsideArgument(envOf(tc.on, c.Base), append([]string{"/usr/bin/python3"}, tc.args...))
		if got != tc.want || arg != tc.wantArg {
			t.Errorf("%s: InsideArgument(%q) = %d, %q; want %d, %q", tc.name, tc.args, got, arg, tc.want, tc.wantArg)
		}
		w.Close()
	}
}

func TestRelativeArgument(t *testing.T) {
	c := regtest.New(t)
	started := regtest.Mkdir(t, filepath.Join(c.Home, "started"))
	regtest.WriteFile(t, filepath.Join(started, "kb.toml"), "x")
	regtest.WriteFile(t, filepath.Join(started, "serve"), "a file named like a subcommand")
	regtest.WriteFile(t, filepath.Join(started, "conf", "kb.toml"), "x")
	regtest.Mkdir(t, filepath.Join(started, "build"))
	regtest.Symlink(t, "kb.toml", filepath.Join(started, "current.toml"))

	for _, tc := range []struct {
		name    string
		args    []string
		want    int
		wantArg string
	}{
		{name: "no arguments"},
		{name: "words that name nothing here", args: []string{"run", "--stdio", "-v", "PORT=8765", "--name=shelf", ""}},
		{name: "full paths", args: []string{filepath.Join(started, "kb.toml"), "~/started/kb.toml", "--config=" + filepath.Join(started, "kb.toml")}},
		{name: "a file here", args: []string{"--config", "kb.toml"}, want: 2, wantArg: "kb.toml"},
		{name: "a file here, after an =", args: []string{"--config=kb.toml"}, want: 1, wantArg: "--config=kb.toml"},
		{name: "a file here, written as a path", args: []string{"./kb.toml"}, want: 1, wantArg: "./kb.toml"},
		{name: "a file in a folder here", args: []string{"conf/kb.toml"}, want: 1, wantArg: "conf/kb.toml"},
		{name: "a link to a file here", args: []string{"current.toml"}, want: 1, wantArg: "current.toml"},
		{name: "a folder here, written as a path", args: []string{"--out", "./build"}, want: 2, wantArg: "./build"},
		{name: "a folder here with a slash", args: []string{"build/"}, want: 1, wantArg: "build/"},
		{name: "a file up from here", args: []string{"../notes.txt"}},
		// A bare word that only a folder shares its name with is a word.
		{name: "a subcommand named like a folder here", args: []string{"build"}},
		{name: "a subcommand named like a file here", args: []string{"serve"}, want: 1, wantArg: "serve"},
		{name: "an option named like nothing here", args: []string{"--serve"}},
	} {
		got, arg := RelativeArgument(envOf(c, started), append([]string{"/usr/bin/kb"}, tc.args...))
		if got != tc.want || arg != tc.wantArg {
			t.Errorf("%s: RelativeArgument(%q) = %d, %q; want %d, %q", tc.name, tc.args, got, arg, tc.want, tc.wantArg)
		}
	}
}
