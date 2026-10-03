//go:build linux

package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// buildStudy builds the study binary for tests that run it on a terminal,
// without a version stamp: the build must not need git to report on the
// checkout, which it refuses to in one owned by another user.
func buildStudy(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the study binary")
	}
	bin := filepath.Join(t.TempDir(), "study")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", bin, "github.com/mordor-forge/lamplight/v2/cmd/study").CombinedOutput(); err != nil {
		t.Fatalf("building study: %v\n%s", err, out)
	}
	return bin
}

// terminal runs study on a pseudo-terminal and plays the learner: it waits
// for what study prints, then types.
type terminal struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	out    bytes.Buffer
	seen   int
}

func startOnTerminal(t *testing.T, bin, home string, args ...string) *terminal {
	t.Helper()
	master, slave := openPTY(t)
	term := &terminal{t: t, master: master, slave: slave}
	term.cmd = exec.CommandContext(t.Context(), bin, args...)
	term.cmd.Env = append(os.Environ(), "STUDY_HOME="+home, "HOME="+home, "NO_COLOR=1", "TERM=dumb")
	term.cmd.Stdin, term.cmd.Stdout, term.cmd.Stderr = slave, slave, slave
	if err := term.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.cmd.Process.Kill() })
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := master.Read(buf)
			term.mu.Lock()
			term.out.Write(buf[:n])
			term.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return term
}

// expect waits until study prints want, after what earlier calls saw.
func (term *terminal) expect(want string) {
	term.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		term.mu.Lock()
		out := term.out.String()
		term.mu.Unlock()
		if i := strings.Index(out[term.seen:], want); i >= 0 {
			term.seen += i + len(want)
			// study reads only after it has printed the prompt, and waiting
			// a moment more lets it switch the terminal to single keys.
			time.Sleep(100 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	term.t.Fatalf("study never printed %q; it printed:\n%s", want, term.out.String())
}

func (term *terminal) send(keys string) {
	term.t.Helper()
	if _, err := term.master.Write([]byte(keys)); err != nil {
		term.t.Fatal(err)
	}
}

// wait waits for study to exit and returns its exit code.
func (term *terminal) wait() int {
	term.t.Helper()
	done := make(chan error, 1)
	go func() { done <- term.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		term.t.Fatalf("study did not exit")
	}
	return term.cmd.ProcessState.ExitCode()
}

func (term *terminal) output() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.out.String()
}

// cooked reports whether the terminal is back in its normal mode, which
// echoes and edits lines.
func (term *terminal) cooked() bool {
	tio, err := unix.IoctlGetTermios(int(term.slave.Fd()), unix.TCGETS)
	if err != nil {
		term.t.Fatal(err)
	}
	return tio.Lflag&unix.ICANON != 0 && tio.Lflag&unix.ECHO != 0
}

// TestTypingAtTheTerminalNeverLosesWork plays a learner who types freely:
// a whole sentence at the recall prompt, keys typed ahead of a question,
// and an arrow key. None of it may drop, edit or rate a Card.
func TestTypingAtTheTerminalNeverLosesWork(t *testing.T) {
	bin := buildStudy(t)
	home := withTopic(t)
	first := addCard(t, home, "What does a pivot column hold?", "A leading 1")
	second := addCard(t, home, "What is a free variable?", "One whose column has no pivot")

	term := startOnTerminal(t, bin, home, "review", "linear-algebra")
	term.expect("Recall the answer")
	// A sentence with s, f, d, e and q in it, as a learner types an answer.
	term.send("it is the leading one of a row, used for elimination and quickly found\r")
	term.expect("New Card:")
	// d, then a key typed ahead of the confirmation, which must be discarded.
	term.send("dy")
	term.expect("Drop this new Card for good? y/N")
	term.send("n")
	term.expect("Kept for now.")
	term.expect("New Card:")
	term.send("k")
	term.expect("How well did you recall it?")
	term.send("\x1b[A") // an arrow key, ignored without reprinting the question
	term.send("3")
	term.expect("Good.")
	term.expect("Recall the answer")
	term.send("q\r")
	term.expect("Stopped. 1 Review recorded.")
	if code := term.wait(); code != 0 {
		t.Errorf("exit %d\n%s", code, term.output())
	}
	if n := strings.Count(term.output(), "How well did you recall it?"); n != 1 {
		t.Errorf("the rating question was printed %d times:\n%s", n, term.output())
	}
	if !strings.Contains(term.output(), "you: it is the leading one") {
		t.Errorf("the typed answer was not shown beside the answer:\n%s", term.output())
	}
	cards := listCards(t, home)
	if len(cards) != 2 || cards[0].ID != first || cards[0].Draft || cards[1].ID != second || !cards[1].Draft ||
		cards[0].Prompt != "What does a pivot column hold?" {
		t.Errorf("after the session: %+v, want the first Card kept unchanged and rated, the second untouched", cards)
	}
	if !term.cooked() {
		t.Error("the terminal was left in raw mode")
	}
}

// TestSIGTERMStopsAReviewAndRestoresTheTerminal sends SIGTERM while study
// review waits for a single key, with the terminal in raw mode: it must
// stop like q, keep what was recorded, and leave the terminal as it was.
func TestSIGTERMStopsAReviewAndRestoresTheTerminal(t *testing.T) {
	bin := buildStudy(t)
	home := withTopic(t)
	addCard(t, home, "What does a pivot column hold?", "A leading 1")
	addCard(t, home, "What is a free variable?", "One whose column has no pivot")

	term := startOnTerminal(t, bin, home, "review", "linear-algebra")
	if !term.cooked() {
		t.Fatal("the pseudo-terminal does not start in its normal mode")
	}
	term.expect("Recall the answer")
	term.send("\r")
	term.expect("New Card:")
	term.send("k")
	term.expect("How well did you recall it?")
	term.send("3")
	term.expect("Recall the answer")
	term.send("\r")
	term.expect("New Card:") // waiting for a single key, in raw mode
	if err := term.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := term.wait(); code != 0 {
		t.Errorf("exit %d after SIGTERM\n%s", code, term.output())
	}
	if !strings.Contains(term.output(), "Stopped. 1 Review recorded.") {
		t.Errorf("study did not report what it kept:\n%s", term.output())
	}
	if !term.cooked() {
		t.Error("SIGTERM left the terminal in raw mode")
	}
	if cards := listCards(t, home); cards[0].Draft || !cards[1].Draft {
		t.Errorf("after SIGTERM: %+v, want the first Review kept and the second Card untouched", cards)
	}
}
