package cli_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordor-forge/lamplight/v2/internal/cli"
)

// kbComputer is a learner's computer as study knowledge-base sees it: a home
// folder with Lamplight's configuration folder in it, study-shelf and
// notes-kb on PATH, and the Study home in ~/study, apart from both.
type kbComputer struct {
	home string
	env  map[string]string
}

func newKBComputer(t *testing.T) *kbComputer {
	t.Helper()
	home := t.TempDir()
	k := &kbComputer{home: home, env: map[string]string{
		"HOME": home, "STUDY_HOME": filepath.Join(home, "study"), "PATH": filepath.Join(home, "bin"),
	}}
	for _, name := range []string{"study-shelf", "notes-kb"} {
		k.program(t, filepath.Join(home, "bin", name))
	}
	return k
}

// program puts a program the learner can run at path.
func (k *kbComputer) program(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// run runs study on the computer, started in its home folder.
func (k *kbComputer) run(t *testing.T, args ...string) result {
	t.Helper()
	return runEnv(t, k.env, k.home, nil, args...)
}

// must runs study and fails the test unless it succeeds.
func (k *kbComputer) must(t *testing.T, args ...string) result {
	t.Helper()
	r := k.run(t, args...)
	if r.code != cli.ExitOK {
		t.Fatalf("study %s: exit %d, stdout %q, stderr %q", strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r
}

func (k *kbComputer) registry() string {
	return filepath.Join(k.home, ".config", "lamplight", "knowledge-base-plugins.json")
}

// withPlugins is a computer with a command plugin, shelf, and one reached by
// URL, remote.
func withPlugins(t *testing.T) *kbComputer {
	t.Helper()
	k := newKBComputer(t)
	k.must(t, "knowledge-base", "add", "shelf", "--", "study-shelf", "--recipe", "embedding gemma")
	k.must(t, "knowledge-base", "add", "remote", "--url", "http://localhost:8765/mcp")
	return k
}

// withAProgramInTheStudyHome is withPlugins run with a Study home that holds
// shelf's program: the Study home was another when shelf was registered.
func withAProgramInTheStudyHome(t *testing.T) *kbComputer {
	t.Helper()
	k := withPlugins(t)
	k.env["STUDY_HOME"] = filepath.Join(k.home, "bin")
	return k
}

// withAScriptInATopic is a computer whose Study home has a Topic with a
// script in it, kb.py, a notes folder beside the Study home, and a file,
// kb.toml, in the folder study is started in.
func withAScriptInATopic(t *testing.T) *kbComputer {
	t.Helper()
	k := newKBComputer(t)
	writeFile(t, filepath.Join(k.home, "study", "go-concurrency", "kb.py"), "print()\n")
	writeFile(t, filepath.Join(k.home, "notes", "index.md"), "# Notes\n")
	writeFile(t, filepath.Join(k.home, "kb.toml"), "port = 8765\n")
	return k
}

// withAnAllowedArgument is withAScriptInATopic with two plugins registered:
// kb, whose argument inside the Study home the learner allowed, and notes,
// whose argument is outside it.
func withAnAllowedArgument(t *testing.T) *kbComputer {
	t.Helper()
	k := withAScriptInATopic(t)
	k.must(t, "knowledge-base", "add", "kb", "--allow-study-home-arguments", "--", "study-shelf", "-u", filepath.Join(k.home, "study", "go-concurrency", "kb.py"))
	k.must(t, "knowledge-base", "add", "notes", "--", "notes-kb", "=serve", "--root="+filepath.Join(k.home, "notes"))
	return k
}

// withAnArgumentInTheStudyHome is withAnAllowedArgument run with a Study
// home that holds what the argument of notes names: the Study home was
// another when notes was registered.
func withAnArgumentInTheStudyHome(t *testing.T) *kbComputer {
	t.Helper()
	k := withAnAllowedArgument(t)
	k.env["STUDY_HOME"] = filepath.Join(k.home, "notes")
	return k
}

// withALockThatIsALink is a computer where the registry's lock is a link to
// the registry's own name.
func withALockThatIsALink(t *testing.T) *kbComputer {
	t.Helper()
	k := newKBComputer(t)
	lock := filepath.Join(filepath.Dir(k.registry()), "knowledge-base-plugins.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("knowledge-base-plugins.json", lock); err != nil {
		t.Fatal(err)
	}
	return k
}

func withRegistry(content string) func(*testing.T) *kbComputer {
	return func(t *testing.T) *kbComputer {
		t.Helper()
		k := newKBComputer(t)
		writeFile(t, k.registry(), content)
		return k
	}
}

func TestKnowledgeBaseJSON(t *testing.T) {
	for _, tc := range []struct {
		golden   string
		computer func(*testing.T) *kbComputer
		args     []string
		code     int
	}{
		{"knowledge_base_list_empty", newKBComputer, []string{"knowledge-base", "list", "--json"}, cli.ExitOK},
		{"knowledge_base_add", newKBComputer, []string{"knowledge-base", "add", "shelf", "--json", "--", "study-shelf", "--recipe", "embedding gemma"}, cli.ExitOK},
		{"knowledge_base_add_dry_run", newKBComputer, []string{"knowledge-base", "add", "shelf", "--dry-run", "--json", "--", "study-shelf"}, cli.ExitOK},
		{"knowledge_base_add_url", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "HTTP://LocalHost:8765/mcp", "--json"}, cli.ExitOK},
		{"knowledge_base_add_unchanged", withPlugins, []string{"knowledge-base", "add", "shelf", "--json", "--", "study-shelf", "--recipe", "embedding gemma"}, cli.ExitOK},
		{"knowledge_base_add_exists", withPlugins, []string{"knowledge-base", "add", "shelf", "--json", "--", "notes-kb"}, cli.ExitError},
		{"knowledge_base_add_replace", withPlugins, []string{"knowledge-base", "add", "shelf", "--replace", "--json", "--", "notes-kb", "--stdio"}, cli.ExitOK},
		{"knowledge_base_add_replace_dry_run", withPlugins, []string{"knowledge-base", "add", "remote", "--replace", "--dry-run", "--json", "--", "notes-kb"}, cli.ExitOK},
		{"knowledge_base_add_not_on_path", newKBComputer, []string{"knowledge-base", "add", "shelf", "--json", "--", "study-shelf-nightly"}, cli.ExitError},
		{"knowledge_base_add_bad_name", newKBComputer, []string{"knowledge-base", "add", "My Shelf", "--json", "--", "study-shelf"}, cli.ExitUsage},
		{"knowledge_base_add_reserved_name", newKBComputer, []string{"knowledge-base", "add", "none", "--url", "http://localhost:8765/mcp", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_bad_url", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "file:///usr/bin/study-shelf", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_url_password", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "http://ada:secret@localhost:8765/mcp", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_url_fragment", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "http://localhost:8765/mcp#top", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_url_port", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "http://localhost:0/mcp", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_dots_in_program", newKBComputer, []string{"knowledge-base", "add", "shelf", "--json", "--", "$HOME/bin/../bin/study-shelf"}, cli.ExitUsage},
		{"knowledge_base_add_argument_in_study_home", withAScriptInATopic, []string{"knowledge-base", "add", "kb", "--json", "--", "study-shelf", "-u", "$HOME/study/go-concurrency/kb.py"}, cli.ExitUsage},
		{"knowledge_base_add_argument_in_study_home_dry_run", withAScriptInATopic, []string{"knowledge-base", "add", "kb", "--dry-run", "--json", "--", "study-shelf", "--script=$HOME/study/go-concurrency/kb.py"}, cli.ExitUsage},
		{"knowledge_base_add_argument_allowed", withAScriptInATopic, []string{"knowledge-base", "add", "kb", "--allow-study-home-arguments", "--json", "--", "study-shelf", "-u", "$HOME/study/go-concurrency/kb.py"}, cli.ExitOK},
		{"knowledge_base_add_argument_allowed_without_need", withAScriptInATopic, []string{"knowledge-base", "add", "notes", "--allow-study-home-arguments", "--json", "--", "notes-kb", "$HOME/notes"}, cli.ExitOK},
		{"knowledge_base_add_allow_with_url", newKBComputer, []string{"knowledge-base", "add", "remote", "--allow-study-home-arguments", "--url", "http://localhost:8765/mcp", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_relative_argument", withAScriptInATopic, []string{"knowledge-base", "add", "kb", "--json", "--", "study-shelf", "--config=kb.toml"}, cli.ExitUsage},
		{"knowledge_base_add_lock_is_a_link", withALockThatIsALink, []string{"knowledge-base", "add", "remote", "--url", "http://localhost:8765/mcp", "--json"}, cli.ExitError},
		{"knowledge_base_add_lock_is_a_link_dry_run", withALockThatIsALink, []string{"knowledge-base", "add", "remote", "--dry-run", "--url", "http://localhost:8765/mcp", "--json"}, cli.ExitError},
		{"knowledge_base_list_allowed", withAnAllowedArgument, []string{"knowledge-base", "list", "--json"}, cli.ExitOK},
		{"knowledge_base_list_argument_problem", withAnArgumentInTheStudyHome, []string{"knowledge-base", "list", "--json"}, cli.ExitOK},
		{"knowledge_base_add_empty_url", newKBComputer, []string{"knowledge-base", "add", "remote", "--url", "", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_no_name", newKBComputer, []string{"knowledge-base", "add", "--json", "--", "study-shelf"}, cli.ExitUsage},
		{"knowledge_base_add_no_dash", newKBComputer, []string{"knowledge-base", "add", "shelf", "study-shelf", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_two_names", newKBComputer, []string{"knowledge-base", "add", "shelf", "kb", "--json", "--", "study-shelf"}, cli.ExitUsage},
		{"knowledge_base_add_nothing", newKBComputer, []string{"knowledge-base", "add", "shelf", "--json"}, cli.ExitUsage},
		{"knowledge_base_add_empty_command", newKBComputer, []string{"knowledge-base", "add", "shelf", "--json", "--"}, cli.ExitUsage},
		{"knowledge_base_add_command_and_url", newKBComputer, []string{"knowledge-base", "add", "shelf", "--url", "http://localhost:8765/mcp", "--json", "--", "study-shelf"}, cli.ExitUsage},
		{"knowledge_base_list", withPlugins, []string{"knowledge-base", "list", "--json"}, cli.ExitOK},
		{"knowledge_base_list_problem", withAProgramInTheStudyHome, []string{"knowledge-base", "list", "--json"}, cli.ExitOK},
		{"knowledge_base_remove_dry_run", withPlugins, []string{"knowledge-base", "remove", "shelf", "--dry-run", "--json"}, cli.ExitOK},
		{"knowledge_base_remove", withPlugins, []string{"knowledge-base", "remove", "shelf", "--json"}, cli.ExitOK},
		{"knowledge_base_remove_unknown", withPlugins, []string{"knowledge-base", "remove", "notes", "--json"}, cli.ExitError},
		{"knowledge_base_remove_no_name", withPlugins, []string{"knowledge-base", "remove", "--json"}, cli.ExitUsage},
		{"knowledge_base_no_subcommand", newKBComputer, []string{"knowledge-base", "--json"}, cli.ExitUsage},
		{"knowledge_base_newer_format", withRegistry(`{"format": 2, "plugins": {"shelf": "study-shelf"}}`), []string{"knowledge-base", "list", "--json"}, cli.ExitError},
		{"knowledge_base_damaged", withRegistry(`{"format": 1, "plugins": [{"name": "shelf", "command": ["study-shelf"]}]}`), []string{"knowledge-base", "list", "--json"}, cli.ExitError},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			k := tc.computer(t)
			args := slices.Clone(tc.args)
			for i := range args {
				args[i] = strings.ReplaceAll(args[i], "$HOME", k.home)
			}
			got := k.run(t, args...)
			if got.code != tc.code {
				t.Errorf("exit code = %d, want %d (stderr: %s)", got.code, tc.code, got.stderr)
			}
			if got.stderr != "" {
				t.Errorf("--json wrote to stderr: %q", got.stderr)
			}
			if strings.Contains(got.stdout, "secret") {
				t.Errorf("the output repeats a password: %s", got.stdout)
			}
			golden(t, tc.golden+".json", got.stdout)
		})
	}
}

func TestKnowledgeBaseHumanOutput(t *testing.T) {
	k := newKBComputer(t)
	golden(t, "knowledge_base_list_empty.txt", k.must(t, "knowledge-base", "list").stdout)
	golden(t, "knowledge_base_add_dry_run.txt", k.must(t, "knowledge-base", "add", "shelf", "--dry-run", "--", "study-shelf", "--recipe", "embedding gemma", "").stdout)
	golden(t, "knowledge_base_list_empty.txt", k.must(t, "knowledge-base", "list").stdout)
	golden(t, "knowledge_base_add.txt", k.must(t, "knowledge-base", "add", "shelf", "--", "study-shelf", "--recipe", "embedding gemma", "").stdout)
	golden(t, "knowledge_base_add_unchanged.txt", k.must(t, "knowledge-base", "add", "shelf", "--", "study-shelf", "--recipe", "embedding gemma", "").stdout)
	golden(t, "knowledge_base_add_url.txt", k.must(t, "knowledge-base", "add", "remote", "--url", "http://localhost:8765/mcp?recipe=gemma&top=5").stdout)
	golden(t, "knowledge_base_list.txt", k.must(t, "knowledge-base", "list").stdout)
	golden(t, "knowledge_base_add_replace_dry_run.txt", k.must(t, "knowledge-base", "add", "shelf", "--replace", "--dry-run", "--", "notes-kb").stdout)
	golden(t, "knowledge_base_add_replace.txt", k.must(t, "knowledge-base", "add", "shelf", "--replace", "--", "notes-kb").stdout)
	golden(t, "knowledge_base_remove_dry_run.txt", k.must(t, "knowledge-base", "remove", "remote", "--dry-run").stdout)
	golden(t, "knowledge_base_remove.txt", k.must(t, "knowledge-base", "remove", "remote").stdout)
	golden(t, "knowledge_base_list_problem.txt", withAProgramInTheStudyHome(t).must(t, "knowledge-base", "list").stdout)

	// An argument inside the Study home: refused, then allowed, and said so
	// wherever the plugin is shown.
	s := withAScriptInATopic(t)
	script := filepath.Join(s.home, "study", "go-concurrency", "kb.py")
	refused := s.run(t, "knowledge-base", "add", "kb", "--", "study-shelf", "-u", script)
	if refused.code != cli.ExitUsage || refused.stdout != "" || !strings.Contains(refused.stderr, "names something inside the Study home") ||
		!strings.Contains(refused.stderr, "--allow-study-home-arguments") {
		t.Errorf("an argument in the Study home: exit %d, stdout %q, stderr %q", refused.code, refused.stdout, refused.stderr)
	}
	golden(t, "knowledge_base_add_argument_allowed_dry_run.txt",
		s.must(t, "knowledge-base", "add", "kb", "--allow-study-home-arguments", "--dry-run", "--", "study-shelf", "-u", script).stdout)
	golden(t, "knowledge_base_add_argument_allowed.txt",
		s.must(t, "knowledge-base", "add", "kb", "--allow-study-home-arguments", "--", "study-shelf", "-u", script).stdout)
	// A word a shell would not take as it is, =serve, is shown quoted.
	s.must(t, "knowledge-base", "add", "notes", "--", "notes-kb", "=serve", "--root="+filepath.Join(s.home, "notes"))
	golden(t, "knowledge_base_list_allowed.txt", s.must(t, "knowledge-base", "list").stdout)
	golden(t, "knowledge_base_list_argument_problem.txt", withAnArgumentInTheStudyHome(t).must(t, "knowledge-base", "list").stdout)

	taken := k.run(t, "knowledge-base", "add", "shelf", "--", "study-shelf")
	if taken.code != cli.ExitError || taken.stdout != "" || !strings.Contains(taken.stderr, "registered already") || !strings.Contains(taken.stderr, "--replace") {
		t.Errorf("a name that is taken: exit %d, stdout %q, stderr %q", taken.code, taken.stdout, taken.stderr)
	}
	// A refused program is a mistake in what was typed, like any invalid
	// argument.
	k.program(t, filepath.Join(k.home, "study", "go-concurrency", "kb"))
	inside := k.run(t, "knowledge-base", "add", "kb", "--", filepath.Join(k.home, "study", "go-concurrency", "kb"))
	if inside.code != cli.ExitUsage || inside.stdout != "" || !strings.Contains(inside.stderr, "inside the Study home") {
		t.Errorf("a program in the Study home: exit %d, stdout %q, stderr %q", inside.code, inside.stdout, inside.stderr)
	}
}

// What follows -- is the plugin's command, word for word: a flag of study's
// there is an argument of the plugin's, and changes nothing about the
// registration.
func TestKnowledgeBaseAddKeepsTheCommandsOwnFlags(t *testing.T) {
	k := newKBComputer(t)
	r := k.must(t, "knowledge-base", "add", "shelf", "--", "study-shelf", "--json", "--dry-run", "--replace", "--url", "http://localhost/", "--", "-h")
	if !strings.HasPrefix(r.stdout, "Registered Knowledge base plugin shelf on this computer: $HOME/bin/study-shelf --json --dry-run --replace --url http://localhost/ -- -h\n") {
		t.Fatalf("stdout = %q", r.stdout)
	}
	listed := k.must(t, "knowledge-base", "list", "--json")
	for _, word := range []string{`"--json"`, `"--dry-run"`, `"--replace"`, `"--url"`, `"http://localhost/"`, `"--"`, `"-h"`} {
		if !strings.Contains(listed.stdout, word) {
			t.Errorf("the registered command lacks %s:\n%s", word, listed.stdout)
		}
	}
	// Before --, the same flags are study's.
	r = k.must(t, "knowledge-base", "add", "notes", "--dry-run", "--json", "--", "notes-kb", "--json")
	if !strings.Contains(r.stdout, `"dry_run": true`) || !strings.Contains(r.stdout, `"--json"`) {
		t.Errorf("study's own flags before --: %s", r.stdout)
	}
	if listed := k.must(t, "knowledge-base", "list").stdout; strings.Contains(listed, "notes") {
		t.Errorf("the dry run registered the plugin:\n%s", listed)
	}
}

// The shell completes the names registered where a registered name goes,
// files where the plugin's command goes, and nothing where a new name goes.
func TestKnowledgeBaseCompletesRegisteredNames(t *testing.T) {
	k := withPlugins(t)
	const names, files, nothing = "remote\nshelf\n:4\n", ":0\n", ":4\n"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"knowledge-base", "remove", ""}, names},
		{[]string{"knowledge-base", "remove", "sh"}, names}, // the shell keeps the ones that fit
		{[]string{"knowledge-base", "remove", "shelf", ""}, nothing},
		{[]string{"knowledge-base", "remove", "--dry-run", ""}, names},
		{[]string{"knowledge-base", "add", ""}, nothing},
		{[]string{"knowledge-base", "add", "--replace", ""}, names},
		{[]string{"knowledge-base", "add", "--replace", "shelf", ""}, nothing},
		{[]string{"knowledge-base", "add", "kb", ""}, nothing},
		{[]string{"knowledge-base", "add", "kb", "--", ""}, files},
		{[]string{"knowledge-base", "add", "kb", "--", "notes-kb", ""}, files},
		{[]string{"knowledge-base", "add", "--replace", "--", ""}, files},
		{[]string{"knowledge-base", "add", "kb", "--url", ""}, nothing},
		{[]string{"knowledge-base", "list", ""}, nothing},
	} {
		r := k.run(t, append([]string{"__complete"}, tc.args...)...)
		if r.code != cli.ExitOK || r.stdout != tc.want {
			t.Errorf("completing study %s: exit %d, stdout %q, want %q (stderr %q)", strings.Join(tc.args, " "), r.code, r.stdout, tc.want, r.stderr)
		}
	}

	// A registry that cannot be read completes nothing and says nothing: the
	// command says why when it is run.
	for name, k := range map[string]*kbComputer{
		"damaged":       withRegistry("{")(t),
		"newer":         withRegistry(`{"format": 2}`)(t),
		"no Study home": {home: t.TempDir(), env: map[string]string{"HOME": t.TempDir(), "STUDY_HOME": "relative"}},
	} {
		r := k.run(t, "__complete", "knowledge-base", "remove", "")
		if r.code != cli.ExitOK || r.stdout != nothing {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
}

// A registry the agent could write is not read: with the configuration
// folder inside the Study home, every command of study knowledge-base says
// why and does nothing, and the rest of study works as before.
func TestKnowledgeBaseIgnoresARegistryInsideTheStudyHome(t *testing.T) {
	k := newKBComputer(t)
	// The Study home is the home folder, so ~/.config is inside it.
	k.env["STUDY_HOME"] = k.home
	outside := filepath.Join(t.TempDir(), "study-shelf")
	k.program(t, outside)
	planted := `{"format": 1, "plugins": [{"name": "planted", "command": ["/bin/sh", "-c", "exit 0"]}]}`
	writeFile(t, k.registry(), planted)

	golden(t, "knowledge_base_inside_study_home.json", k.run(t, "knowledge-base", "list", "--json").stdout)
	for _, args := range [][]string{
		{"knowledge-base", "list"},
		{"knowledge-base", "add", "shelf", "--", outside},
		{"knowledge-base", "add", "remote", "--url", "http://localhost:8765/mcp"},
		{"knowledge-base", "add", "planted", "--replace", "--dry-run", "--url", "http://localhost:8765/mcp"},
		{"knowledge-base", "remove", "planted"},
		{"knowledge-base", "remove", "planted", "--dry-run"},
	} {
		r := k.run(t, args...)
		if r.code != cli.ExitError || r.stdout != "" || !strings.Contains(r.stderr, "is inside the Study home") ||
			!strings.Contains(r.stderr, "is not read") || strings.Contains(r.stderr, "planted") {
			t.Errorf("study %s: exit %d, stdout %q, stderr %q", strings.Join(args, " "), r.code, r.stdout, r.stderr)
		}
	}
	if r := k.run(t, "__complete", "knowledge-base", "remove", ""); strings.Contains(r.stdout, "planted") {
		t.Errorf("completion read the registry: %q", r.stdout)
	}
	if data, err := os.ReadFile(k.registry()); err != nil || string(data) != planted {
		t.Errorf("the registry in the Study home was written: %q, %v", data, err)
	}
	if left, err := os.ReadDir(filepath.Dir(k.registry())); err != nil || len(left) != 1 {
		t.Errorf("something was made beside it: %v, %v", left, err)
	}
	if r := k.run(t, "status"); r.code != cli.ExitOK {
		t.Errorf("study status: exit %d, stderr %q", r.code, r.stderr)
	}

	// Moved apart, the same registry is read.
	apart := maps.Clone(k.env)
	apart["STUDY_HOME"] = filepath.Join(k.home, "study")
	if r := runEnv(t, apart, k.home, nil, "knowledge-base", "list"); r.code != cli.ExitOK || !strings.Contains(r.stdout, "planted") {
		t.Errorf("with the Study home apart: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}
