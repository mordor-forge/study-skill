package core

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// atPoint runs do once, the first time a write reaches point, then lets the
// write go on: what another process could do while this one waits there.
func atPoint(point string, do func()) func(string) error {
	var done atomic.Bool
	return func(p string) error {
		if p == point && done.CompareAndSwap(false, true) {
			do()
		}
		return nil
	}
}

// removalRace is a Topic two machines share: one writes, one removes.
type removalRace struct {
	home            string
	writer, remover *machine
}

func newRemovalRace(t *testing.T) removalRace {
	t.Helper()
	gitIdentity(t)
	home := t.TempDir()
	r := removalRace{home: home, writer: newMachine(t, home, "w", t0), remover: newMachine(t, home, "r", t0)}
	if _, err := r.writer.CreateTopic(context.Background(), TopicSpec{Title: "Rust", Goal: "Write a CLI"}); err != nil {
		t.Fatal(err)
	}
	// Work for a Checkpoint to save.
	if err := os.WriteFile(filepath.Join(home, "rust", "notes.md"), []byte("ownership\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r
}

// removed is the one folder the removals left in .lamplight/removed.
func (r removalRace) removed(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.home, ".lamplight", removedDir))
	if err != nil || len(entries) != 1 {
		t.Fatalf("removed Topics: %v, %v", entries, err)
	}
	return filepath.Join(r.home, ".lamplight", removedDir, entries[0].Name())
}

// The writers that open a Topic and then wait for its lock: each must find,
// once it has the lock, whether the Topic was removed meanwhile.
var raceWriters = []struct {
	name  string
	write func(m *machine) error
	// wrote reports whether the write is in the Topic folder dir.
	wrote func(t *testing.T, dir string) bool
}{
	{"UpdateTopic", func(m *machine) error {
		goal := "Write a web server"
		_, err := m.UpdateTopic(context.Background(), "rust", TopicChanges{Goal: &goal})
		return err
	}, func(t *testing.T, dir string) bool {
		return readSettings(t, dir).Goal == "Write a web server"
	}},
	{"Checkpoint", func(m *machine) error {
		_, err := m.Checkpoint(context.Background(), CheckpointSpec{Topic: "rust", Role: "learner"})
		return err
	}, func(t *testing.T, dir string) bool {
		return strings.Contains(git(t, dir, "ls-files"), "notes.md")
	}},
}

// A write that waited for the lock while the Topic was removed writes
// nothing, and above all nothing into the removed folder; a write that got
// the lock first is moved along with the Topic. Both orders, both writers.
func TestAWriteWaitingForTheLockNeverLandsInARemovedTopic(t *testing.T) {
	ctx := context.Background()
	for _, w := range raceWriters {
		t.Run(w.name+", the removal first", func(t *testing.T) {
			r := newRemovalRace(t)
			var removedHash string
			r.writer.crash = atPoint(crashBeforeLock, func() {
				if _, err := r.remover.RemoveTopic(ctx, "rust", false); err != nil {
					t.Errorf("RemoveTopic: %v", err)
				}
				removedHash = treeHash(t, r.removed(t))
			})
			if err := w.write(r.writer); CodeOf(err) != CodeNotFound || !strings.Contains(err.Error(), "removed or replaced") {
				t.Fatalf("a write that waited through the removal: %v", err)
			}
			dir := r.removed(t)
			if treeHash(t, dir) != removedHash || w.wrote(t, dir) {
				t.Error("the write landed in the removed Topic")
			}
			if exists(filepath.Join(r.home, "rust")) || hasIntentIn(t, r.home, "rust") {
				t.Error("the refused write left something behind")
			}
		})
		t.Run(w.name+", the write first", func(t *testing.T) {
			r := newRemovalRace(t)
			r.remover.crash = atPoint(crashBeforeLock, func() {
				if err := w.write(r.writer); err != nil {
					t.Errorf("the write: %v", err)
				}
			})
			if _, err := r.remover.RemoveTopic(ctx, "rust", false); err != nil {
				t.Fatal(err)
			}
			if dir := r.removed(t); !w.wrote(t, dir) {
				t.Error("the write made before the removal is not in the removed Topic")
			}
			if exists(filepath.Join(r.home, "rust")) {
				t.Error("the Topic is still in the Study home")
			}
		})
	}
}

// The removal re-checks, once it holds the lock, what it checked before.
func TestRemoveTopicRechecksAfterTheLock(t *testing.T) {
	ctx := context.Background()

	t.Run("a write interrupted while it waited", func(t *testing.T) {
		r := newRemovalRace(t)
		r.remover.crash = atPoint(crashBeforeLock, func() { leaveIntent(t, r.home, "rust") })
		if _, err := r.remover.RemoveTopic(ctx, "rust", false); CodeOf(err) != CodeFailedPrecondition ||
			!strings.Contains(err.Error(), "interrupted") {
			t.Fatalf("RemoveTopic: %v", err)
		}
		if !exists(filepath.Join(r.home, "rust", "topic.toml")) {
			t.Error("the Topic was moved with an interrupted write")
		}
	})

	t.Run("the Topic replaced while it waited", func(t *testing.T) {
		r := newRemovalRace(t)
		r.remover.crash = atPoint(crashBeforeLock, func() {
			if err := os.Rename(filepath.Join(r.home, "rust"), filepath.Join(t.TempDir(), "rust")); err != nil {
				t.Error(err)
			}
			if _, err := r.writer.CreateTopic(ctx, TopicSpec{Title: "Rust", Goal: "A new start"}); err != nil {
				t.Error(err)
			}
		})
		if _, err := r.remover.RemoveTopic(ctx, "rust", false); CodeOf(err) != CodeNotFound {
			t.Fatalf("RemoveTopic: %v", err)
		}
		if readSettings(t, filepath.Join(r.home, "rust")).Goal != "A new start" {
			t.Error("the removal moved the Topic that replaced the one it opened")
		}
	})

	t.Run("the Topic removed while it waited", func(t *testing.T) {
		r := newRemovalRace(t)
		r.writer.crash = atPoint(crashBeforeLock, func() {
			if _, err := r.remover.RemoveTopic(ctx, "rust", false); err != nil {
				t.Error(err)
			}
		})
		if _, err := r.writer.RemoveTopic(ctx, "rust", false); CodeOf(err) != CodeNotFound {
			t.Fatalf("the second removal: %v", err)
		}
		r.removed(t) // exactly one
	})
}

