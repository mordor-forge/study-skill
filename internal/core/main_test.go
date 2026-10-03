package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain clears the caller's GIT_* variables. A commit hook that runs the
// tests sets GIT_INDEX_FILE, and GIT_DIR from a linked worktree: the git
// commands the tests run themselves would then work on the repository being
// committed. The core drops these variables on its own, and the tests of
// that set them again.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

// A test that reads a Topic's commits with plain git runs the same under a
// caller's git environment and writes nothing through it.
func TestTheTestsIgnoreTheCallersGitEnvironment(t *testing.T) {
	stray := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCheckpoint$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(stray, "repo.git"),
		"GIT_WORK_TREE="+filepath.Join(stray, "tree"), "GIT_INDEX_FILE="+filepath.Join(stray, "index"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("TestCheckpoint under a caller's git environment: %v\n%s", err, out)
	}
	if left, _ := os.ReadDir(stray); len(left) != 0 {
		t.Errorf("git wrote where the caller's environment pointed: %v", left)
	}
}
