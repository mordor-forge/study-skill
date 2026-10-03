package checkpoint_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/checkpoint"
)

func TestTakeCommitsFirstCheckpointThenSkipsWhenUnchanged(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "lessons/intro.md", "# Intro\n")
	write(t, dir, "practice/l1/main.py", "print('hi')\n")

	first := take(t, dir, checkpoint.Agent, "Write the first lesson")
	if !first.Committed || first.Commit == "" {
		t.Fatalf("first Checkpoint: %+v, want a commit", first)
	}
	if head := git(t, dir, "rev-parse", "HEAD"); head != first.Commit {
		t.Fatalf("HEAD = %s, want %s", head, first.Commit)
	}
	if parents := git(t, dir, "rev-list", "--parents", "-n1", "HEAD"); strings.Contains(parents, " ") {
		t.Fatalf("first Checkpoint has a parent: %s", parents)
	}
	got := files(t, dir, "HEAD")
	if want := []string{"lessons/intro.md", "practice/l1/main.py"}; !slices.Equal(paths(got), want) {
		t.Fatalf("files = %v, want %v", paths(got), want)
	}
	if b := blob(t, dir, got["practice/l1/main.py"].oid); b != "print('hi')\n" {
		t.Fatalf("blob = %q", b)
	}

	second := take(t, dir, checkpoint.Learner, "Nothing new")
	if second.Committed {
		t.Fatalf("unchanged Topic made a commit: %+v", second)
	}
	if second.Commit != first.Commit || second.Tree != first.Tree {
		t.Fatalf("skipped Checkpoint = %+v, want HEAD %s", second, first.Commit)
	}
	if n := git(t, dir, "rev-list", "--count", "HEAD"); n != "1" {
		t.Fatalf("commits = %s, want 1", n)
	}
}

// Told which folder the caller opened, Take commits in that folder only: not
// in one that has its name by the time the Checkpoint starts.
func TestTakeCommitsOnlyInTheFolderItWasAskedFor(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, dir, "lessons/intro.md", "# Intro\n")
	other, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		res, err := checkpoint.Take(ctx, dir, checkpoint.Options{Role: checkpoint.Learner, Time: when, Folder: other, DryRun: dryRun})
		if !errors.Is(err, checkpoint.ErrRepositoryChanged) {
			t.Fatalf("Take for another folder (dry run %v) = %+v, %v; want ErrRepositoryChanged", dryRun, res, err)
		}
	}
	if out := gitMayFail(dir, "rev-parse", "--verify", "-q", "HEAD"); out != "" {
		t.Fatalf("the refused Checkpoint committed %s", out)
	}

	asked, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := checkpoint.Take(ctx, dir, checkpoint.Options{Role: checkpoint.Learner, Time: when, Folder: asked})
	if err != nil || !res.Committed {
		t.Fatalf("Take for the folder itself = %+v, %v; want a commit", res, err)
	}
}

func TestTakeDryRunWritesNothingToGit(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "notes.md", "first")
	first := take(t, dir, checkpoint.Learner, "")
	write(t, dir, "notes.md", "second")
	write(t, dir, "data.bin", strings.Repeat("x", 2048))
	before := gitFolder(t, dir)

	dry := checkpoint.Options{Role: checkpoint.Agent, Time: when, DryRun: true, LargeFileThreshold: 1024}
	res, err := checkpoint.Take(context.Background(), dir, dry)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !res.Committed || res.Commit != "" || res.Tree != "" {
		t.Errorf("dry run with changes = %+v, want a would-commit result with no commit", res)
	}
	if len(res.LargeFiles) != 1 || res.LargeFiles[0] != (checkpoint.LargeFile{Path: "data.bin", Size: 2048}) {
		t.Errorf("large files = %+v, want data.bin", res.LargeFiles)
	}
	if after := gitFolder(t, dir); !maps.Equal(before, after) {
		t.Errorf("a dry run changed .git:\nbefore %v\nafter  %v", before, after)
	}

	// A dry run reads the index as it is, without waiting for its lock.
	write(t, dir, ".git/index.lock", "")
	start := time.Now()
	res, err = checkpoint.Take(context.Background(), dir, dry)
	if err != nil || !res.Committed || time.Since(start) > 2*time.Second {
		t.Errorf("dry run while the index is locked = %+v, %v after %s", res, err, time.Since(start))
	}
	if err := os.Remove(filepath.Join(dir, ".git", "index.lock")); err != nil {
		t.Errorf("the other process's index.lock is gone: %v", err)
	}

	write(t, dir, "notes.md", "first")
	if err := os.Remove(filepath.Join(dir, "data.bin")); err != nil {
		t.Fatal(err)
	}
	res, err = checkpoint.Take(context.Background(), dir, dry)
	if err != nil || res.Committed || res.Commit != first.Commit {
		t.Errorf("dry run without changes = %+v, %v; want a skip at HEAD", res, err)
	}
	// The dry run agrees with the Checkpoint it previews.
	write(t, dir, "notes.md", "third")
	preview, err := checkpoint.Take(context.Background(), dir, dry)
	if err != nil {
		t.Fatal(err)
	}
	if real := take(t, dir, checkpoint.Agent, ""); preview.Committed != real.Committed {
		t.Errorf("dry run said committed=%v, the Checkpoint committed=%v", preview.Committed, real.Committed)
	}
}

