package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordor-forge/lamplight/v2/internal/checkpoint"
)

const maxCheckpointMessageRunes = 200

// CheckpointSpec describes a Checkpoint to take.
type CheckpointSpec struct {
	// Topic is the ID of the Topic to checkpoint. Required: writes always
	// name their Topic.
	Topic string
	// Role is whose turn just ended, "agent" or "learner": whose work the
	// Checkpoint saves.
	Role string
	// Message summarises the turn. Optional.
	Message string
	// DryRun reports whether a Checkpoint would be made, and any large
	// files it would save, without committing.
	DryRun bool
}

// CheckpointResult reports a Checkpoint.
type CheckpointResult struct {
	Topic string `json:"topic"`
	// Committed is false when nothing changed since the last Checkpoint. In
	// a dry run it says whether a Checkpoint would be made.
	Committed bool `json:"committed"`
	// Commit is the new commit, or the previous one when nothing changed
	// ("" before the first Checkpoint, and in a dry run that would commit).
	Commit     string      `json:"commit"`
	LargeFiles []LargeFile `json:"large_files"`
	DryRun     bool        `json:"dry_run,omitempty"`
}

// LargeFile is a large file the Checkpoint added or changed. Data and model
// files usually belong in .gitignore instead.
type LargeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

// Checkpoint commits the learner's or the agent's work in a Topic, without
// running any program the Topic's git configuration names. See the design's
// Checkpoints section and ADR-0009.
//
// It holds the Topic lock, so it never commits a write half done, and first
// finishes an interrupted write, so a Checkpoint never carries an Event
// without its content, or a cut-off line of the History, to another machine.
//
// A Checkpoint of the role the History says is owed, after a turn switch or
// a completion, settles it: a checkpoint.taken Event records that.
func (c *Core) Checkpoint(ctx context.Context, spec CheckpointSpec) (CheckpointResult, error) {
	role, message, err := checkpointRoleAndMessage(spec)
	if err != nil {
		return CheckpointResult{}, err
	}
	// One opened folder serves the commit and its record in the History:
	// were the Topic replaced between the two, another Topic's History would
	// record a commit it does not have.
	home, topic, err := c.openTopicFolder(spec.Topic)
	if err != nil {
		return CheckpointResult{}, err
	}
	defer home.Close()
	defer topic.Close()
	out, err := c.takeCheckpoint(ctx, home, topic, spec, role, message)
	if err != nil || spec.DryRun {
		return out, err
	}
	_, err = c.writeOpenedTopic(ctx, home, topic, spec.Topic, func(s *replayed, _ *topicView) (*change, error) {
		owed := s.study.owed
		if owed == nil || owed.role != spec.Role {
			return nil, nil
		}
		return &change{Type: eventCheckpointTaken, Data: checkpointTakenData{
			For: owed.event, Role: spec.Role, Commit: out.Commit, Committed: out.Committed,
		}}, nil
	}, false)
	if err != nil {
		// The Checkpoint is taken; it stays owed in the History, and the
		// next one, skipped if nothing changed, settles it.
		c.log.Warn("could not record a Checkpoint the History owed", "topic", spec.Topic, "err", err)
	}
	return out, nil
}

const eventCheckpointTaken = "checkpoint.taken"

// checkpointTakenData is the payload of a checkpoint.taken Event: the
// Checkpoint that settled the one owed by Event For.
type checkpointTakenData struct {
	For       string `json:"for"`
	Role      string `json:"role"`
	Commit    string `json:"commit"`
	Committed bool   `json:"committed"`
}

func replayCheckpointTaken(s *replayed, ev event) error {
	var d checkpointTakenData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return fmt.Errorf("its payload is unreadable: %v", err)
	}
	if s.study.owed != nil && s.study.owed.event == d.For {
		s.study.owed = nil
	}
	return nil
}

// checkpointRoleAndMessage checks whose Checkpoint a spec asks for, and its
// message.
func checkpointRoleAndMessage(spec CheckpointSpec) (checkpoint.Role, string, error) {
	role := checkpoint.Role(spec.Role)
	switch {
	case role == "":
		return "", "", invalidf("say whose turn ended: pass the role \"agent\" or \"learner\"")
	case role != checkpoint.Agent && role != checkpoint.Learner:
		return "", "", invalidf("the role must be \"agent\" or \"learner\", not %q", spec.Role)
	}
	message, err := cleanText("message", spec.Message, maxCheckpointMessageRunes)
	return role, message, err
}

