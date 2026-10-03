package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/core"
)

// TestSIGTERMStopsACheck sends SIGTERM to a real study check, as a harness
// does when the agent's turn is cancelled: study must stop the whole Check
// promptly, record nothing, and report the canceled code.
func TestSIGTERMStopsACheck(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the study binary")
	}
	ctx := context.Background()
	// Built before HOME moves, so the build uses the usual module cache, and
	// without a version stamp (see buildStudy).
	bin := filepath.Join(t.TempDir(), "study")
	if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/mordor-forge/lamplight/v2/cmd/study").CombinedOutput(); err != nil {
		t.Fatalf("building study: %v\n%s", err, out)
	}
	withGitIdentity(t)
	a := &agent{t: t, home: t.TempDir(), clock: &clock{at: time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)}}
	c, err := core.Open(a.options())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateTopic(ctx, core.TopicSpec{Title: "C", ID: "c"}); err != nil {
		t.Fatal(err)
	}
	p, err := c.ProposeRevision(ctx, "c", core.RevisionSpec{Summary: "s", Syllabus: core.Syllabus{Milestones: []core.Milestone{
		{ID: "basics", Title: "Basics", Lessons: []core.SyllabusLesson{{ID: "answer", Title: "The answer"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyRevision(ctx, "c", p.Revision, core.Approval{Via: "chat", LearnerSaid: "yes"}, false); err != nil {
		t.Fatal(err)
	}
	a.write("lessons/answer.md", lesson)
	a.write("practice/answer/check.sh", `echo $$ > "$PGID_FILE"; sleep 30 & sleep 30`+"\n")
	pgidFile := filepath.Join(t.TempDir(), "pgid")

	var stdout bytes.Buffer
	cmd := exec.Command(bin, "check", "answer", "--topic", "c", "--json")
	cmd.Env = append(os.Environ(), "STUDY_HOME="+a.home, "PGID_FILE="+pgidFile)
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if _, err := os.Stat(pgidFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("study took %s to stop after SIGTERM", took)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Errorf("exit = %v, want status 1", err)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || env.OK || env.Error.Code != string(core.CodeCanceled) {
		t.Errorf("stdout = %s (%v), want a canceled error", stdout.String(), err)
	}
	data, err := os.ReadFile(pgidFile)
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Errorf("the Check's processes survived SIGTERM (kill 0: %v)", err)
	}
	for _, ev := range historyEvents(t, filepath.Join(a.home, "c", "history.jsonl")) {
		if ev.Type == "attempt.recorded" {
			t.Errorf("a cancelled Check recorded an Attempt")
		}
	}
}
