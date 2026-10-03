package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// lockWait is how long a write waits for another process to finish writing
// the same Topic before giving up with CodeBusy.
const lockWait = 30 * time.Second

// lockTopic takes the per-Topic write lock, which serialises writes from
// every process: the CLI and the MCP server can run at the same time. It is
// an advisory lock on .lamplight/locks/<topic>.lock in the Study home, local
// and never synced, which the operating system releases if the process dies.
func lockTopic(ctx context.Context, home *os.Root, topicID string) (unlock func(), err error) {
	return lockFile(ctx, home, lockPath(topicID), "Topic "+topicID, "writing to Topic "+topicID)
}

// lockOpenedTopic takes the lock of a Topic whose folder the caller opened
// earlier, then checks that the folder is still the Topic: one removed
// (study topic remove) or replaced since, while the caller waited for the
// lock or did its work, would otherwise receive a write meant for the
// Topic. On any error the lock is not held.
func (c *Core) lockOpenedTopic(ctx context.Context, home, topic *os.Root, topicID string) (unlock func(), err error) {
	if err := c.crashAt(crashBeforeLock); err != nil {
		return nil, err
	}
	unlock, err = lockTopic(ctx, home, topicID)
	if err != nil {
		return nil, err
	}
	if err := stillTheTopic(home, topic, topicID); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

// stillTheTopic checks that the Study home's folder named topicID is the
// folder topic was opened on.
func stillTheTopic(home, topic *os.Root, topicID string) error {
	opened, err := topic.Stat(".")
	if err != nil {
		return internalError("reading Topic "+topicID, err)
	}
	now, err := home.Lstat(topicID)
	if err == nil && os.SameFile(opened, now) {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return internalError("reading Topic "+topicID, err)
	}
	return &Error{Code: CodeNotFound, Message: "Topic " + topicID + " was removed or replaced while this change was " +
		"under way, so nothing was written: run study status to see your Topics"}
}

// importLock serialises imports: two of the same v1 workspace must not both
// succeed. Its name cannot be a Topic id's.
const importLock = ".import"

// lockFile takes the advisory lock at rel in the Study home, waiting up to
// lockWait for another process to release it. what names the lock in
// errors, and doing what its holder is doing.
func lockFile(ctx context.Context, home *os.Root, rel, what, doing string) (unlock func(), err error) {
	dir := filepath.Dir(rel)
	if err := home.MkdirAll(dir, 0o755); err != nil {
		return nil, internalError("creating "+dir, err)
	}
	f, err := home.OpenFile(rel, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, internalError("opening the lock of "+what, err)
	}
	deadline := time.Now().Add(lockWait)
	wait := time.Millisecond
	for {
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, internalError("locking "+what, err)
		}
		if locked {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, &Error{Code: CodeBusy,
				Message: "another study process has been " + doing + " for over " + lockWait.String() + ": try again"}
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(2*wait, 50*time.Millisecond)
	}
}

func lockPath(topicID string) string { return filepath.Join(localDir, "locks", topicID+".lock") }

// wasInterrupted reports whether a write to the Topic was interrupted: its
// marker is there and no writer is at work. The marker is read while the
// Topic's lock is held, taken only if it is free: read one after the other,
// a marker and a free lock are also what a write that finished in between
// leaves, and status would flag a healthy write. It never creates the lock
// file and never waits.
func wasInterrupted(home *os.Root, topicID string) bool {
	f, err := home.OpenFile(lockPath(topicID), os.O_RDWR, 0)
	if err != nil {
		// No lock file, so no writer.
		return hasIntent(home, topicID)
	}
	defer f.Close()
	locked, err := tryLock(f)
	if err != nil {
		return hasIntent(home, topicID)
	}
	if !locked {
		// A writer is at work: its marker is a write in progress.
		return false
	}
	interrupted := hasIntent(home, topicID)
	_ = unlockFile(f)
	return interrupted
}

// lockHeld reports whether a writer holds the Topic's lock right now. It
// never creates the lock file and never waits.
func lockHeld(home *os.Root, topicID string) bool {
	f, err := home.OpenFile(lockPath(topicID), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	locked, err := tryLock(f)
	if err != nil {
		return false
	}
	if locked {
		_ = unlockFile(f)
		return false
	}
	return true
}
