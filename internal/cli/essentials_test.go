package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mordor-forge/lamplight/v2/internal/cli"
	"github.com/mordor-forge/lamplight/v2/internal/core"
)

// TestMain keeps results independent of the machine: completion scripts that
// packages installed here must not show up in doctor or install results, and
// the caller's GIT_* variables are cleared. A commit hook that runs the tests
// sets GIT_INDEX_FILE, and GIT_DIR from a linked worktree: the git and go
// commands the tests run themselves would then work on the repository being
// committed. The core drops these variables on its own.
func TestMain(m *testing.M) {
	*cli.SystemCompletionRoots = []string{filepath.Join(os.TempDir(), "study-test-no-package-completions")}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

// The import test, which builds a v1 workspace with plain git, runs the same
// under a caller's git environment and writes nothing through it.
func TestTheTestsIgnoreTheCallersGitEnvironment(t *testing.T) {
	stray := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestImportCommand$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(stray, "repo.git"),
		"GIT_WORK_TREE="+filepath.Join(stray, "tree"), "GIT_INDEX_FILE="+filepath.Join(stray, "index"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("TestImportCommand under a caller's git environment: %v\n%s", err, out)
	}
	if left, _ := os.ReadDir(stray); len(left) != 0 {
		t.Errorf("git wrote where the caller's environment pointed: %v", left)
	}
}

// runEnv runs study with exactly the environment in env, started in dir, and
// replaces $HOME in the output so golden files are stable.
func runEnv(t *testing.T, env map[string]string, dir string, stdin io.Reader, args ...string) result {
	t.Helper()
	var n atomic.Int64
	opts := core.Options{
		Getenv: func(key string) string { return env[key] },
		Dir:    dir,
		Now:    func() time.Time { return fixedNow },
		NewID:  func() string { return fmt.Sprintf("id%03d", n.Add(1)) },
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), args, stdin, &stdout, &stderr, opts)
	norm := func(s string) string {
		if home := env["HOME"]; home != "" {
			if resolved, err := filepath.EvalSymlinks(home); err == nil {
				s = strings.ReplaceAll(s, resolved, "$HOME")
			}
			s = strings.ReplaceAll(s, home, "$HOME")
		}
		return s
	}
	return result{code: code, stdout: norm(stdout.String()), stderr: norm(stderr.String())}
}

// fakeGit puts a git on PATH that reports version 2.47.1 and the given
// identity, so doctor's output does not depend on the machine.
func fakeGit(t *testing.T, name, email string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
--version) echo "git version 2.47.1" ;;
config)
  case "$3" in
  user.name) [ -n "$FAKE_GIT_NAME" ] || exit 1; echo "$FAKE_GIT_NAME" ;;
  user.email) [ -n "$FAKE_GIT_EMAIL" ] || exit 1; echo "$FAKE_GIT_EMAIL" ;;
  esac ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_GIT_NAME", name)
	t.Setenv("FAKE_GIT_EMAIL", email)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorHealthy(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "SHELL": "/usr/bin/fish"}
	for _, args := range [][]string{
		{"topic", "create", "--title", "Linear algebra"},
		{"completion", "install"},
	} {
		if r := runEnv(t, env, home, nil, args...); r.code != cli.ExitOK {
			t.Fatalf("setup %v: exit %d, %s", args, r.code, r.stderr)
		}
	}
	fakeGit(t, "Ada Lovelace", "ada@example.com")

	human := runEnv(t, env, home, nil, "doctor")
	if human.code != cli.ExitOK {
		t.Errorf("doctor: exit %d, stderr %s", human.code, human.stderr)
	}
	golden(t, "doctor_healthy.txt", human.stdout)

	got := runEnv(t, env, home, nil, "doctor", "--json")
	if got.code != cli.ExitOK || got.stderr != "" {
		t.Errorf("doctor --json: exit %d, stderr %q", got.code, got.stderr)
	}
	golden(t, "doctor_healthy.json", got.stdout)
}

// TestDoctorBrokenHome runs doctor where nothing works: config.toml is
// damaged, so the Study home cannot be found, and git is missing.
func TestDoctorBrokenHome(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "lamplight", "config.toml"), "study_home = [\n")
	t.Setenv("PATH", t.TempDir())
	env := map[string]string{"HOME": home}

	got := runEnv(t, env, home, nil, "doctor", "--json")
	if got.code != cli.ExitError || got.stderr != "" {
		t.Errorf("exit %d, stderr %q; want exit 1 and nothing on stderr", got.code, got.stderr)
	}
	golden(t, "doctor_broken_home.json", got.stdout)
}

