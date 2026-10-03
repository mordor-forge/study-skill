package checkpoint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Take commits the Topic's working tree in dir, which must be the top folder
// of its git repository: every tracked and untracked file that .gitignore
// does not exclude, including deletions. It skips the commit when the tree
// matches HEAD, and refuses during a merge, rebase, cherry-pick or revert, on
// a detached HEAD, or with unresolved conflicts.
func Take(ctx context.Context, dir string, opts Options) (Result, error) {
	if opts.Role != Agent && opts.Role != Learner {
		return Result{}, fmt.Errorf("%w, not %q", ErrInvalidRole, opts.Role)
	}
	when := opts.Time
	if when.IsZero() {
		when = time.Now()
	}
	threshold := opts.LargeFileThreshold
	if threshold == 0 {
		threshold = DefaultLargeFileThreshold
	}
	lockTimeout := opts.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = DefaultLockTimeout
	}

	r, err := open(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	defer r.close()
	// From here on the pinned folder is the one checked before every write.
	if opts.Folder != nil && !os.SameFile(opts.Folder, r.dirInfo) {
		return Result{}, fmt.Errorf("%w: %s is no longer the folder the Checkpoint was asked for", ErrRepositoryChanged, r.dir)
	}
	identity, err := r.identity(ctx, when)
	if err != nil {
		return Result{}, err
	}
	if opts.DryRun {
		return r.preview(ctx, threshold)
	}

	unlock, err := r.lockIndex(ctx, lockTimeout)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	hook("locked")
	// The agent may have swapped folders while the lock was awaited.
	if err := r.verify(); err != nil {
		return Result{}, err
	}
	branch, err := r.checkState(ctx)
	if err != nil {
		return Result{}, err
	}
	head, headTree, err := r.head(ctx)
	if err != nil {
		return Result{}, err
	}

	tmp, err := r.tempDir()
	if err != nil {
		return Result{}, err
	}
	defer r.gitRoot.RemoveAll(tmp)
	if err := copyIndex(r.gitRoot, "index", tmp+"/index"); err != nil {
		return Result{}, err
	}
	tree, states, err := r.stageWorktree(ctx, r.indexArg(tmp))
	if err != nil {
		return Result{}, err
	}
	if tree == headTree {
		return Result{Commit: head, Tree: tree}, nil
	}
	large, err := r.largeFiles(ctx, headTree, tree, states, threshold)
	if err != nil {
		return Result{}, err
	}

	msg := message(opts.Role, opts.Message)
	args := []string{"commit-tree", "--no-gpg-sign"}
	if head != "" {
		args = append(args, "-p", head)
	}
	out, err := r.git(ctx, call{stdin: strings.NewReader(msg), env: identity}, append(args, tree)...)
	if err != nil {
		return Result{}, err
	}
	commit := strings.TrimSpace(out)

	// Still under index.lock. The learner's index is kept aside until the
	// branch has moved, and put back if it does not.
	restore, err := r.swapIndex(tmp)
	if err != nil {
		return Result{}, err
	}
	fail := func(err error) (Result, error) {
		if rerr := restore(); rerr != nil {
			err = fmt.Errorf("%w (and putting the index back failed: %v)", err, rerr)
		}
		return Result{}, err
	}
	if err := r.verify(); err != nil {
		return fail(err)
	}
	if err := r.verifyBranch(branch); err != nil {
		return fail(err)
	}
	hook("update-ref")
	subject, _, _ := strings.Cut(msg, "\n")
	if _, err := r.git(ctx, call{}, "update-ref", "-m", "checkpoint: "+subject, "HEAD", commit, head); err != nil {
		// git may have moved the branch before it was stopped.
		now, _, headErr := r.head(context.WithoutCancel(ctx))
		switch {
		case headErr == nil && now == commit:
		case headErr == nil && now != head:
			return fail(fmt.Errorf("%w: %v", ErrHeadMoved, err))
		case refLocked(err):
			return fail(fmt.Errorf("%w: %v", ErrRefLocked, err))
		default:
			return fail(err)
		}
	}
	if err := r.verifyRef(branch, commit); err != nil {
		return fail(err)
	}
	return Result{Committed: true, Commit: commit, Tree: tree, LargeFiles: large}, nil
}