// A marker with a writer holding the lock is a write in progress, not an
// interrupted one: the dry run says so without waiting, and the removal
// waits for the write, then moves the Topic with it.
func TestRemoveTopicWaitsForAWriteInProgress(t *testing.T) {
	ctx := context.Background()
	r := newRemovalRace(t)
	hook, reached, release := pauseAt(crashAfterIntent)
	r.writer.crash = hook
	written := make(chan error, 1)
	go func() { written <- raceWriters[0].write(r.writer) }()
	<-reached

	dry, err := r.remover.RemoveTopic(ctx, "rust", true)
	if err != nil || !strings.Contains(dry.Note, "in progress") {
		t.Fatalf("dry run during a write = %+v, %v", dry, err)
	}
	// The removal opens the Topic and reaches the lock while the write still
	// holds it, its marker written.
	waiting := make(chan struct{})
	r.remover.crash = atPoint(crashBeforeLock, func() { close(waiting) })
	removed := make(chan error, 1)
	go func() {
		_, err := r.remover.RemoveTopic(ctx, "rust", false)
		removed <- err
	}()
	select {
	case <-waiting:
	case err := <-removed:
		t.Fatalf("the removal did not wait for the write: %v", err)
	}
	release()
	if err := <-written; err != nil {
		t.Fatalf("the write: %v", err)
	}
	if err := <-removed; err != nil {
		t.Fatalf("the removal after the write: %v", err)
	}
	if !raceWriters[0].wrote(t, r.removed(t)) {
		t.Error("the removed Topic lacks the write it waited for")
	}
}

func hasIntentIn(t *testing.T, home, topicID string) bool {
	t.Helper()
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return hasIntent(root, topicID)
}

// A Check runs for as long as its commands do. When the Topic is removed
// meanwhile and another takes its id, the Attempt is recorded in neither: it
// judged work in a Topic that is no longer the one named.
func TestACheckThatOutlivesItsTopicRecordsNothing(t *testing.T) {
	ctx := context.Background()
	m := learningTopic(t)
	var removed TopicRemoval
	swap := func(CheckProgress) {
		if removed.MovedTo != "" {
			return
		}
		var err error
		if removed, err = m.RemoveTopic(ctx, "c", false); err != nil {
			t.Errorf("RemoveTopic: %v", err)
			return
		}
		// The same Topic, started again from a copy: another folder.
		if err := os.CopyFS(filepath.Join(m.home, "c"), os.DirFS(removed.MovedTo)); err != nil {
			t.Error(err)
		}
	}
	a, err := m.RunCheck(ctx, "c", "answer", CheckOptions{Progress: swap})
	if CodeOf(err) != CodeNotFound || !strings.Contains(err.Error(), "removed or replaced") {
		t.Errorf("RunCheck = %+v, %v; want not_found", a, err)
	}
	for _, dir := range []string{filepath.Join(m.home, "c"), removed.MovedTo} {
		history, err := os.ReadFile(filepath.Join(dir, historyFile))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(history), eventAttemptRecorded) {
			t.Errorf("an Attempt was recorded in %s", dir)
		}
	}
}

// A Checkpoint commits in the folder it checked under the lock, or nowhere.
// A program that ignores the Topic's lock can still move that folder away
// and put another in its place before the commit: it must not get the
// commit.
func TestACheckpointNeverCommitsInAFolderPutInTheTopicsPlace(t *testing.T) {
	ctx := context.Background()
	r := newRemovalRace(t)
	moved := filepath.Join(t.TempDir(), "rust")
	// The Checkpoint reads the clock once it holds the lock and has checked
	// the folder, just before it commits.
	swap := func() {
		if err := os.Rename(filepath.Join(r.home, "rust"), moved); err != nil {
			t.Error(err)
		}
		if err := os.CopyFS(filepath.Join(r.home, "rust"), os.DirFS(moved)); err != nil {
			t.Error(err)
		}
	}
	c, err := Open(Options{
		Getenv: func(key string) string { return map[string]string{"STUDY_HOME": r.home, "HOME": r.home}[key] },
		Dir:    r.home,
		Now: func() time.Time {
			if swap != nil {
				do := swap
				swap = nil
				do()
			}
			return t0
		},
		NewID:  func() string { return "c001" },
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Checkpoint(ctx, CheckpointSpec{Topic: "rust", Role: "learner"})
	if CodeOf(err) != CodeNotFound || !strings.Contains(err.Error(), "removed or replaced") {
		t.Errorf("Checkpoint = %+v, %v; want not_found", res, err)
	}
	for _, dir := range []string{filepath.Join(r.home, "rust"), moved} {
		if raceWriters[1].wrote(t, dir) {
			t.Errorf("the Checkpoint committed in %s", dir)
		}
	}
}