// TestDoctorBrokenTopics runs doctor on a Study home with damaged files.
func TestDoctorBrokenTopics(t *testing.T) {
	home := t.TempDir()
	study := filepath.Join(home, "study")
	env := map[string]string{"HOME": home, "STUDY_HOME": study, "SHELL": "/bin/zsh", "STUDY_LOG": "verbose"}
	if r := runEnv(t, env, home, nil, "topic", "create", "--title", "Linear algebra"); r.code != cli.ExitOK {
		t.Fatalf("setup: %s", r.stderr)
	}
	writeFile(t, filepath.Join(study, "physics", "topic.toml"), "format = 99\ntitle = \"Physics\"\n")
	writeFile(t, filepath.Join(study, "c", "topic.toml"), "title = [broken\n")
	writeFile(t, filepath.Join(study, "go", "topic.toml"), "format = 1\ntitle = \"Go\"\n")
	writeFile(t, filepath.Join(study, ".lamplight", "state.toml"), "recent_topic = \n")
	writeFile(t, filepath.Join(study, ".lamplight", "library.json"), `{"format": 99, "books": []}`)
	fakeGit(t, "", "")

	got := runEnv(t, env, home, nil, "doctor", "--json")
	if got.code != cli.ExitError {
		t.Errorf("exit %d, want 1", got.code)
	}
	golden(t, "doctor_broken_topics.json", got.stdout)

	human := runEnv(t, env, home, nil, "doctor")
	if human.code != cli.ExitError {
		t.Errorf("exit %d, want 1", human.code)
	}
	golden(t, "doctor_broken_topics.txt", human.stdout)
	if strings.Contains(human.stderr, "Error") {
		t.Errorf("doctor printed an error after its report: %q", human.stderr)
	}
}

func TestCompletionInstallAndUninstall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    func(home string) map[string]string
		args   []string
		file   string // relative to HOME
		header string
	}{
		{"fish", func(h string) map[string]string { return map[string]string{"HOME": h, "SHELL": "/usr/bin/fish"} },
			nil, ".config/fish/completions/study.fish", "# fish completion for study"},
		{"bash with XDG_DATA_HOME", func(h string) map[string]string {
			return map[string]string{"HOME": h, "SHELL": "/bin/bash", "XDG_DATA_HOME": filepath.Join(h, "data")}
		}, nil, "data/bash-completion/completions/study", "# bash completion V2 for study"},
		{"zsh with an exported FPATH", func(h string) map[string]string {
			fpath := filepath.Join(h, ".zfunc")
			if err := os.MkdirAll(fpath, 0o755); err != nil {
				t.Fatal(err)
			}
			return map[string]string{"HOME": h, "SHELL": "/bin/zsh", "FPATH": "/nonexistent:" + fpath}
		}, nil, ".zfunc/_study", "#compdef study"},
		{"zsh with Oh My Zsh", func(h string) map[string]string {
			writeFile(t, filepath.Join(h, ".oh-my-zsh", "oh-my-zsh.sh"), "# omz\n")
			return map[string]string{"HOME": h, "SHELL": "/bin/zsh", "ZSH": filepath.Join(h, ".oh-my-zsh")}
		}, nil, ".oh-my-zsh/cache/completions/_study", "#compdef study"},
		{"zsh with --dir", func(h string) map[string]string { return map[string]string{"HOME": h} },
			[]string{"--shell", "zsh", "--dir", "~/funcs"}, "funcs/_study", "#compdef study"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			env := tc.env(home)
			path := filepath.Join(home, tc.file)

			dry := runEnv(t, env, home, nil, append([]string{"completion", "install", "--dry-run", "--json"}, tc.args...)...)
			if dry.code != cli.ExitOK || !strings.Contains(dry.stdout, `"dry_run": true`) {
				t.Fatalf("dry run: exit %d, %s %s", dry.code, dry.stdout, dry.stderr)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("--dry-run wrote %s", path)
			}

			install := runEnv(t, env, home, nil, append([]string{"completion", "install"}, tc.args...)...)
			if install.code != cli.ExitOK {
				t.Fatalf("install: exit %d, %s", install.code, install.stderr)
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(data), tc.header) {
				t.Fatalf("installed script at %s: %v, starts %q", path, err, firstLine(string(data)))
			}
			if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
				t.Errorf("install touched .zshrc when it did not need to")
			}

			shell := []string{}
			if len(tc.args) > 0 {
				shell = tc.args[:2]
			}
			uninstall := runEnv(t, env, home, nil, append([]string{"completion", "uninstall", "--json"}, shell...)...)
			if uninstall.code != cli.ExitOK {
				t.Fatalf("uninstall: exit %d, %s", uninstall.code, uninstall.stdout)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("uninstall left %s", path)
			}
			if _, err := os.Stat(filepath.Join(home, ".local", "state", "lamplight", "completions.json")); !os.IsNotExist(err) {
				t.Errorf("uninstall left the completion record behind")
			}
		})
	}
}