// preview is a dry run of Take. It compares what a Checkpoint would hold with
// HEAD, file by file, from hashes that are computed but not written, so it
// writes nothing to .git and takes no lock. It reads the learner's index as
// it is: git replaces the index by renaming, so the read is consistent even
// while an editor works.
func (r *repo) preview(ctx context.Context, threshold int64) (Result, error) {
	cmp, err := r.compareWithHead(ctx)
	if err != nil {
		return Result{}, err
	}
	changed := len(cmp.next) != len(cmp.current)
	var large []LargeFile
	for p, e := range cmp.next {
		if c, ok := cmp.current[p]; ok && c == e {
			continue
		}
		changed = true
		if st, ok := cmp.states[p]; ok && threshold >= 0 && st.mode != modeSymlink && st.size >= threshold {
			large = append(large, LargeFile{Path: p, Size: st.size})
		}
	}
	if !changed {
		return Result{Commit: cmp.head, Tree: cmp.headTree}, nil
	}
	sort.Slice(large, func(i, j int) bool { return large[i].Path < large[j].Path })
	return Result{Committed: true, LargeFiles: large}, nil
}

// comparison is what a Checkpoint would hold, next to what HEAD holds.
type comparison struct {
	head, headTree string
	next, current  map[string]treeEntry
	states         map[string]fileState
}

// compareWithHead lists the files a Checkpoint would hold, hashed but not
// written, and the files of HEAD. It writes nothing and takes no lock.
func (r *repo) compareWithHead(ctx context.Context) (comparison, error) {
	if _, err := r.checkState(ctx); err != nil {
		return comparison{}, err
	}
	head, headTree, err := r.head(ctx)
	if err != nil {
		return comparison{}, err
	}
	entries, err := r.listIndex(ctx, "", "")
	if err != nil {
		return comparison{}, err
	}
	next := map[string]treeEntry{}
	var candidates []string
	for _, e := range entries {
		if e.stage != "0" {
			return comparison{}, fmt.Errorf("%w (%s)", ErrUnmergedPaths, e.path)
		}
		if e.skipWorktree || e.mode == modeGitlink {
			next[e.path] = treeEntry{mode: e.mode, oid: e.oid}
			continue
		}
		candidates = append(candidates, e.path)
	}
	untracked, err := r.listUntracked(ctx, "", "")
	if err != nil {
		return comparison{}, err
	}
	states, err := r.hashWorktree(ctx, append(candidates, untracked...), false)
	if err != nil {
		return comparison{}, err
	}
	for p, st := range states {
		next[p] = treeEntry{mode: st.mode, oid: st.oid}
	}
	current, err := r.lsTree(ctx, head)
	if err != nil {
		return comparison{}, err
	}
	return comparison{head: head, headTree: headTree, next: next, current: current, states: states}, nil
}

// Kinds of change that ChangesSince reports.
const (
	Added    = "added"
	Modified = "modified"
	Deleted  = "deleted"
)

// Change is one file that differs from the last Checkpoint.
type Change struct {
	Path string
	Kind string
}

// Changes is what differs between a Topic's working tree and its last
// Checkpoint.
type Changes struct {
	// Since is the last Checkpoint's commit, "" before the first.
	Since string
	// Files are the files added, modified or deleted since, by path.
	Files []Change
}

