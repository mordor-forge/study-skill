package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mordor-forge/lamplight/v2/internal/cli"
	"github.com/mordor-forge/lamplight/v2/internal/core"
)

// The v2.0 acceptance walkthrough: from study setup to a finished Lesson, a
// break and a resume, the Milestone's Assessment, and an imported v1
// workspace. It is the automated half of issue #36's "from install to a
// finished Lesson and back after a break"; the manual runs in Claude Code and
// Codex stay the maintainer's.
//
// Everything happens in one temporary HOME. study's own PATH holds only fake
// claude and codex commands and links to the few tools the run needs; the
// process's PATH holds failing stand-ins for claude and codex, so nothing can
// reach the real agents.

// journeyClaude keeps user-scope MCP servers in $HOME/.claude.json, as Claude
// Code does, and logs its calls. It never runs the real CLI.
const journeyClaude = `#!/bin/sh
echo "claude $*" >> "$HOME/agent-calls.log"
case "$1 $2" in
"mcp add")
  shift 2
  [ "$1" = "--scope" ] && shift 2
  name=$1; shift
  [ "$1" = "--" ] && shift
  cmd=$1; shift
  args=""
  for a in "$@"; do args="$args${args:+,}\"$a\""; done
  printf '{"mcpServers":{"%s":{"type":"stdio","command":"%s","args":[%s]}}}\n' "$name" "$cmd" "$args" > "$HOME/.claude.json" ;;
"mcp remove") printf '{"mcpServers":{}}\n' > "$HOME/.claude.json" ;;
*) exit 2 ;;
esac
`

// journeyCodex keeps one server in a file and answers mcp get --json as
// Codex does, and logs its calls.
const journeyCodex = `#!/bin/sh
echo "codex $*" >> "$HOME/agent-calls.log"
reg="$HOME/.codex-fake.json"
case "$1 $2" in
"mcp add")
  shift 2
  name=$1; shift
  [ "$1" = "--" ] && shift
  cmd=$1; shift
  args=""
  for a in "$@"; do args="$args${args:+,}\"$a\""; done
  printf '{"name":"%s","enabled":true,"transport":{"type":"stdio","command":"%s","args":[%s]}}\n' "$name" "$cmd" "$args" > "$reg" ;;
"mcp remove") rm -f "$reg" ;;
"mcp get")
  if [ -f "$reg" ]; then cat "$reg"; else echo "Error: No MCP server named '$3' found." >&2; exit 1; fi ;;
*) exit 2 ;;
esac
`

// forbiddenJourneyAgent stands in for claude and codex on the process's PATH:
// anything that looks an agent up there runs this, which logs and fails.
const forbiddenJourneyAgent = `#!/bin/sh
echo "$0 $*" >> '%s'
exit 97
`

// journeyTools are the only programs from the machine the walkthrough uses,
// each linked into its own folder: never claude or codex.
var journeyTools = []string{"sh", "cat", "rm", "git"}

// journeyLesson has a run criterion, a rubric item and a Break point.
const journeyLesson = `---
check:
  - id: answer
    describe: answer.txt holds the answer to everything
    run: [sh, check.sh]
  - id: explain
    rubric: explain.md says why the answer is what it is
break_points:
  - id: first-try
    describe: The first answer is written and checked
---
# The answer

Write the answer to everything in answer.txt, and why in explain.md.
`

type journey struct {
	t         *testing.T
	home      string // HOME
	studyHome string
	studyPATH string // study's own PATH: fake agents and the tools
	forbidden string // log of calls to the stand-ins on the process's PATH
	studyBin  string // study, built for the run
	clock     *clock
}

