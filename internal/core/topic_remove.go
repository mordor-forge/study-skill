package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// removedDir is where removed Topics are kept, in the Study home's .lamplight
// folder: out of the way of status, never deleted.
const removedDir = "removed"

// TopicRemoval reports a Topic moved out of the Study home.
type TopicRemoval struct {
	Topic string `json:"topic"`
	// MovedTo is the folder the Topic now lives in, whole, git history
	// included: .lamplight/removed/<UTC time>-<topic>.
	MovedTo string `json:"moved_to"`
	// Restore is the shell command that moves the folder back, restoring
	// the Topic under its id. It checks first that no Topic has that id,
	// and moves nothing if one has.
	Restore string `json:"restore"`
	// Note says, in a dry run, that a write to the Topic is in progress:
	// the removal waits for it to finish.
	Note   string `json:"note,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// RemoveTopic moves a Topic's folder out of the Study home, into
// .lamplight/removed/<time>-<id>, on this computer only: nothing is deleted,
// and the Topic's git remote and other computers keep their copies. It
// takes the Topic's lock, so it waits for a write in progress, and refuses
// while an interrupted write waits to be finished.
//
// It exists for the learner: to import a v1 workspace again, for example
// with --not-done after the adoption showed a Lesson wrongly proven done.
// Agents have no tool for it.
func (c *Core) RemoveTopic(ctx context.Context, topicID string, dryRun bool) (TopicRemoval, error) {
	home, topic, err := c.openTopicFolder(topicID)
	if err != nil {
		return TopicRemoval{}, err
	}
	defer home.Close()
	defer topic.Close()

	if dryRun {
		out := c.removal(home, topicID)
		out.DryRun = true
		if wasInterrupted(home, topicID) {
			return TopicRemoval{}, interruptedWrite(topicID)
		}
		if hasIntent(home, topicID) {
			// A writer holding the lock is still at work; the real run
			// waits for it.
			out.Note = "a write to " + topicID + " is in progress: the removal waits for it to finish"
		}
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return TopicRemoval{}, err
	}
	unlock, err := c.lockOpenedTopic(ctx, home, topic, topicID)
	if err != nil {
		return TopicRemoval{}, err
	}
	defer unlock()
	// The lock is ours, so a marker left now is a write that was
	// interrupted, not one in progress.
	if hasIntent(home, topicID) {
		return TopicRemoval{}, interruptedWrite(topicID)
	}

	out := c.removal(home, topicID)
	rel, err := filepath.Rel(c.home, out.MovedTo)
	if err != nil {
		return TopicRemoval{}, internalError("placing the removed Topic", err)
	}
	dir := filepath.Dir(rel)
	if err := home.MkdirAll(dir, 0o755); err != nil {
		return TopicRemoval{}, internalError("creating "+dir, err)
	}
	if err := home.Rename(topicID, rel); err != nil {
		return TopicRemoval{}, moveError(topicID, out.MovedTo, err)
	}
	syncDir(home, ".")
	syncDir(home, dir)
	c.log.Info("removed a Topic from the Study home", "topic", topicID, "moved_to", out.MovedTo)
	return out, nil
}

// removal names where the Topic would go now: the UTC time, to the second,
// then the id, with a random suffix if that name is taken.
func (c *Core) removal(home *os.Root, topicID string) TopicRemoval {
	name := c.now().UTC().Format("20060102-150405") + "-" + topicID
	if _, err := home.Lstat(filepath.Join(localDir, removedDir, name)); err == nil {
		name += "-" + randomID()[:6]
	}
	movedTo := filepath.Join(c.home, localDir, removedDir, name)
	return TopicRemoval{Topic: topicID, MovedTo: movedTo, Restore: restoreCommand(movedTo, filepath.Join(c.home, topicID))}
}

// restoreCommand is the shell command that moves a removed Topic from its
// folder back to to. It moves nothing when something is at to already: mv
// alone would put the removed Topic inside the Topic that took its id, and
// succeed.
//
// The check comes before the move, not with it: the systems study runs on
// share no mv that refuses an existing destination (-T is GNU's). A Topic
// created under the id between the two would still get the removed one
// inside it; nothing is lost then, and it can be moved back out.
func restoreCommand(from, to string) string {
	return "test ! -e " + shellWord(to) + " && mv " + shellWord(from) + " " + shellWord(to)
}

func interruptedWrite(topicID string) error {
	return &Error{Code: CodeFailedPrecondition, Message: "a write to Topic " + topicID +
		" was interrupted: finish it first (study checkpoint --topic " + topicID + " --role learner, or any change " +
		"to the Topic), then remove it"}
}

// moveError explains a move the operating system refused.
func moveError(topicID, to string, err error) error {
	switch {
	case errors.Is(err, fs.ErrExist):
		return &Error{Code: CodeBusy, Err: err, Message: to + " appeared while Topic " + topicID + " was being removed: try again"}
	case errors.Is(err, syscall.EXDEV):
		return &Error{Code: CodeFailedPrecondition, Err: err, Message: "Topic " + topicID +
			" and the Study home's .lamplight folder are on different file systems, so the Topic cannot be moved there: " +
			"move the Topic's folder out of the Study home yourself"}
	case errors.Is(err, syscall.EBUSY):
		return &Error{Code: CodeBusy, Err: err, Message: "Topic " + topicID +
			"'s folder is in use or is a mount point: close the programs using it, or unmount it, and try again"}
	}
	return internalError("moving Topic "+topicID+" to "+to, err)
}

// shellWord quotes s as one word of a POSIX shell. It always quotes, so a
// command reads the same whatever the paths in it.
func shellWord(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