// takeCheckpoint commits the work in the Topic folder the caller opened.
func (c *Core) takeCheckpoint(ctx context.Context, home, topic *os.Root, spec CheckpointSpec, role checkpoint.Role,
	message string) (CheckpointResult, error) {
	if spec.DryRun {
		// A dry run writes nothing, so it cannot finish an interrupted
		// write, and what it would commit is not yet known.
		if hasIntent(home, spec.Topic) || historyTailPending(topic) {
			return CheckpointResult{}, &Error{Code: CodeFailedPrecondition, Message: "an interrupted write to " + spec.Topic +
				" must be finished first: a Checkpoint without --dry-run finishes it, and so does the next change"}
		}
	} else {
		if err := ctx.Err(); err != nil {
			return CheckpointResult{}, err
		}
		unlock, err := c.lockOpenedTopic(ctx, home, topic, spec.Topic)
		if err != nil {
			return CheckpointResult{}, err
		}
		defer unlock()
		if err := c.recoverTopic(home, topic, spec.Topic); err != nil {
			return CheckpointResult{}, err
		}
	}
	// git works on the folder by its path. The lock keeps study from moving
	// it, but not other programs: the Checkpoint is told which folder was
	// opened, and commits in no other.
	folder, err := topic.Stat(".")
	if err != nil {
		return CheckpointResult{}, internalError("reading Topic "+spec.Topic, err)
	}
	path := filepath.Join(c.home, spec.Topic)
	res, err := checkpoint.Take(ctx, path, checkpoint.Options{
		Role: role, Message: message, Time: c.now(), DryRun: spec.DryRun, Folder: folder,
	})
	if errors.Is(err, checkpoint.ErrRepositoryChanged) {
		if gone := stillTheTopic(home, topic, spec.Topic); gone != nil {
			return CheckpointResult{}, gone
		}
	}
	if err != nil {
		return CheckpointResult{}, checkpointError(spec.Topic, path, err)
	}
	out := CheckpointResult{Topic: spec.Topic, Committed: res.Committed, Commit: res.Commit,
		LargeFiles: []LargeFile{}, DryRun: spec.DryRun}
	for _, f := range res.LargeFiles {
		// Held-out data is kept out of the learner's sight, names included,
		// and belongs in the Topic however large it is.
		if strings.HasPrefix(f.Path, ".heldout/") {
			continue
		}
		out.LargeFiles = append(out.LargeFiles, LargeFile{Path: f.Path, Size: f.Size})
	}
	return out, nil
}

// checkpointError maps the checkpoint package's errors to core errors with
// advice the learner or the agent can act on. The cause is always kept.
func checkpointError(topic, path string, err error) error {
	fail := func(code ErrorCode, msg string) error {
		return &Error{Code: code, Message: "cannot checkpoint " + topic + ": " + msg, Err: err}
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, checkpoint.ErrIndexLocked):
		return fail(CodeBusy, "another program holds its git index. Try again in a moment; if no git program or "+
			"editor is using "+path+", delete "+filepath.Join(path, ".git", "index.lock"))
	case errors.Is(err, checkpoint.ErrRefLocked), errors.Is(err, checkpoint.ErrHeadMoved):
		return fail(CodeBusy, "another program is updating its git branch; try again in a moment")
	case errors.Is(err, checkpoint.ErrWorktreeChanged):
		return fail(CodeBusy, "files kept changing while they were being saved, as when an editor is saving; try again in a moment")
	case errors.Is(err, checkpoint.ErrRepositoryChanged):
		return fail(CodeCorrupt, "its git repository was replaced while the Checkpoint was being taken, "+
			"or leads outside the Topic ("+err.Error()+"). Nothing was committed; check "+filepath.Join(path, ".git"))
	case errors.Is(err, checkpoint.ErrGitNotFound):
		return fail(CodeFailedPrecondition, "git is not installed, or not on PATH: install git and try again")
	case errors.Is(err, checkpoint.ErrNotRepository):
		return fail(CodeCorrupt, "it has no usable git repository ("+err.Error()+"). Lamplight creates one with "+
			"every Topic; if .git is missing, run git -C "+path+" init --initial-branch=main")
	case errors.Is(err, checkpoint.ErrDetachedHead),
		errors.Is(err, checkpoint.ErrMergeInProgress),
		errors.Is(err, checkpoint.ErrRebaseInProgress),
		errors.Is(err, checkpoint.ErrCherryPickInProgress),
		errors.Is(err, checkpoint.ErrRevertInProgress),
		errors.Is(err, checkpoint.ErrUnmergedPaths),
		errors.Is(err, checkpoint.ErrNoIdentity):
		return fail(CodeFailedPrecondition, err.Error())
	}
	return internalError("checkpointing "+topic, err)
}