// ChangesSince reports what changed in the Topic's working tree in dir since
// its last Checkpoint, by the rules a Checkpoint follows: tracked and
// untracked files that are not ignored, compared as raw bytes. Like a dry
// run of Take, it writes nothing to .git, takes no lock and needs no git
// identity.
func ChangesSince(ctx context.Context, dir string) (Changes, error) {
	r, err := open(ctx, dir)
	if err != nil {
		return Changes{}, err
	}
	defer r.close()
	cmp, err := r.compareWithHead(ctx)
	if err != nil {
		return Changes{}, err
	}
	out := Changes{Since: cmp.head, Files: []Change{}}
	for p, e := range cmp.next {
		switch c, ok := cmp.current[p]; {
		case !ok:
			out.Files = append(out.Files, Change{Path: p, Kind: Added})
		case c != e:
			out.Files = append(out.Files, Change{Path: p, Kind: Modified})
		}
	}
	for p := range cmp.current {
		if _, ok := cmp.next[p]; !ok {
			out.Files = append(out.Files, Change{Path: p, Kind: Deleted})
		}
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

// treeEntry is one file of a tree, flattened.
type treeEntry struct{ mode, oid string }

// lsTree lists every file in a commit's tree. It reads objects only.
func (r *repo) lsTree(ctx context.Context, commit string) (map[string]treeEntry, error) {
	files := map[string]treeEntry{}
	if commit == "" {
		return files, nil
	}
	out, err := r.git(ctx, call{readOnly: true}, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	for _, record := range splitNUL(out) {
		meta, p, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree: unexpected entry %q", record)
		}
		files[p] = treeEntry{mode: fields[0], oid: fields[2]}
	}
	return files, nil
}

// Snapshot returns the tree hash of the folder subpath (such as
// "practice/<lesson-id>") as the working tree holds it now, by the same rules
// as Take: tracked and untracked files that are not ignored, stored as raw
// bytes. It equals that folder's tree in a Checkpoint taken at the same
// moment, and it neither commits nor touches the learner's index. A missing
// folder hashes as the empty tree; "" or "." means the whole Topic.
func Snapshot(ctx context.Context, dir, subpath string) (string, error) {
	sub, err := cleanSubpath(subpath)
	if err != nil {
		return "", err
	}
	r, err := open(ctx, dir)
	if err != nil {
		return "", err
	}
	defer r.close()
	if exists, err := r.folderExists(sub); err != nil {
		return "", err
	} else if !exists {
		return emptyTrees[r.format], nil
	}

	entries, err := r.listIndex(ctx, "", sub)
	if err != nil {
		return "", err
	}
	untracked, err := r.listUntracked(ctx, "", sub)
	if err != nil {
		return "", err
	}
	relative := func(p string) (string, bool) {
		if sub == "" {
			return p, true
		}
		return strings.CutPrefix(p, sub+"/")
	}

	var info strings.Builder
	var candidates []string
	seen := map[string]bool{}
	for _, e := range entries {
		rel, ok := relative(e.path)
		if !ok || seen[e.path] {
			continue // unmerged paths repeat once per stage
		}
		seen[e.path] = true
		if e.stage == "0" && (e.skipWorktree || e.mode == modeGitlink) {
			fmt.Fprintf(&info, "%s %s\t%s\x00", e.mode, e.oid, rel)
			continue
		}
		candidates = append(candidates, e.path)
	}
	for _, p := range untracked {
		if _, ok := relative(p); ok {
			candidates = append(candidates, p)
		}
	}
	states, err := r.hashWorktree(ctx, candidates, true)
	if err != nil {
		return "", err
	}
	for _, p := range candidates {
		if st, ok := states[p]; ok {
			rel, _ := relative(p)
			fmt.Fprintf(&info, "%s %s\t%s\x00", st.mode, st.oid, rel)
		}
	}

	tmp, err := r.tempDir()
	if err != nil {
		return "", err
	}
	defer r.gitRoot.RemoveAll(tmp)
	if err := r.verify(); err != nil {
		return "", err
	}
	return r.writeTree(ctx, r.indexArg(tmp), info.String())
}

// stageWorktree updates the index copy at index to match the working tree
// and writes it as a tree. Entries whose mode and blob are unchanged are not
// rewritten, so they keep their cached stats.
func (r *repo) stageWorktree(ctx context.Context, index string) (string, map[string]fileState, error) {
	entries, err := r.listIndex(ctx, index, "")
	if err != nil {
		return "", nil, err
	}
	tracked := make(map[string]indexEntry, len(entries))
	var candidates []string
	for _, e := range entries {
		if e.stage != "0" {
			return "", nil, fmt.Errorf("%w (%s)", ErrUnmergedPaths, e.path)
		}
		// A sparse checkout keeps skip-worktree files out of the working
		// tree, and a submodule's commit is not ours to change.
		if e.skipWorktree || e.mode == modeGitlink {
			continue
		}
		tracked[e.path] = e
		candidates = append(candidates, e.path)
	}
	untracked, err := r.listUntracked(ctx, index, "")
	if err != nil {
		return "", nil, err
	}
	candidates = append(candidates, untracked...)
	if err := r.verify(); err != nil {
		return "", nil, err
	}
	states, err := r.hashWorktree(ctx, candidates, true)
	if err != nil {
		return "", nil, err
	}

	var info strings.Builder
	for _, p := range candidates {
		st, present := states[p]
		e, isTracked := tracked[p]
		switch {
		case present && (!isTracked || st.mode != e.mode || st.oid != e.oid):
			fmt.Fprintf(&info, "%s %s\t%s\x00", st.mode, st.oid, p)
		case !present && isTracked:
			fmt.Fprintf(&info, "0 %s\t%s\x00", r.nullOID(), p)
		}
	}
	tree, err := r.writeTree(ctx, index, info.String())
	return tree, states, err
}

// writeTree applies update-index --index-info lines to an index, then writes
// the index as a tree. update-index only records the given modes and IDs;
// write-tree only reads the index. Neither reads the working tree.
func (r *repo) writeTree(ctx context.Context, index, info string) (string, error) {
	if info != "" {
		if _, err := r.git(ctx, call{index: index, stdin: strings.NewReader(info)},
			"update-index", "-z", "--index-info"); err != nil {
			return "", err
		}
	}
	out, err := r.git(ctx, call{index: index}, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// identity reads the learner's user.name and user.email and returns them, with
// the time, as the commit-tree environment.
func (r *repo) identity(ctx context.Context, when time.Time) ([]string, error) {
	name, err := r.learnerConfig(ctx, "user.name", false)
	if err != nil {
		return nil, err
	}
	email, err := r.learnerConfig(ctx, "user.email", false)
	if err != nil {
		return nil, err
	}
	if name == "" || email == "" {
		return nil, ErrNoIdentity
	}
	date := fmt.Sprintf("@%d %s", when.Unix(), when.Format("-0700"))
	return []string{
		"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email, "GIT_COMMITTER_DATE=" + date,
	}, nil
}

// checkState refuses while another git operation is unfinished or HEAD is not
// on a branch, and returns the branch HEAD points at, such as
// refs/heads/main.
func (r *repo) checkState(ctx context.Context) (string, error) {
	for _, s := range []struct {
		name string
		err  error
	}{
		{"MERGE_HEAD", ErrMergeInProgress},
		{"rebase-merge", ErrRebaseInProgress},
		{"rebase-apply", ErrRebaseInProgress},
		{"CHERRY_PICK_HEAD", ErrCherryPickInProgress},
		{"REVERT_HEAD", ErrRevertInProgress},
	} {
		if _, err := r.gitRoot.Lstat(s.name); err == nil {
			return "", s.err
		}
	}
	out, err := r.git(ctx, call{readOnly: true}, "symbolic-ref", "-q", "HEAD")
	if exitCode(err) == 1 {
		return "", ErrDetachedHead
	}
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if !strings.HasPrefix(branch, "refs/heads/") || !filepath.IsLocal(branch) {
		return "", fmt.Errorf("%w (HEAD points at %s)", ErrDetachedHead, branch)
	}
	return branch, nil
}

// head returns HEAD's commit and tree. Before the first commit, the commit is
// "" and the tree is the empty tree.
func (r *repo) head(ctx context.Context) (commit, tree string, err error) {
	out, err := r.git(ctx, call{readOnly: true}, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if exitCode(err) == 1 {
		return "", emptyTrees[r.format], nil
	}
	if err != nil {
		return "", "", err
	}
	commit = strings.TrimSpace(out)
	out, err = r.git(ctx, call{readOnly: true}, "rev-parse", "-q", "--verify", commit+"^{tree}")
	if err != nil {
		return "", "", err
	}
	return commit, strings.TrimSpace(out), nil
}

// verifyRef checks, through the pinned .git, that the branch now names
// commit: that update-ref wrote to this repository and not to one swapped in
// under its name. A repository that keeps refs in a reftable has no loose
// ref to read; there only the pinning protects the write.
func (r *repo) verifyRef(branch, commit string) error {
	data, err := r.gitRoot.ReadFile(branch)
	if missing(err) {
		if _, rerr := r.gitRoot.Lstat("reftable"); rerr == nil {
			return nil
		}
	}
	if err != nil || strings.TrimSpace(string(data)) != commit {
		return fmt.Errorf("%w: the branch did not move in %s", ErrRepositoryChanged, r.gitDir)
	}
	return nil
}

// refLocked reports whether git could not take a ref's lock file.
func refLocked(err error) bool {
	var g *gitError
	return errors.As(err, &g) && strings.Contains(g.stderr, "cannot lock ref")
}

// largeFiles lists files added or changed between two trees whose size is at
// least threshold. --raw output needs no blob contents, and the flags rule out
// textconv and external diff drivers in any case.
func (r *repo) largeFiles(ctx context.Context, from, to string, states map[string]fileState, threshold int64) ([]LargeFile, error) {
	if threshold < 0 {
		return nil, nil
	}
	out, err := r.git(ctx, call{readOnly: true}, "diff-tree", "-r", "-z", "--raw",
		"--no-renames", "--no-textconv", "--no-ext-diff", "--diff-filter=AMT", from, to)
	if err != nil {
		return nil, err
	}
	fields := splitNUL(out)
	var large []LargeFile
	for i := 1; i < len(fields); i += 2 {
		st, ok := states[fields[i]]
		if ok && st.mode != modeSymlink && st.size >= threshold {
			large = append(large, LargeFile{Path: fields[i], Size: st.size})
		}
	}
	sort.Slice(large, func(i, j int) bool { return large[i].Path < large[j].Path })
	return large, nil
}

// folderExists reports whether sub is a real folder in the Topic. It is
// false when sub or a parent is missing, and an error when one is a file or
// a symbolic link.
func (r *repo) folderExists(sub string) (bool, error) {
	if sub == "" {
		return true, nil
	}
	parts := strings.Split(sub, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		info, err := r.root.Lstat(p)
		if missing(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, fmt.Errorf("%w: %s is not a folder", ErrInvalidPath, p)
		}
	}
	return true, nil
}

// cleanSubpath validates a folder path relative to the Topic and returns it
// in git's slash form, or "" for the whole Topic.
func cleanSubpath(p string) (string, error) {
	if p == "" || p == "." {
		return "", nil
	}
	clean := path.Clean(filepath.ToSlash(p))
	first, _, _ := strings.Cut(clean, "/")
	// EqualFold: on a case-insensitive file system .GIT is the same folder.
	if !filepath.IsLocal(clean) || strings.EqualFold(first, ".git") {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}
	return clean, nil
}

// message renders the commit message with its role prefix.
func message(role Role, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "Checkpoint"
	}
	return "[" + string(role) + "] " + msg + "\n"
}

// lockIndex takes git's index.lock in the pinned .git the way git does
// (exclusive create, which never follows a symbolic link), retrying with
// backoff while another process holds it. The returned function releases it.
func (r *repo) lockIndex(ctx context.Context, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	wait := 20 * time.Millisecond
	for {
		f, err := r.gitRoot.OpenFile("index.lock", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			f.Close()
			return func() { r.gitRoot.Remove("index.lock") }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("%w after waiting %s", ErrIndexLocked, timeout)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		wait = min(wait*2, 200*time.Millisecond)
	}
}

// tempDir makes a private folder in the pinned .git for a temporary index.
func (r *repo) tempDir() (string, error) {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.
	name := "lamplight-" + hex.EncodeToString(b)
	if err := r.gitRoot.Mkdir(name, 0o700); err != nil {
		return "", err
	}
	return name, nil
}

// indexArg is the GIT_INDEX_FILE path of the temporary index in tmp.
func (r *repo) indexArg(tmp string) string {
	return r.gitDirArg() + "/" + tmp + "/index"
}

// swapIndex puts the staged index from tmp in place of the learner's index,
// keeping the learner's aside, and returns a function that puts it back.
func (r *repo) swapIndex(tmp string) (restore func() error, err error) {
	staged := tmp + "/index"
	if _, err := r.gitRoot.Lstat(staged); missing(err) {
		return func() error { return nil }, nil
	}
	backup := tmp + "/index.learner"
	info, err := r.gitRoot.Lstat("index")
	hadIndex := err == nil
	switch {
	case hadIndex && !info.Mode().IsRegular():
		return nil, fmt.Errorf("%w: .git/index is not a regular file", ErrRepositoryChanged)
	case hadIndex:
		// A hard link keeps the learner's index without copying it; some
		// file systems have none.
		if err := r.gitRoot.Link("index", backup); err != nil {
			if err := copyIndex(r.gitRoot, "index", backup); err != nil {
				return nil, err
			}
		}
	case !missing(err):
		return nil, err
	}
	if err := r.gitRoot.Rename(staged, "index"); err != nil {
		return nil, err
	}
	return func() error {
		if hadIndex {
			return r.gitRoot.Rename(backup, "index")
		}
		return r.gitRoot.Remove("index")
	}, nil
}

// copyIndex copies an index file inside root, keeping its modification time
// so git's check for racily clean entries still works. A missing index is
// left missing; a symbolic link is refused rather than followed.
func copyIndex(root *os.Root, src, dst string) error {
	info, err := root.Lstat(src)
	if missing(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the index: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: .git/%s is not a regular file", ErrRepositoryChanged, src)
	}
	in, err := root.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("read the index: %w", err)
	}
	defer in.Close()
	if now, err := in.Stat(); err != nil || !os.SameFile(info, now) {
		return fmt.Errorf("read the index: .git/%s %w", src, errChanged)
	}
	out, err := root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return root.Chtimes(dst, info.ModTime(), info.ModTime())
}