func newJourney(t *testing.T) *journey {
	t.Helper()
	startPATH := os.Getenv("PATH")
	// Built before HOME changes, so the go command finds its build cache.
	studyBin := buildStudy(t)
	home := t.TempDir()
	j := &journey{t: t, home: home, studyHome: filepath.Join(home, "study"), forbidden: filepath.Join(home, "forbidden-calls.log"),
		studyBin: studyBin, clock: &clock{at: time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)}}
	bin, tools, absent := filepath.Join(home, "bin"), filepath.Join(home, "tools"), filepath.Join(home, "absent")
	writeExecutable(t, filepath.Join(bin, "claude"), journeyClaude)
	writeExecutable(t, filepath.Join(bin, "codex"), journeyCodex)
	for _, agent := range []string{"claude", "codex"} {
		writeExecutable(t, filepath.Join(absent, agent), fmt.Sprintf(forbiddenJourneyAgent, j.forbidden))
	}
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range journeyTools {
		real, err := lookOn(startPATH, name)
		if err != nil {
			// Never skipped: a green run must mean the walkthrough ran.
			t.Fatalf("the walkthrough needs %s: %v", name, err)
		}
		if err := os.Symlink(real, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	j.studyPATH = bin + string(os.PathListSeparator) + tools

	// The process: git, run by the core, finds the tools and the failing
	// stand-ins only. HOME holds the learner's git identity, with git's
	// background maintenance off so nothing writes to .git after a test.
	t.Setenv("PATH", absent+string(os.PathListSeparator)+tools)
	t.Setenv("HOME", home)
	// git run by the test itself reads the identity below whatever the
	// parent's GIT_CONFIG_GLOBAL; the core strips GIT_* on its own.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for key, rel := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_STATE_HOME": ".local/state",
		"XDG_CACHE_HOME": ".cache", "XDG_DATA_HOME": ".local/share"} {
		t.Setenv(key, filepath.Join(home, rel))
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("STUDY_LOG", "")
	gitconfig := "[user]\n\tname = Ada Learner\n\temail = ada@example.com\n[maintenance]\n\tauto = false\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if data, err := os.ReadFile(j.forbidden); err == nil {
			t.Errorf("an agent was looked up outside study's PATH:\n%s", data)
		}
	})
	j.guard()
	return j
}

// guard fails the test unless claude and codex can only resolve to the fakes
// or the failing stand-ins, all inside the test's HOME.
func (j *journey) guard() {
	j.t.Helper()
	for _, path := range []string{j.studyPATH, os.Getenv("PATH")} {
		for _, dir := range filepath.SplitList(path) {
			if rel, err := filepath.Rel(j.home, dir); err != nil || strings.HasPrefix(rel, "..") {
				j.t.Fatalf("PATH holds %s, outside the test's HOME", dir)
			}
		}
	}
	for _, agent := range []string{"claude", "codex"} {
		if p, err := exec.LookPath(agent); err == nil && !strings.HasPrefix(p, j.home+string(filepath.Separator)) {
			j.t.Fatalf("%s resolves to %s, outside the test's HOME", agent, p)
		}
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func lookOn(path, name string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s is not on PATH", name)
}

// buildStudy builds ./cmd/study into a temporary folder, so study setup
// registers a real study and the MCP client can start exactly what it
// registered. It builds with -race when the tests run with it, to reuse the
// packages the test build compiled, and without a version stamp: the build
// must not need git to report on the checkout, which it refuses to in one
// owned by another user.
func buildStudy(t *testing.T) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the walkthrough builds study with the go command: %v", err)
	}
	out := filepath.Join(t.TempDir(), "study")
	args := []string{"build", "-buildvcs=false", "-o", out}
	if raceEnabled {
		args = append(args, "-race")
	}
	cmd := exec.Command(goTool, append(args, "./cmd/study")...)
	cmd.Dir = filepath.Join("..", "..")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building study: %v\n%s", err, b)
	}
	return out
}

// env is the environment study runs in when an agent or the learner's
// terminal starts it: the test's HOME and folders, and study's own PATH.
func (j *journey) env() []string {
	env := []string{"HOME=" + j.home, "PATH=" + j.studyPATH, "STUDY_HOME=" + j.studyHome, "TMPDIR=" + os.TempDir()}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		env = append(env, key+"="+os.Getenv(key))
	}
	return env
}

// study runs the built study, as the learner does in a terminal, and
// decodes the JSON envelope's data into out.
func (j *journey) study(out any, args ...string) {
	j.t.Helper()
	j.guard()
	cmd := exec.Command(j.studyBin, append(args, "--json")...)
	cmd.Env, cmd.Dir = j.env(), j.home
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	j.decode(out, args, err == nil, stdout.Bytes(), stderr.String())
}

func (j *journey) decode(out any, args []string, ran bool, stdout []byte, stderr string) {
	j.t.Helper()
	var env struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(stdout, &env); err != nil || !ran || !env.OK {
		j.t.Fatalf("study %s failed:\n%s%s", strings.Join(args, " "), stdout, stderr)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			j.t.Fatalf("study %s: %s: %v", strings.Join(args, " "), env.Data, err)
		}
	}
}

