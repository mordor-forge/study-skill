package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Removing a Topic moves its folder, whole, into .lamplight/removed, where
// moving it back restores it; the dry run reports the same place and moves
// nothing.
func TestRemoveTopicMovesItOutOfTheStudyHome(t *testing.T) {
	ctx := context.Background()
	gitIdentity(t)
	home := t.TempDir()
	m := newMachine(t, home, "id", t0)
	if _, err := m.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "Write a CLI"}); err != nil {
		t.Fatal(err)
	}

	dry, err := m.RemoveTopic(ctx, "rust", true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".lamplight", "removed", "20261001-093000-rust")
	if !dry.DryRun || dry.MovedTo != want {
		t.Fatalf("dry run = %+v, want moved_to %s", dry, want)
	}
	if !exists(filepath.Join(home, "rust", "topic.toml")) || exists(want) {
		t.Fatal("the dry run moved the Topic")
	}

	got, err := m.RemoveTopic(ctx, "rust", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.DryRun || got.MovedTo != dry.MovedTo {
		t.Fatalf("removal = %+v, the dry run said %+v", got, dry)
	}
	for _, rel := range []string{"topic.toml", "history.jsonl", ".git"} {
		if !exists(filepath.Join(want, rel)) {
			t.Errorf("the removed Topic lacks %s", rel)
		}
	}
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Topics) != 0 || len(status.Problems) != 0 || status.ActiveTopic != nil {
		t.Fatalf("status after the removal = %+v", status)
	}

	// The id is free again, and a second removal in the same second does
	// not overwrite the first.
	if _, err := m.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "Write a web server"}); err != nil {
		t.Fatal(err)
	}
	again, err := m.RemoveTopic(ctx, "rust", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.MovedTo == got.MovedTo || !exists(filepath.Join(got.MovedTo, "topic.toml")) ||
		!exists(filepath.Join(again.MovedTo, "topic.toml")) {
		t.Fatalf("two removals of one id: %s and %s", got.MovedTo, again.MovedTo)
	}

	// The restore command, run as given, brings the Topic back under its id.
	back := shellWord(filepath.Join(home, "rust"))
	if want := "test ! -e " + back + " && mv " + shellWord(got.MovedTo) + " " + back; got.Restore != want {
		t.Errorf("restore = %q, want %q", got.Restore, want)
	}
	if out, err := exec.Command("sh", "-c", got.Restore).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", got.Restore, err, out)
	}
	status, err = m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Topics) != 1 || status.Topics[0].Goal != "Write a CLI" || len(status.Topics[0].Flags) != 0 {
		t.Fatalf("status after moving it back = %+v", status.Topics)
	}
}

// Once another Topic has the id, the restore command refuses: it never moves
// the removed Topic inside the Topic that took its place.
func TestRestoreCommandRefusesATakenID(t *testing.T) {
	ctx := context.Background()
	gitIdentity(t)
	home := t.TempDir()
	m := newMachine(t, home, "id", t0)
	if _, err := m.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "Write a CLI"}); err != nil {
		t.Fatal(err)
	}
	removed, err := m.RemoveTopic(ctx, "rust", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "Write a web server"}); err != nil {
		t.Fatal(err)
	}

	if out, err := exec.Command("sh", "-c", removed.Restore).CombinedOutput(); err == nil {
		t.Errorf("%s succeeded although another Topic is named rust\n%s", removed.Restore, out)
	}
	if !exists(filepath.Join(removed.MovedTo, "topic.toml")) {
		t.Errorf("the removed Topic left %s", removed.MovedTo)
	}
	if nested := filepath.Join(home, "rust", filepath.Base(removed.MovedTo)); exists(nested) {
		t.Errorf("the removed Topic was moved inside the new one, to %s", nested)
	}
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Topics) != 1 || status.Topics[0].Goal != "Write a web server" || len(status.Topics[0].Flags) != 0 {
		t.Fatalf("status after the refused restore = %+v", status.Topics)
	}
}

// The reason removal exists: an import whose proof marked a Lesson done
// that the learner did not finish is removed, then imported again with
// --not-done.
func TestRemoveTopicLetsAnImportBeRedone(t *testing.T) {
	ctx := context.Background()
	gitIdentity(t)
	src := v1Workspace(t, v1Options{commits: []string{"[agent] complete lesson 01"}})
	m := newMachine(t, t.TempDir(), "id", t0)
	if _, err := m.ImportV1(ctx, ImportSpec{Dir: src}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportV1(ctx, ImportSpec{Dir: src, NotDone: []string{"lesson-01"}}); CodeOf(err) != CodeAlreadyExists {
		t.Fatalf("importing over the Topic: %v", err)
	}
	if _, err := m.RemoveTopic(ctx, "go-concurrency", false); err != nil {
		t.Fatal(err)
	}
	again, err := m.ImportV1(ctx, ImportSpec{Dir: src, NotDone: []string{"lesson-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if ids := lessonIDs(again.Completed); len(ids) != 1 || ids[0] != "lesson-02" {
		t.Errorf("completed after the second import = %v, want lesson-02 only", ids)
	}
}

func TestRemoveTopicRefusals(t *testing.T) {
	ctx := context.Background()
	gitIdentity(t)
	home := t.TempDir()
	m := newMachine(t, home, "id", t0)
	if _, err := m.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "Write a CLI"}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.RemoveTopic(ctx, "go", false); CodeOf(err) != CodeNotFound {
		t.Errorf("an unknown Topic: %v", err)
	}
	if _, err := m.RemoveTopic(ctx, "../rust", false); CodeOf(err) != CodeInvalidArgument {
		t.Errorf("a path for an id: %v", err)
	}
	if err := os.Symlink(filepath.Join(home, "rust"), filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RemoveTopic(ctx, "alias", false); CodeOf(err) != CodeCorrupt {
		t.Errorf("a link to a Topic: %v", err)
	}

	// An interrupted write (a marker and no writer holding the lock) is
	// finished first, by the next write, never carried away half done.
	leaveIntent(t, home, "rust")
	for _, dryRun := range []bool{true, false} {
		if _, err := m.RemoveTopic(ctx, "rust", dryRun); CodeOf(err) != CodeFailedPrecondition ||
			!strings.Contains(err.Error(), "interrupted") {
			t.Errorf("an interrupted write (dry run %v): %v", dryRun, err)
		}
	}
	if !exists(filepath.Join(home, "rust", "topic.toml")) {
		t.Fatal("a refused removal moved the Topic")
	}
}

// Moves the operating system refuses get advice, not an internal error.
func TestRemoveTopicExplainsARefusedMove(t *testing.T) {
	for _, tc := range []struct {
		errno syscall.Errno
		code  ErrorCode
		says  string
	}{
		{syscall.EXDEV, CodeFailedPrecondition, "different file systems"},
		{syscall.EBUSY, CodeBusy, "in use or is a mount point"},
	} {
		err := moveError("rust", "/study/.lamplight/removed/x", &os.LinkError{Op: "rename", Old: "rust", New: "x", Err: tc.errno})
		if CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.says) || !errors.Is(err, tc.errno) {
			t.Errorf("%v: %v (%s)", tc.errno, err, CodeOf(err))
		}
	}
}

// leaveIntent leaves the marker a write to the Topic leaves while it runs.
func leaveIntent(t *testing.T, home, topicID string) {
	t.Helper()
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeIntent(root, intent{Format: FormatVersion, Topic: topicID, Event: "id999", Type: "topic.updated"}); err != nil {
		t.Fatal(err)
	}
}