// gitFolder records every file and folder in dir/.git with its size and
// modification time.
func gitFolder(t *testing.T, dir string) map[string]string {
	t.Helper()
	seen := map[string]string{}
	root := filepath.Join(dir, ".git")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		seen[rel] = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return seen
}

func TestTakeSkipsAnEmptyTopic(t *testing.T) {
	dir := newRepo(t)
	res := take(t, dir, checkpoint.Learner, "")
	if res.Committed || res.Commit != "" {
		t.Fatalf("empty Topic: %+v, want no commit", res)
	}
	if out := gitMayFail(dir, "rev-parse", "-q", "--verify", "HEAD"); strings.TrimSpace(out) != "" {
		t.Fatalf("HEAD exists after an empty Checkpoint: %s", out)
	}
}

func TestTakeRecordsModificationsDeletionsAndNewFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "notes/a.md", "a\n")
	write(t, dir, "notes/b.md", "b\n")
	write(t, dir, "practice/l1/old.py", "old\n")
	first := take(t, dir, checkpoint.Agent, "Start")

	write(t, dir, "notes/a.md", "a, edited\n")
	if err := os.Remove(filepath.Join(dir, "notes", "b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "practice", "l1")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "practice/l2/new.py", "new\n")
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	second := take(t, dir, checkpoint.Learner, "My work")
	if !second.Committed {
		t.Fatal("changes made no commit")
	}
	if parent := git(t, dir, "rev-parse", "HEAD^"); parent != first.Commit {
		t.Fatalf("parent = %s, want %s", parent, first.Commit)
	}
	got := files(t, dir, "HEAD")
	if want := []string{"notes/a.md", "practice/l2/new.py", "run.sh"}; !slices.Equal(paths(got), want) {
		t.Fatalf("files = %v, want %v", paths(got), want)
	}
	if b := blob(t, dir, got["notes/a.md"].oid); b != "a, edited\n" {
		t.Fatalf("notes/a.md = %q", b)
	}
	if got["run.sh"].mode != "100755" || got["notes/a.md"].mode != "100644" {
		t.Fatalf("modes: run.sh %s, notes/a.md %s", got["run.sh"].mode, got["notes/a.md"].mode)
	}
}

func TestTakeHandlesUnusualNamesAndFolderFileSwaps(t *testing.T) {
	dir := newRepo(t)
	odd := []string{"notes/with space.md", "notes/ünïcödé.md", "notes/tab\there.md", "notes/new\nline.md", "notes/\"quoted\".md"}
	for _, p := range odd {
		write(t, dir, p, p+"\n")
	}
	write(t, dir, "becomes-folder", "file\n")
	write(t, dir, "becomes-file/inner.txt", "inner\n")
	take(t, dir, checkpoint.Learner, "")

	for _, p := range []string{"becomes-folder", "becomes-file"} {
		if err := os.RemoveAll(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, dir, "becomes-folder/inner.txt", "now a folder\n")
	write(t, dir, "becomes-file", "now a file\n")
	take(t, dir, checkpoint.Learner, "")

	got := files(t, dir, "HEAD")
	want := append(slices.Clone(odd), "becomes-folder/inner.txt", "becomes-file")
	slices.Sort(want)
	if !slices.Equal(paths(got), want) {
		t.Fatalf("files = %q, want %q", paths(got), want)
	}
	for _, p := range odd {
		if b := blob(t, dir, got[p].oid); b != p+"\n" {
			t.Errorf("%q = %q", p, b)
		}
	}
	if status := git(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("git status:\n%s", status)
	}
}

func TestTakeKeepsSkipWorktreeAndSubmoduleEntries(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	write(t, dir, "sparse.md", "kept out of the working tree\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	submodule := strings.Repeat("ab", 20)
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+submodule+",vendor/lib")
	git(t, dir, "update-index", "--skip-worktree", "sparse.md")
	if err := os.Remove(filepath.Join(dir, "sparse.md")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.md", "edited\n")

	take(t, dir, checkpoint.Learner, "")
	got := files(t, dir, "HEAD")
	if want := []string{"a.md", "sparse.md", "vendor/lib"}; !slices.Equal(paths(got), want) {
		t.Fatalf("files = %v, want %v", paths(got), want)
	}
	if got["vendor/lib"] != (entry{mode: "160000", oid: submodule}) {
		t.Fatalf("submodule entry = %+v", got["vendor/lib"])
	}
	if b := blob(t, dir, got["sparse.md"].oid); b != "kept out of the working tree\n" {
		t.Fatalf("skip-worktree file = %q, want it kept", b)
	}
}

func TestTakeLeavesIgnoredFilesOut(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", checkpoint.DefaultGitignore())
	write(t, dir, "practice/l1/train.py", "train()\n")
	for _, p := range []string{
		"practice/l1/data.parquet", "practice/l1/warehouse.duckdb", "models/llama.gguf",
		"models/model.safetensors", "models/epoch1.pt", "models/best.pth",
		"practice/l1/__pycache__/train.cpython-312.pyc", ".venv/bin/python", ".env",
	} {
		write(t, dir, p, "artefact\n")
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("scratch.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "scratch.txt", "local only\n")

	take(t, dir, checkpoint.Learner, "Train")
	if want, got := []string{".gitignore", "practice/l1/train.py"}, paths(files(t, dir, "HEAD")); !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestTakeKeepsTrackedFilesThatAreNowIgnored(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "data.parquet", "v1\n")
	take(t, dir, checkpoint.Learner, "Add data")
	write(t, dir, ".gitignore", "*.parquet\n")
	write(t, dir, "data.parquet", "v2\n")

	take(t, dir, checkpoint.Learner, "Ignore data")
	got := files(t, dir, "HEAD")
	if b := blob(t, dir, got["data.parquet"].oid); b != "v2\n" {
		t.Fatalf("tracked, now ignored file = %q, want it still tracked at v2", b)
	}
}

func TestTakeHonoursTheLearnersGlobalExcludesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	write(t, home, ".gitignore_global", "*.secret\n")
	write(t, home, ".gitconfig", "[core]\n\texcludesFile = ~/.gitignore_global\n")

	dir := newRepo(t)
	write(t, dir, "keep.txt", "keep\n")
	write(t, dir, "api.secret", "token\n")
	take(t, dir, checkpoint.Learner, "")
	if got := paths(files(t, dir, "HEAD")); !slices.Equal(got, []string{"keep.txt"}) {
		t.Fatalf("files = %v, want the global excludes file honoured", got)
	}
}

func TestTakeRendersAuthorshipAndIdentity(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	take(t, dir, checkpoint.Agent, "  Teach lesson 2  ")
	if msg := git(t, dir, "log", "-1", "--format=%B"); msg != "[agent] Teach lesson 2" {
		t.Fatalf("message = %q", msg)
	}
	write(t, dir, "a.md", "b\n")
	take(t, dir, checkpoint.Learner, "")
	if msg := git(t, dir, "log", "-1", "--format=%B"); msg != "[learner] Checkpoint" {
		t.Fatalf("default message = %q", msg)
	}
	got := git(t, dir, "log", "-1", "--format=%an <%ae>|%cn <%ce>|%at %ai|%ct")
	want := "Ada Learner <ada@example.com>|Ada Learner <ada@example.com>|1790839800 2026-10-01 09:30:00 +0200|1790839800"
	if got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
	if sig := git(t, dir, "cat-file", "commit", "HEAD"); strings.Contains(sig, "gpgsig") {
		t.Fatal("Checkpoint was signed")
	}
}

func TestTakeRejectsAnUnknownRole(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	_, err := checkpoint.Take(context.Background(), dir, checkpoint.Options{Role: "teacher"})
	if !errors.Is(err, checkpoint.ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestTakeNeedsAnIdentity(t *testing.T) {
	if out := gitMayFail(t.TempDir(), "config", "--system", "--get", "user.name"); strings.TrimSpace(out) != "" {
		t.Skip("the system git configuration sets user.name")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.md", "a\n")
	_, err := checkpoint.Take(context.Background(), dir, checkpoint.Options{Role: checkpoint.Learner})
	if !errors.Is(err, checkpoint.ErrNoIdentity) {
		t.Fatalf("err = %v, want ErrNoIdentity", err)
	}
}

func TestTakeReportsLargeFilesItAddsOrChanges(t *testing.T) {
	dir := newRepo(t)
	big := strings.Repeat("x", 2048)
	write(t, dir, "data/big.csv", big)
	write(t, dir, "small.txt", "small\n")
	opts := checkpoint.Options{Role: checkpoint.Learner, Time: when, LargeFileThreshold: 1024}

	res, err := checkpoint.Take(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []checkpoint.LargeFile{{Path: "data/big.csv", Size: 2048}}
	if !slices.Equal(res.LargeFiles, want) {
		t.Fatalf("LargeFiles = %v, want %v", res.LargeFiles, want)
	}

	write(t, dir, "small.txt", "still small\n")
	res, err = checkpoint.Take(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed || len(res.LargeFiles) != 0 {
		t.Fatalf("unchanged large file reported again: %+v", res)
	}

	write(t, dir, "data/big.csv", big+"more")
	opts.LargeFileThreshold = -1
	res, err = checkpoint.Take(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed || len(res.LargeFiles) != 0 {
		t.Fatalf("negative threshold still reported: %+v", res)
	}
}

func TestTakeLeavesTheLearnersIndexMatchingTheCheckpoint(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "b.txt", "two\n")
	take(t, dir, checkpoint.Learner, "")

	// The learner stages half a change, then keeps editing.
	write(t, dir, "a.txt", "one, staged\n")
	git(t, dir, "add", "a.txt")
	write(t, dir, "a.txt", "one, staged, then edited\n")
	write(t, dir, "c.txt", "three\n")
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}

	take(t, dir, checkpoint.Agent, "")
	if status := git(t, dir, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("git status after a Checkpoint:\n%s", status)
	}
	if b := blob(t, dir, files(t, dir, "HEAD")["a.txt"].oid); b != "one, staged, then edited\n" {
		t.Fatalf("a.txt = %q, want the working tree's version", b)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index.lock left behind: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".git", "lamplight-*")); len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestTakeStoresSymlinksWithoutFollowingThem(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", "TOP SECRET\n")
	write(t, outside, "dir/inner.txt", "ALSO SECRET\n")

	dir := newRepo(t)
	write(t, dir, "notes/real.md", "real\n")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	link(filepath.Join(outside, "secret.txt"), "secret-link")
	link(filepath.Join(outside, "dir"), "dir-link")
	link("notes/real.md", "inside-link")

	take(t, dir, checkpoint.Learner, "")
	got := files(t, dir, "HEAD")
	if want := []string{"dir-link", "inside-link", "notes/real.md", "secret-link"}; !slices.Equal(paths(got), want) {
		t.Fatalf("files = %v, want %v", paths(got), want)
	}
	for name, target := range map[string]string{
		"secret-link": filepath.Join(outside, "secret.txt"),
		"dir-link":    filepath.Join(outside, "dir"),
		"inside-link": "notes/real.md",
	} {
		if got[name].mode != "120000" {
			t.Errorf("%s mode = %s, want 120000", name, got[name].mode)
		}
		if b := blob(t, dir, got[name].oid); b != target {
			t.Errorf("%s = %q, want the link target %q", name, b, target)
		}
	}

	// A tracked folder replaced by a link: its files count as deleted, and
	// nothing behind the link is read.
	git(t, dir, "rm", "-q", "--cached", "secret-link", "dir-link", "inside-link")
	for _, l := range []string{"secret-link", "dir-link", "inside-link"} {
		os.Remove(filepath.Join(dir, l))
	}
	write(t, dir, "practice/inner.txt", "mine\n")
	take(t, dir, checkpoint.Learner, "")
	if err := os.RemoveAll(filepath.Join(dir, "practice")); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(outside, "dir"), "practice")
	take(t, dir, checkpoint.Learner, "")
	got = files(t, dir, "HEAD")
	if want := []string{"notes/real.md", "practice"}; !slices.Equal(paths(got), want) {
		t.Fatalf("files = %v, want %v", paths(got), want)
	}
	if got["practice"].mode != "120000" {
		t.Fatalf("practice mode = %s, want a link", got["practice"].mode)
	}
}

func TestTakeLeavesNestedRepositoriesOut(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	nested := filepath.Join(dir, "practice", "vendored")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")
	write(t, nested, "lib.py", "lib\n")

	take(t, dir, checkpoint.Learner, "")
	if got := paths(files(t, dir, "HEAD")); !slices.Equal(got, []string{"a.md"}) {
		t.Fatalf("files = %v, want the nested repository left out", got)
	}
}

func TestTakeRefusesUnfinishedOperations(t *testing.T) {
	// conflicted builds a repository where branch "other" and main both
	// changed a.txt.
	conflicted := func(t *testing.T) string {
		dir := newRepo(t)
		write(t, dir, "a.txt", "base\n")
		git(t, dir, "add", "a.txt")
		git(t, dir, "commit", "-q", "-m", "base")
		git(t, dir, "checkout", "-q", "-b", "other")
		write(t, dir, "a.txt", "other\n")
		git(t, dir, "commit", "-q", "-am", "other")
		git(t, dir, "checkout", "-q", "main")
		write(t, dir, "a.txt", "main\n")
		git(t, dir, "commit", "-q", "-am", "main")
		return dir
	}
	cases := []struct {
		name  string
		setup func(t *testing.T) string
		want  error
	}{
		{"merge", func(t *testing.T) string {
			dir := conflicted(t)
			gitMayFail(dir, "merge", "-q", "other")
			return dir
		}, checkpoint.ErrMergeInProgress},
		{"rebase", func(t *testing.T) string {
			dir := conflicted(t)
			gitMayFail(dir, "rebase", "-q", "other")
			return dir
		}, checkpoint.ErrRebaseInProgress},
		{"cherry-pick", func(t *testing.T) string {
			dir := conflicted(t)
			gitMayFail(dir, "cherry-pick", "other")
			return dir
		}, checkpoint.ErrCherryPickInProgress},
		{"revert", func(t *testing.T) string {
			dir := conflicted(t)
			git(t, dir, "reset", "-q", "--hard", "other")
			write(t, dir, "a.txt", "changed again\n")
			git(t, dir, "commit", "-q", "-am", "again")
			gitMayFail(dir, "revert", "--no-edit", "HEAD~1")
			return dir
		}, checkpoint.ErrRevertInProgress},
		{"detached HEAD", func(t *testing.T) string {
			dir := conflicted(t)
			git(t, dir, "checkout", "-q", "--detach")
			write(t, dir, "new.txt", "new\n")
			return dir
		}, checkpoint.ErrDetachedHead},
		{"unresolved conflicts", func(t *testing.T) string {
			dir := conflicted(t)
			git(t, dir, "checkout", "-q", "other")
			write(t, dir, "a.txt", "stashed\n")
			git(t, dir, "stash", "-q")
			git(t, dir, "checkout", "-q", "main")
			gitMayFail(dir, "stash", "pop")
			return dir
		}, checkpoint.ErrUnmergedPaths},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := c.setup(t)
			head := gitMayFail(dir, "rev-parse", "HEAD")
			index := readIndex(t, dir)
			_, err := checkpoint.Take(context.Background(), dir, checkpoint.Options{Role: checkpoint.Learner, Time: when})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if now := gitMayFail(dir, "rev-parse", "HEAD"); now != head {
				t.Fatalf("HEAD moved from %s to %s", head, now)
			}
			if readIndex(t, dir) != index {
				t.Fatal("a refused Checkpoint changed the index")
			}
		})
	}
}

func TestTakeWaitsWhileAnotherProcessHoldsTheIndexLock(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		os.Remove(lock)
	}()
	start := time.Now()
	res, err := checkpoint.Take(context.Background(), dir, checkpoint.Options{
		Role: checkpoint.Learner, Time: when, LockTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !res.Committed {
		t.Fatal("no commit after the lock was released")
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Fatalf("Take returned after %s, before the lock was released", waited)
	}
}

func TestTakeGivesUpWhenTheIndexLockIsNeverReleased(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("editor"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := checkpoint.Take(context.Background(), dir, checkpoint.Options{
		Role: checkpoint.Learner, Time: when, LockTimeout: 200 * time.Millisecond,
	})
	if !errors.Is(err, checkpoint.ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
	if b, err := os.ReadFile(lock); err != nil || string(b) != "editor" {
		t.Fatalf("the other process's lock was disturbed: %q, %v", b, err)
	}
	if out := gitMayFail(dir, "rev-parse", "-q", "--verify", "HEAD"); strings.TrimSpace(out) != "" {
		t.Fatal("a commit was made without the lock")
	}
}

func TestTakeAndSnapshotNeedTheTopOfARepository(t *testing.T) {
	ctx := context.Background()
	opts := checkpoint.Options{Role: checkpoint.Learner, Time: when}
	other := newRepo(t)
	write(t, other, "theirs.txt", "theirs\n")
	git(t, other, "add", ".")
	git(t, other, "commit", "-q", "-m", "theirs")
	otherHead := git(t, other, "rev-parse", "HEAD")

	cases := map[string]func(t *testing.T) string{
		"no repository": func(t *testing.T) string { return t.TempDir() },
		"subfolder": func(t *testing.T) string {
			dir := newRepo(t)
			write(t, dir, "sub/a.md", "a\n")
			return filepath.Join(dir, "sub")
		},
		".git file": func(t *testing.T) string {
			dir := t.TempDir()
			write(t, dir, ".git", "gitdir: "+filepath.Join(other, ".git")+"\n")
			write(t, dir, "a.md", "a\n")
			return dir
		},
		".git symlink": func(t *testing.T) string {
			dir := t.TempDir()
			if err := os.Symlink(filepath.Join(other, ".git"), filepath.Join(dir, ".git")); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "a.md", "a\n")
			return dir
		},
		"commondir": func(t *testing.T) string {
			dir := newRepo(t)
			write(t, dir, ".git/commondir", filepath.Join(other, ".git")+"\n")
			write(t, dir, "a.md", "a\n")
			return dir
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := setup(t)
			if _, err := checkpoint.Take(ctx, dir, opts); !errors.Is(err, checkpoint.ErrNotRepository) {
				t.Errorf("Take err = %v, want ErrNotRepository", err)
			}
			if _, err := checkpoint.Snapshot(ctx, dir, ""); !errors.Is(err, checkpoint.ErrNotRepository) {
				t.Errorf("Snapshot err = %v, want ErrNotRepository", err)
			}
			if now := git(t, other, "rev-parse", "HEAD"); now != otherHead {
				t.Fatal("another repository's branch moved")
			}
		})
	}
}

func TestSnapshotHashesOneFolderAsTheWorkingTreeHoldsIt(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", checkpoint.DefaultGitignore())
	write(t, dir, "practice/l1/main.py", "print(1)\n")
	write(t, dir, "practice/l1/test_main.py", "assert True\n")
	write(t, dir, "practice/l2/main.py", "print(2)\n")
	write(t, dir, "notes/n.md", "note\n")
	take(t, dir, checkpoint.Learner, "")
	write(t, dir, "practice/l1/helper.py", "untracked\n")
	write(t, dir, "practice/l1/__pycache__/main.cpython-312.pyc", "ignored\n")

	headBefore := git(t, dir, "rev-parse", "HEAD")
	indexBefore := readIndex(t, dir)
	first := snapshot(t, dir, "practice/l1")
	if again := snapshot(t, dir, "practice/l1/"); again != first {
		t.Fatalf("same content hashed as %s then %s", first, again)
	}
	if git(t, dir, "rev-parse", "HEAD") != headBefore || readIndex(t, dir) != indexBefore {
		t.Fatal("Snapshot changed HEAD or the learner's index")
	}
	if got := paths(files(t, dir, first)); !slices.Equal(got, []string{"helper.py", "main.py", "test_main.py"}) {
		t.Fatalf("snapshot holds %v", got)
	}

	// Changes outside the folder, and to ignored files, do not matter.
	write(t, dir, "practice/l2/main.py", "print(22)\n")
	write(t, dir, "notes/n.md", "edited\n")
	write(t, dir, "practice/l1/__pycache__/main.cpython-312.pyc", "rebuilt\n")
	if got := snapshot(t, dir, "practice/l1"); got != first {
		t.Fatalf("changes outside the folder changed its hash: %s, want %s", got, first)
	}

	// A change inside it does, and reverting it restores the hash.
	write(t, dir, "practice/l1/main.py", "print(10)\n")
	changed := snapshot(t, dir, "practice/l1")
	if changed == first {
		t.Fatal("editing a file did not change the hash")
	}
	write(t, dir, "practice/l1/main.py", "print(1)\n")
	if got := snapshot(t, dir, "practice/l1"); got != first {
		t.Fatalf("reverted content hashed as %s, want %s", got, first)
	}

	// Deleting a tracked file changes it.
	if err := os.Remove(filepath.Join(dir, "practice", "l1", "test_main.py")); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, dir, "practice/l1"); got == first {
		t.Fatal("deleting a tracked file did not change the hash")
	}

	// The hash is the folder's tree in a Checkpoint taken now.
	write(t, dir, "practice/l1/main.py", "print(100)\n")
	want := snapshot(t, dir, "practice/l1")
	take(t, dir, checkpoint.Learner, "")
	if got := git(t, dir, "rev-parse", "HEAD:practice/l1"); got != want {
		t.Fatalf("Checkpoint holds practice/l1 as %s, Snapshot said %s", got, want)
	}
	if got := snapshot(t, dir, ""); got != git(t, dir, "rev-parse", "HEAD^{tree}") {
		t.Fatalf("whole-Topic Snapshot %s differs from the Checkpoint's tree", got)
	}
}

func TestSnapshotOfAMissingFolderIsTheEmptyTree(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.md", "a\n")
	got := snapshot(t, dir, "practice/not-started")
	if want := git(t, dir, "hash-object", "-t", "tree", "/dev/null"); got != want {
		t.Fatalf("missing folder = %s, want the empty tree %s", got, want)
	}
}

func TestSnapshotRejectsPathsOutsideTheTopic(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "notes.md", "a file, not a folder\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"../elsewhere", "/etc", ".git", ".git/objects", ".GIT/objects", "practice/../../x", "notes.md", "linked"} {
		if _, err := checkpoint.Snapshot(context.Background(), dir, sub); !errors.Is(err, checkpoint.ErrInvalidPath) {
			t.Errorf("Snapshot(%q) err = %v, want ErrInvalidPath", sub, err)
		}
	}
}

// TestHeldOutDataIsCommittedWhateverTheLinesAbove: a learner's line such as
// .* leaves out the .heldout folder, and git never looks inside a folder it
// leaves out; the default's last lines bring the folder and its files back.
func TestHeldOutDataIsCommittedWhateverTheLinesAbove(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", ".*\n!.gitignore\n"+checkpoint.DefaultGitignore())
	write(t, dir, ".heldout/answer/data.parquet", "PAR1")
	write(t, dir, "scratch.parquet", "PAR1")
	st := git(t, dir, "status", "--porcelain", "--ignored", "--untracked-files=all")
	if !strings.Contains(st, "?? .heldout/answer/data.parquet") || !strings.Contains(st, "!! scratch.parquet") {
		t.Errorf("git status:\n%s", st)
	}
}

func TestDefaultGitignoreCoversDataAndModelArtefacts(t *testing.T) {
	ignore := checkpoint.DefaultGitignore()
	for _, pattern := range []string{"*.parquet", "*.duckdb", "*.gguf", "*.safetensors", "*.pt", "*.pth", "__pycache__/"} {
		if !slices.Contains(strings.Split(ignore, "\n"), pattern) {
			t.Errorf("DefaultGitignore lacks %q", pattern)
		}
	}
}