// TestZshCompletionNeedsConsent covers zsh without a writable folder on
// $fpath: study adds one line to .zshrc only with --yes, and uninstall
// restores .zshrc byte for byte.
func TestZshCompletionNeedsConsent(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "SHELL": "/bin/zsh"}
	zshrc := filepath.Join(home, ".zshrc")
	original := "autoload -Uz compinit && compinit\nexport EDITOR=vi" // no final newline
	writeFile(t, zshrc, original)
	script := filepath.Join(home, ".local", "share", "lamplight", "completions", "_study")

	refused := runEnv(t, env, home, nil, "completion", "install", "--json")
	if refused.code != cli.ExitUsage || !strings.Contains(refused.stdout, "--yes") {
		t.Fatalf("install without consent: exit %d, %s", refused.code, refused.stdout)
	}
	if data, _ := os.ReadFile(zshrc); string(data) != original {
		t.Fatalf("install without consent changed .zshrc:\n%s", data)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatalf("install without consent wrote %s", script)
	}

	for range 2 {
		if r := runEnv(t, env, home, nil, "completion", "install", "--yes"); r.code != cli.ExitOK {
			t.Fatalf("install --yes: exit %d, %s", r.code, r.stderr)
		}
	}
	data, _ := os.ReadFile(zshrc)
	if n := strings.Count(string(data), "&& source "); n != 1 {
		t.Fatalf(".zshrc has %d study lines after two installs:\n%s", n, data)
	}
	if !strings.Contains(string(data), "source '"+script+"'") {
		t.Errorf(".zshrc does not source the script:\n%s", data)
	}
	if _, err := os.Stat(script); err != nil {
		t.Errorf("script not installed: %v", err)
	}

	if r := runEnv(t, env, home, nil, "completion", "uninstall", "--dry-run"); r.code != cli.ExitOK ||
		!strings.Contains(r.stdout, "Would remove") {
		t.Fatalf("uninstall --dry-run: exit %d, %q", r.code, r.stdout)
	}
	if after, _ := os.ReadFile(zshrc); string(after) != string(data) {
		t.Fatal("uninstall --dry-run changed .zshrc")
	}
	if r := runEnv(t, env, home, nil, "completion", "uninstall"); r.code != cli.ExitOK {
		t.Fatalf("uninstall: exit %d, %s", r.code, r.stderr)
	}
	if after, _ := os.ReadFile(zshrc); string(after) != original {
		t.Errorf("uninstall did not restore .zshrc:\n%q\nwant\n%q", after, original)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Errorf("uninstall left %s", script)
	}
}

func TestCompletionShellErrors(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		env  map[string]string
		args []string
	}{
		{map[string]string{"HOME": home, "SHELL": "/bin/tcsh"}, []string{"completion", "install", "--json"}},
		{map[string]string{"HOME": home}, []string{"completion", "uninstall", "--shell", "tcsh", "--json"}},
		{map[string]string{"HOME": home}, []string{"completion", "install", "--shell", "powershell", "--json"}},
		{map[string]string{"HOME": home}, []string{"completion", "install", "--shell", "fish", "--dir", home, "--json"}},
	} {
		got := runEnv(t, tc.env, home, nil, tc.args...)
		if got.code != cli.ExitUsage || !strings.Contains(got.stdout, `"code": "usage"`) {
			t.Errorf("%v: exit %d, %s", tc.args, got.code, got.stdout)
		}
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("failed installs wrote %d entries into HOME", len(entries))
	}
}

func TestCompletionScriptsStillPrint(t *testing.T) {
	home := t.TempDir()
	got := runEnv(t, map[string]string{"HOME": home}, home, nil, "completion", "bash")
	if got.code != cli.ExitOK || !strings.HasPrefix(got.stdout, "# bash completion V2 for study") {
		t.Errorf("study completion bash: exit %d, starts %q", got.code, firstLine(got.stdout))
	}
}

var (
	anyEscape   = regexp.MustCompile("\x1b\\[")
	colorEscape = regexp.MustCompile("\x1b\\[[0-9;]*(3[0-9]|9[0-7]|38;)")
)

func TestStyledOutput(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"STUDY_HOME": home, "HOME": home}
	if r := runEnv(t, env, home, nil, "topic", "create", "--title", "Linear algebra"); r.code != cli.ExitOK {
		t.Fatal(r.stderr)
	}
	piped := runEnv(t, env, home, nil, "status")
	if anyEscape.MatchString(piped.stdout) {
		t.Errorf("piped output has escape sequences: %q", piped.stdout)
	}

	terminal := map[string]string{"STUDY_HOME": home, "HOME": home, "TTY_FORCE": "1", "TERM": "xterm-256color"}
	styled := runEnv(t, terminal, home, nil, "status")
	if !colorEscape.MatchString(styled.stdout) {
		t.Errorf("terminal output has no colour: %q", styled.stdout)
	}
	if plain := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(styled.stdout, ""); plain != piped.stdout {
		t.Errorf("styled output differs from plain output beyond styles:\n%s\nwant\n%s", plain, piped.stdout)
	}

	terminal["NO_COLOR"] = "1"
	noColor := runEnv(t, terminal, home, nil, "status")
	if colorEscape.MatchString(noColor.stdout) {
		t.Errorf("NO_COLOR output has colour: %q", noColor.stdout)
	}
}

