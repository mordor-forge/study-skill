// Package checkpoint takes Checkpoints: snapshots of a Topic, stored as git
// commits, taken whenever the turn passes between the agent and the learner
// so the learner's own work can be seen on its own. It also computes the
// snapshot hash of one folder that an Attempt records.
//
// # Security
//
// The agent can edit a Topic's .git/config and .gitattributes, and this code
// runs outside the agent's sandbox, so it never runs a program that
// repository configuration names (ADR-0009): hooks, clean, smudge and process
// filters, fsmonitor, textconv and external diff drivers, or signing programs.
// It uses only plumbing that cannot start one:
//
//   - ls-files lists tracked and untracked files without reading them;
//   - hash-object -w --no-filters writes blobs from files this process opened
//     through an os.Root, so nothing outside the Topic is read;
//   - update-index --index-info, write-tree, commit-tree --no-gpg-sign and
//     update-ref record them;
//   - diff-tree --no-textconv --no-ext-diff finds large files.
//
// Every call runs with GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_NOSYSTEM=1,
// GIT_DIR and GIT_WORK_TREE pinned to the Topic, no inherited GIT_* variable,
// stdin from the null device and --no-pager. Command-scope configuration sets
// core.hooksPath=/dev/null, core.fsmonitor=false, commit.gpgSign=false and
// protocol.allow=never, and switches off every hook event and every hook named
// in configuration (hook.<name>.command), which core.hooksPath alone does not
// stop. Read-only calls set GIT_OPTIONAL_LOCKS=0. The only calls that see the
// learner's global and system configuration are plain `git config --get`
// reads of user.name, user.email and core.excludesFile, made beforehand; the
// identity is then passed explicitly to commit-tree.
//
// The Topic's .git must be a real folder inside it, not a .git file, a
// symbolic link or a linked worktree, so a Checkpoint never writes to another
// repository. Both folders are pinned when a Checkpoint starts and checked
// again before every step in which git writes; on Linux git itself works on
// the pinned folders (see repo). A swap in between fails with
// ErrRepositoryChanged.
//
// # Raw bytes
//
// Checkpoints store files exactly as they are on disk. Line-ending conversion
// (core.autocrlf, the text and eol attributes), ident, clean filters and Git
// LFS are not applied, and modes come from the file system (core.fileMode is
// not consulted). Symbolic links are stored as links (mode 120000) and never
// followed; nested repositories are left out. A learner whose own git applies
// conversions may see such files as modified after a Checkpoint.
//
// # The index
//
// Take holds the Topic's index.lock for the whole operation, as git commands
// do, waiting briefly while an editor holds it. It stages into a private copy
// of the index (GIT_INDEX_FILE), so a refusal or failure leaves the learner's
// index untouched. Once the commit object exists, the copy replaces the index
// by an atomic rename and the branch is moved; if the branch cannot be moved,
// the learner's index is put back. The learner's index then matches the
// Checkpoint, so `git status` stays clean, and entries whose contents did not
// change keep their cached file stats. Snapshot only reads the learner's
// index, and a dry run writes nothing to .git at all.
package checkpoint

import (
	"errors"
	"io/fs"
	"time"
)

// Errors returned by Take and Snapshot. Refusals leave the Topic unchanged.
var (
	ErrNotRepository        = errors.New("not the top folder of a git repository")
	ErrDetachedHead         = errors.New("HEAD is not on a branch; check out a branch first")
	ErrMergeInProgress      = errors.New("a merge is in progress; finish or abort it first")
	ErrRebaseInProgress     = errors.New("a rebase (or git am) is in progress; finish or abort it first")
	ErrCherryPickInProgress = errors.New("a cherry-pick is in progress; finish or abort it first")
	ErrRevertInProgress     = errors.New("a revert is in progress; finish or abort it first")
	ErrUnmergedPaths        = errors.New("some files have unresolved conflicts; resolve them first")
	ErrNoIdentity           = errors.New(`no git identity; set one with "git config --global user.name" and "git config --global user.email"`)
	ErrIndexLocked          = errors.New("another git process holds the index (.git/index.lock exists)")
	ErrRefLocked            = errors.New("another git process is updating the branch (a .lock file exists under .git/refs)")
	ErrHeadMoved            = errors.New("the branch moved while the checkpoint was being taken")
	ErrWorktreeChanged      = errors.New("files kept changing while the checkpoint was being taken")
	ErrRepositoryChanged    = errors.New("the Topic's git repository was replaced or links outside the Topic")
	ErrGitNotFound          = errors.New("git is not installed, or not on PATH")
	ErrInvalidRole          = errors.New(`role must be "agent" or "learner"`)
	ErrInvalidPath          = errors.New("path must name a folder inside the Topic")
)

// testHook, when set by a test, runs at named points of Take: "locked" once
// the index lock is held, "hash" before each batch of files is opened for
// hashing, and "update-ref" just before the branch is moved, after the last
// verification.
var testHook func(point string)

func hook(point string) {
	if testHook != nil {
		testHook(point)
	}
}

// Role says whose turn just ended: whose work the Checkpoint saves.
type Role string

// Roles, rendered as a "[agent]" or "[learner]" prefix on the commit message.
const (
	Agent   Role = "agent"
	Learner Role = "learner"
)

// Defaults for Options.
const (
	DefaultLargeFileThreshold int64 = 10 << 20
	DefaultLockTimeout              = 5 * time.Second
)

// Options describe one Checkpoint.
type Options struct {
	Role    Role
	Message string    // summary after the role prefix; "Checkpoint" when empty
	Time    time.Time // author and committer time; now when zero

	// LargeFileThreshold is the size in bytes from which an added or
	// changed file is reported in Result.LargeFiles. Zero means
	// DefaultLargeFileThreshold; a negative value reports nothing.
	LargeFileThreshold int64
	// LockTimeout bounds the wait for another process's index.lock. Zero
	// means DefaultLockTimeout.
	LockTimeout time.Duration
	// DryRun computes the Checkpoint, including the refusals and the large
	// files, without writing anything to .git: no objects, no index, no
	// lock. It reads the learner's index as it is, so it does not wait for
	// an editor that holds the lock.
	DryRun bool
	// Folder, when set, is the Topic folder as the caller opened it. Take
	// fails with ErrRepositoryChanged when dir no longer names that folder,
	// so a Checkpoint never commits in a folder put in the Topic's place
	// after the caller checked it.
	Folder fs.FileInfo
}

// Result describes a Checkpoint.
type Result struct {
	// Committed is false when nothing changed since HEAD, so no commit
	// was made. In a dry run it says whether a commit would be made.
	Committed bool
	// Commit is the new commit, or HEAD when skipped ("" before the first
	// commit). In a dry run that would commit, it is "".
	Commit string
	// Tree is the tree of the working tree as it was snapshotted. A dry run
	// that would commit writes no tree, so it is "" then.
	Tree string
	// LargeFiles lists files at or above the threshold that this
	// Checkpoint added or changed, by path.
	LargeFiles []LargeFile
}

// LargeFile is a file a Checkpoint added or changed that is at least as large
// as the threshold.
type LargeFile struct {
	Path string
	Size int64
}