// registration is how an agent starts an MCP server.
type registration struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// registrations reads what study setup registered with the fake Claude Code
// and the fake Codex.
func (j *journey) registrations() (claude, codex registration) {
	j.t.Helper()
	var claudeConfig struct {
		Servers map[string]registration `json:"mcpServers"`
	}
	var codexConfig struct {
		Name      string       `json:"name"`
		Transport registration `json:"transport"`
	}
	for file, into := range map[string]any{".claude.json": &claudeConfig, ".codex-fake.json": &codexConfig} {
		data, err := os.ReadFile(filepath.Join(j.home, file))
		if err != nil {
			j.t.Fatal(err)
		}
		if err := json.Unmarshal(data, into); err != nil {
			j.t.Fatalf("%s: %v\n%s", file, err, data)
		}
	}
	if codexConfig.Name != "lamplight" {
		j.t.Fatalf("Codex's server is named %q", codexConfig.Name)
	}
	return claudeConfig.Servers["lamplight"], codexConfig.Transport
}

// statusThrough starts the MCP server as the agent would, from its
// registration, and asks it for status.
func (j *journey) statusThrough(reg registration) core.Status {
	j.t.Helper()
	ctx := context.Background()
	cmd := exec.Command(reg.Command, reg.Args...)
	cmd.Env, cmd.Dir = j.env(), j.home
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	session, err := mcp.NewClient(&mcp.Implementation{Name: "registered-agent", Version: "1"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		j.t.Fatalf("starting %s %v: %v\n%s", reg.Command, reg.Args, err, stderr.String())
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "status", Arguments: map[string]any{}})
	if err != nil {
		j.t.Fatalf("status through %s: %v\n%s", reg.Command, err, stderr.String())
	}
	if res.IsError {
		j.t.Fatalf("status through %s: %s\n%s", reg.Command, text(res), stderr.String())
	}
	if err := session.Close(); err != nil {
		j.t.Fatalf("stopping the MCP server: %v\n%s", err, stderr.String())
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		j.t.Fatal(err)
	}
	var status core.Status
	if err := json.Unmarshal(data, &status); err != nil {
		j.t.Fatalf("status: %s: %v", data, err)
	}
	return status
}

// cli runs the study command line in this process, as the learner does in a
// terminal, with study's own PATH and the walkthrough's clock, and decodes
// the JSON envelope's data into out.
func (j *journey) cli(out any, args ...string) {
	j.t.Helper()
	j.guard()
	opts := core.Options{
		Getenv: func(key string) string {
			switch key {
			case "PATH":
				return j.studyPATH
			case "STUDY_HOME":
				return j.studyHome
			}
			return os.Getenv(key)
		},
		Dir: j.home,
		Now: j.clock.now,
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), append(args, "--json"), strings.NewReader(""), &stdout, &stderr, opts)
	j.decode(out, args, code == cli.ExitOK, stdout.Bytes(), stderr.String())
}