func TestLogging(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "state")
	logFile := filepath.Join(state, "lamplight", "study.log")
	env := map[string]string{"STUDY_HOME": filepath.Join(home, "study"), "HOME": home, "XDG_STATE_HOME": state}

	quiet := runEnv(t, env, home, nil, "status", "--json")
	if quiet.stderr != "" {
		t.Errorf("status wrote to stderr: %q", quiet.stderr)
	}
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Errorf("a run that logged nothing created %s", logFile)
	}

	created := runEnv(t, env, home, nil, "topic", "create", "--title", "C", "--json")
	if created.stderr != "" {
		t.Errorf("by default stderr only shows warnings, got %q", created.stderr)
	}
	records := readLog(t, logFile)
	if len(records) != 1 || records[0]["msg"] != "topic created" || records[0]["level"] != "INFO" {
		t.Errorf("log records = %v, want one info record", records)
	}

	env["STUDY_LOG"] = "debug"
	debug := runEnv(t, env, home, nil, "topic", "create", "--title", "Go", "--json")
	if !strings.Contains(debug.stderr, "topic created") || !strings.Contains(debug.stderr, "running") {
		t.Errorf("STUDY_LOG=debug should show debug records on stderr, got %q", debug.stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(debug.stdout), &envelope); err != nil || envelope["ok"] != true {
		t.Errorf("logging corrupted --json stdout: %v %q", err, debug.stdout)
	}

	bad := runEnv(t, env, home, nil, "status", "--log-level", "loud", "--json")
	if bad.code != cli.ExitUsage || !strings.Contains(bad.stdout, "--log-level") {
		t.Errorf("--log-level loud: exit %d, %s", bad.code, bad.stdout)
	}
}

// TestMCPStdoutIsOnlyJSONRPC runs study mcp at debug level and checks that
// every byte on stdout is a JSON-RPC message, with the Log in the file.
func TestMCPStdoutIsOnlyJSONRPC(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	state := filepath.Join(home, "state")
	env := map[string]string{"STUDY_HOME": filepath.Join(home, "study"), "HOME": home, "XDG_STATE_HOME": state}

	clientToServer, serverIn := io.Pipe()
	serverOut, serverToClient := io.Pipe()
	var captured, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		var n atomic.Int64
		opts := core.Options{
			Getenv: func(key string) string { return env[key] },
			Dir:    home,
			Now:    func() time.Time { return fixedNow },
			NewID:  func() string { return fmt.Sprintf("id%03d", n.Add(1)) },
		}
		done <- cli.Run(ctx, []string{"mcp", "--log-level", "debug"}, clientToServer,
			io.MultiWriter(serverToClient, &captured), &stderr, opts)
		_ = serverToClient.Close()
	}()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil).
		Connect(ctx, &mcp.IOTransport{Reader: serverOut, Writer: serverIn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []mcp.CallToolParams{
		{Name: "topic_create", Arguments: map[string]any{"title": "Physics"}},
		{Name: "status", Arguments: map[string]any{}},
		{Name: "topic_create", Arguments: map[string]any{"title": "Physics"}}, // a tool error
	} {
		if _, err := session.CallTool(ctx, &call); err != nil {
			t.Fatalf("%s: %v", call.Name, err)
		}
	}
	_ = session.Close()
	if code := <-done; code != cli.ExitOK {
		t.Fatalf("study mcp exited with %d", code)
	}

	scanner := bufio.NewScanner(&captured)
	scanner.Buffer(nil, 1<<20)
	lines := 0
	for scanner.Scan() {
		var msg struct {
			JSONRPC string `json:"jsonrpc"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.JSONRPC != "2.0" {
			t.Errorf("stdout line is not JSON-RPC: %q", scanner.Text())
		}
		lines++
	}
	if lines == 0 {
		t.Error("captured no JSON-RPC messages")
	}
	if stderr.Len() != 0 {
		t.Errorf("study mcp wrote below warn level to stderr: %q", stderr.String())
	}
	debug := 0
	for _, r := range readLog(t, filepath.Join(state, "lamplight", "study.log")) {
		if r["level"] == "DEBUG" {
			debug++
		}
	}
	if debug == 0 {
		t.Error("no debug records reached the log file")
	}
}

func readLog(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		records = append(records, r)
	}
	return records
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