// v1Workspace builds a small workspace of the v1 study skill, as v1's
// lifecycle smoke test does: Lesson 1 done and proven by its commit, Lesson 2
// open.
func (j *journey) v1Workspace() string {
	j.t.Helper()
	dir := filepath.Join(j.t.TempDir(), "go-concurrency")
	cfg := map[string]any{
		"version": 3, "topic": "Go Concurrency", "approach": "concept", "end_goal": "Write a concurrent web crawler",
		"difficulty": "beginner",
		"lessons": []any{
			map[string]any{"num": 1, "title": "Goroutines", "file": "lessons/01-goroutines.md", "status": "completed"},
			map[string]any{"num": 2, "title": "Channels", "file": "lessons/02-channels.md", "status": "in_progress"},
		},
		"session_state": map[string]any{"phase": "practicing", "pending_action": "review the channels exercise",
			"context": "The learner was writing a pipeline."},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		j.t.Fatal(err)
	}
	for rel, content := range map[string]string{
		".study-config.json":         string(data) + "\n",
		"lessons/01-goroutines.md":   "# Lesson 1: Goroutines\n",
		"lessons/02-channels.md":     "# Lesson 2: Channels\n",
		"practice/lesson-01/main.go": "package main\n",
		".fsrs/cards.json":           `{"cards":[{"id":"lesson-01","title":"Goroutines"}]}` + "\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			j.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			j.t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"commit", "-q", "-m", "[agent] init study workspace"},
		{"commit", "-q", "--allow-empty", "-m", "[agent] complete lesson 01"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			j.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

func TestTheV2Journey(t *testing.T) {
	j := newJourney(t)

	// 1. study setup, run from the built binary, installs the skill and
	// registers that binary's study mcp with both agents. The binary lives in
	// a temporary folder, which setup refuses without --force.
	j.study(nil, "setup", "--force")
	skill := filepath.Join(j.home, ".agents", "skills", "lamplight")
	if _, err := os.Stat(filepath.Join(skill, "SKILL.md")); err != nil {
		t.Fatalf("the skill was not installed: %v", err)
	}
	link, err := filepath.EvalSymlinks(filepath.Join(j.home, ".claude", "skills", "lamplight"))
	if want, _ := filepath.EvalSymlinks(skill); err != nil || link != want {
		t.Fatalf("Claude Code's skill link = %q, %v; want %s", link, err, want)
	}
	// Setup registers its own path with links resolved.
	study, err := filepath.EvalSymlinks(j.studyBin)
	if err != nil {
		t.Fatal(err)
	}
	want := registration{Type: "stdio", Command: study, Args: []string{"mcp"}}
	claudeReg, codexReg := j.registrations()
	for agent, reg := range map[string]registration{"Claude Code": claudeReg, "Codex": codexReg} {
		if reg.Type != want.Type || reg.Command != want.Command || !slices.Equal(reg.Args, want.Args) {
			t.Fatalf("%s's registration = %+v, want %+v", agent, reg, want)
		}
	}
	j.study(nil, "setup", "--check")

	a := &agent{t: t, home: j.studyHome, clock: j.clock}
	a.connect()

	// 2. status on an empty Study home.
	var status core.Status
	a.call("status", map[string]any{}, &status)
	if len(status.Topics) != 0 {
		t.Fatalf("a new Study home has Topics: %+v", status.Topics)
	}

	// 3. A Topic, and a Session on it; the learner picks the Focus, recorded
	// on the open Session.
	a.call("topic_create", map[string]any{"title": "C", "id": "c", "goal": "Write small C programs"}, nil)
	var opened core.SessionOpened
	a.call("session_open", map[string]any{"topic": "c", "energy": "full"}, &opened)
	if opened.Suggested == nil || opened.Suggested.Suggest != core.SuggestPlan {
		t.Fatalf("the first Session suggests %+v, want plan", opened.Suggested)
	}
	first := opened.Session
	a.call("session_open", map[string]any{"topic": "c", "session": first, "focus": "learn"}, &opened)
	if opened.Session != first || opened.Focus != "learn" {
		t.Fatalf("recording the Focus = %+v, want it on Session %s", opened, first)
	}

	// 4. The Syllabus, approved by the learner in chat.
	var proposal core.RevisionProposal
	a.call("revision_propose", map[string]any{"topic": "c", "summary": "One Milestone with one Lesson to start",
		"syllabus": map[string]any{"milestones": []any{map[string]any{
			"id": "basics", "title": "Basics", "outcome": "Answer questions with a program", "priority": "must",
			"lessons": []any{map[string]any{"id": "answer", "title": "The answer", "hours": 1}},
		}}}}, &proposal)
	a.call("revision_apply", map[string]any{"topic": "c", "revision": proposal.Revision, "learner_said": "Looks good, let's go"}, nil)

	// 5. Teaching: the Lesson file with its Check.
	a.call("phase_set", map[string]any{"topic": "c", "lesson": "answer", "phase": "teaching"}, nil)
	a.write("lessons/answer.md", journeyLesson)
	a.write("practice/answer/check.sh", checkScript)
	a.call("phase_set", map[string]any{"topic": "c", "lesson": "answer", "phase": "practicing"}, nil)

	// 6. The learner's first answer fails its Check.
	a.write("practice/answer/answer.txt", "41\n")
	a.write("practice/answer/explain.md", "It is the answer to everything.\n")
	a.call("phase_set", map[string]any{"topic": "c", "lesson": "answer", "phase": "feedback"}, nil)
	if attempt := a.shellCheck("answer"); attempt.Outcome != core.OutcomeFailed {
		t.Fatalf("first Attempt = %+v", attempt)
	}

	// 9. A Break point, and the learner stops for the day. Stopping saves
	// the work: only the History's record of that Checkpoint is left.
	var reached core.BreakPointReached
	a.call("break_point_reached", map[string]any{"topic": "c", "lesson": "answer", "break_point": "first-try",
		"next_step": "Fix the answer in answer.txt"}, &reached)
	var closed core.SessionClosed
	a.call("session_close", map[string]any{"topic": "c", "next_step": "Fix the answer in answer.txt",
		"context": "The Check said answer.txt holds 41."}, &closed)
	if dirty := a.git("status", "--porcelain"); dirty != "M history.jsonl" {
		t.Fatalf("after stopping, git status = %q, want only the History's record of the Checkpoint", dirty)
	}
	if !strings.Contains(a.git("show", "HEAD:practice/answer/answer.txt"), "41") {
		t.Fatalf("the stop did not save the learner's work")
	}
	a.disconnect()

	// 10. The next day, a new conversation resumes from the Next step.
	j.clock.advance(24 * time.Hour)
	a.connect()
	a.call("status", map[string]any{}, &status)
	resume := status.Topics[0].Resume
	if resume == nil || resume.Lesson != "answer" || resume.BreakPoint == nil || resume.BreakPoint.ID != "first-try" ||
		resume.NextStep == nil || resume.NextStep.Step != "Fix the answer in answer.txt" {
		t.Fatalf("resume = %+v", resume)
	}
	if status.Recommended == nil || status.Recommended.Action != core.ActionNextStep ||
		status.Recommended.Text != "Fix the answer in answer.txt" {
		t.Fatalf("recommended = %+v, want the Next step word for word", status.Recommended)
	}
	a.call("session_open", map[string]any{"topic": "c", "energy": "half", "focus": "practice"}, &opened)
	if len(opened.Unclosed) != 0 || opened.Resume.NextStep == nil {
		t.Fatalf("second Session = %+v", opened)
	}

	// 6, continued. The fix, and a passing Check.
	a.call("phase_set", map[string]any{"topic": "c", "lesson": "answer", "phase": "practicing"}, nil)
	a.write("practice/answer/answer.txt", "42\n")
	a.call("phase_set", map[string]any{"topic": "c", "lesson": "answer", "phase": "feedback"}, nil)
	passed := a.shellCheck("answer")
	if passed.Outcome != core.OutcomePassed {
		t.Fatalf("second Attempt = %+v", passed)
	}

	// 7. The rubric item, graded on the work it looked at.
	a.call("rubric_record", map[string]any{"topic": "c", "lesson": "answer", "criterion": "explain", "grade": "met",
		"note": "Says why in one line", "looked_at": []any{"explain.md"}}, nil)
	var results core.CheckResults
	a.call("check_results", map[string]any{"topic": "c", "lesson": "answer"}, &results)
	if !results.CanComplete {
		t.Fatalf("check results = %+v", results)
	}

	// 8. Completion with a draft Card, which finishes the Milestone, then the
	// Card's first Review.
	var done core.LessonCompletion
	a.call("lesson_complete", map[string]any{"topic": "c", "lesson": "answer",
		"cards": []any{map[string]any{"prompt": "What does answer.txt hold when the Check passes?", "answer": "42"}}}, &done)
	if !done.Changed || done.Attempt != passed.ID || len(done.Cards) != 1 || done.Next == nil || done.Next.Code != "assess_milestone" {
		t.Fatalf("completion = %+v", done)
	}
	var reviewed core.ReviewResult
	a.call("review_record", map[string]any{"topic": "c", "card": done.Cards[0].ID, "rating": "good", "draft": "keep",
		"request": "review-1"}, &reviewed)
	if reviewed.Card.Draft {
		t.Fatalf("review = %+v", reviewed)
	}

	// 11. The Milestone's Assessment is the recommended action until it is
	// recorded.
	a.call("status", map[string]any{}, &status)
	if status.Recommended == nil || status.Recommended.Action != core.ActionAssess || status.Recommended.Milestone == nil ||
		status.Recommended.Milestone.ID != "basics" {
		t.Fatalf("recommended after the Milestone = %+v, want assess basics", status.Recommended)
	}
	a.call("assessment_record", map[string]any{"topic": "c", "kind": "milestone", "milestone": "basics",
		"items":   []any{map[string]any{"area": "files", "question": "Read a number from a file", "outcome": "correct"}},
		"summary": "Reads and checks files confidently", "level": "beginner", "request": "basics-end"}, nil)
	a.call("status", map[string]any{}, &status)
	if status.Recommended != nil && status.Recommended.Action == core.ActionAssess {
		t.Fatalf("the Assessment is still recommended after it was recorded: %+v", status.Recommended)
	}
	a.call("session_close", map[string]any{"topic": "c", "next_step": "Plan the next Milestone with the learner"}, nil)

	// The History holds the story in order, and the replayed state agrees.
	types := historyTypes(t, filepath.Join(j.studyHome, "c", "history.jsonl"))
	for _, want := range [][]string{
		{"topic.created", "session.opened", "session.focused", "revision.proposed", "revision.applied"},
		{"attempt.recorded", "break_point.reached", "session.closed"},
		{"session.opened", "attempt.recorded", "rubric.graded", "lesson.completed", "review.recorded",
			"assessment.recorded", "session.closed"},
	} {
		if !inOrder(types, want) {
			t.Errorf("the History lacks %v in order:\n%v", want, types)
		}
	}
	var syllabus core.SyllabusView
	a.call("syllabus", map[string]any{"topic": "c"}, &syllabus)
	if syllabus.Milestones[0].Lessons[0].Status != core.LessonDone {
		t.Errorf("Syllabus = %+v", syllabus)
	}
	a.call("status", map[string]any{}, &status)
	if topic := status.Topics[0]; len(topic.Flags) != 0 || topic.Level == nil || topic.Level.Level != "beginner" {
		t.Errorf("final Topic = %+v", topic)
	}
	a.disconnect()

	// 12. A v1 workspace comes over: a dry run first, then the import, then
	// the adoption through an approved Revision.
	src := j.v1Workspace()
	var dry core.TopicImport
	j.cli(&dry, "import", src, "--dry-run")
	if _, err := os.Stat(filepath.Join(j.studyHome, "go-concurrency")); !os.IsNotExist(err) {
		t.Fatalf("the dry run wrote the Topic (err = %v)", err)
	}
	var imported core.TopicImport
	j.cli(&imported, "import", src)
	if imported.Topic.ID != "go-concurrency" || len(imported.Completed) != 1 || imported.Completed[0].Lesson != "lesson-01" ||
		len(imported.Open) != 1 || imported.Open[0].Lesson != "lesson-02" {
		t.Fatalf("import = %+v", imported)
	}
	if !slices.EqualFunc(dry.Completed, imported.Completed, func(x, y core.ImportedLesson) bool { return x.Lesson == y.Lesson }) {
		t.Errorf("the dry run reported %+v, the import %+v", dry.Completed, imported.Completed)
	}
	a.connect()
	a.call("session_open", map[string]any{"topic": "go-concurrency", "energy": "full"}, &opened)
	if opened.Suggested == nil || opened.Suggested.Suggest != core.SuggestAdopt {
		t.Fatalf("an imported Topic's first Session suggests %+v, want adopt", opened.Suggested)
	}
	a.call("revision_propose", map[string]any{"topic": "go-concurrency", "summary": "The v1 plan as a Syllabus",
		"syllabus": map[string]any{"milestones": []any{map[string]any{
			"id": "core", "title": "Core", "outcome": "Use goroutines and channels", "priority": "must",
			"lessons": []any{
				map[string]any{"id": "lesson-01", "title": "Goroutines", "hours": 2},
				map[string]any{"id": "lesson-02", "title": "Channels", "hours": 2},
			},
		}}}}, &proposal)
	a.call("revision_apply", map[string]any{"topic": "go-concurrency", "revision": proposal.Revision,
		"learner_said": "Yes, that's my plan"}, nil)
	a.call("status", map[string]any{}, &status)
	var adopted *core.Topic
	for i := range status.Topics {
		if status.Topics[i].ID == "go-concurrency" {
			adopted = &status.Topics[i]
		}
	}
	if adopted == nil || adopted.Imported == nil || !adopted.Imported.Adopted || adopted.Resume == nil ||
		adopted.Resume.Lesson != "lesson-02" {
		t.Errorf("the adopted Topic = %+v", adopted)
	}
	a.disconnect()

	// Each agent, starting the MCP server exactly as setup registered it,
	// sees both Topics. Setup still holds, and no real agent was reached.
	for agent, reg := range map[string]registration{"Claude Code": claudeReg, "Codex": codexReg} {
		var ids []string
		for _, topic := range j.statusThrough(reg).Topics {
			ids = append(ids, topic.ID)
		}
		if slices.Sort(ids); !slices.Equal(ids, []string{"c", "go-concurrency"}) {
			t.Errorf("status through %s's registration lists %v", agent, ids)
		}
	}
	j.study(nil, "setup", "--check")
}

// inOrder reports whether want appears in types as a subsequence.
func inOrder(types, want []string) bool {
	i := 0
	for _, t := range types {
		if i < len(want) && t == want[i] {
			i++
		}
	}
	return i == len(want)
}
