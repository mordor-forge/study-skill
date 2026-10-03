package core

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mordor-forge/lamplight/v2/internal/checkpoint"
)

const eventAttemptRecorded = "attempt.recorded"

// Attempt outcomes.
const (
	OutcomePassed  = "passed"
	OutcomeFailed  = "failed"
	OutcomeErrored = "errored"
)

// DefaultCheckTimeout bounds each criterion of a Check.
const DefaultCheckTimeout = 30 * time.Minute

// maxOutputBytes is how much of a criterion's output a Check keeps: the end,
// where failures are reported.
const maxOutputBytes = 16 << 10

// Attempt is one run of a Lesson's Check against the learner's work as it
// stood at that moment. Its ID is the ID of the Event that recorded it.
type Attempt struct {
	ID     string `json:"id"`
	Lesson string `json:"lesson"`
	// CheckVersion is the version of the Check that ran.
	CheckVersion string `json:"check_version"`
	// Snapshot is the hash of practice/<lesson-id>/ when the Check ran.
	Snapshot string            `json:"snapshot"`
	Outcome  string            `json:"outcome"`
	Criteria []CriterionResult `json:"criteria"`
	// Reason explains an errored Attempt that no criterion explains.
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// CriterionResult is the outcome of one run or held_out criterion in an
// Attempt.
type CriterionResult struct {
	ID string `json:"id"`
	// Kind is run or held_out; empty in Attempts recorded before held_out
	// criteria existed, which were all runs.
	Kind     string `json:"kind,omitempty"`
	Outcome  string `json:"outcome"`
	ExitCode int    `json:"exit_code"`
	// Reason explains an errored criterion.
	Reason string `json:"reason,omitempty"`
	// Results is what the criterion wrote to its results file, if anything.
	Results *CriterionScores `json:"results,omitempty"`
	// Counted, for a held_out criterion that produced results, says whether
	// this run is its counted measurement (see countHeldOut), and
	// NotCounted why not.
	Counted    *bool  `json:"counted,omitempty"`
	NotCounted string `json:"not_counted,omitempty"`
	// Output is the end of what a run criterion's command printed. It is
	// shown to the agent running the Check but never recorded in the
	// History, and never kept for held_out criteria, so Held-out data
	// cannot leak through it.
	Output string `json:"output,omitempty"`
}

// CriterionScores is what a criterion's command reported in its results
// file: whether it passed, a score out of a maximum, named metrics and a
// short summary. Every field is optional.
type CriterionScores struct {
	Passed  *bool              `json:"passed,omitempty"`
	Score   *float64           `json:"score,omitempty"`
	Max     *float64           `json:"max,omitempty"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
	Summary string             `json:"summary,omitempty"`
}

// kind is the criterion's kind, run for Attempts recorded before kinds.
func (r CriterionResult) kind() string {
	if r.Kind == "" {
		return CriterionRun
	}
	return r.Kind
}

// attemptRecordedData is the payload of an attempt.recorded Event.
type attemptRecordedData struct {
	Lesson       string            `json:"lesson"`
	CheckVersion string            `json:"check_version"`
	Snapshot     string            `json:"snapshot"`
	Outcome      string            `json:"outcome"`
	Criteria     []CriterionResult `json:"criteria"`
	Reason       string            `json:"reason,omitempty"`
}

// CheckOptions configures RunCheck.
type CheckOptions struct {
	// Timeout bounds each criterion. Zero means DefaultCheckTimeout.
	Timeout time.Duration
	// Progress, if set, is told when each criterion starts and ends, and
	// every ProgressEvery while one runs. It is never called concurrently.
	Progress func(CheckProgress)
	// ProgressEvery is how often a running criterion is reported. Zero
	// means DefaultProgressEvery.
	ProgressEvery time.Duration
}

// DefaultProgressEvery is how often a long criterion is reported as running.
const DefaultProgressEvery = 15 * time.Second

// Progress states of a criterion.
const (
	ProgressStarted  = "started"
	ProgressRunning  = "running"
	ProgressFinished = "finished"
)

// CheckProgress reports a criterion of a running Check.
type CheckProgress struct {
	Criterion string
	Kind      string
	// Index counts criteria from 1, out of Total that run.
	Index, Total int
	State        string
	// Elapsed is how long the criterion has run.
	Elapsed time.Duration
	// Outcome is set when the criterion finished.
	Outcome string
}

// RunCheck runs a Lesson's Check against the work in practice/<lesson-id>/
// and records the result as an Attempt. It runs commands the agent wrote, so
// it is reached only through the CLI, from the agent's own shell, where the
// agent's sandbox applies; the MCP server never runs a Check (ADR-0009).
//
// Each run and held_out criterion's command runs in the practice folder, in
// a process group of its own, with STUDY_TOPIC, STUDY_LESSON and
// STUDY_RESULTS set, and STUDY_HELDOUT_DIR for held_out criteria only. A
// command may write a results file (see CriterionScores) to STUDY_RESULTS;
// held_out criteria must. A run criterion passes when its command exits
// with 0, leaves nothing running and does not report passed: false. The
// Attempt's outcome comes from the run criteria alone: held_out results are
// diagnostic, and rubric items are graded with RecordRubricGrade. The work
// is snapshotted before and after (see checkpoint.SnapshotWork): work the
// snapshot cannot fully see, or that changed while the Check ran, makes the
// Attempt errored. When ctx is cancelled, as when study receives SIGTERM,
// every process the Check started is stopped and nothing is recorded.
func (c *Core) RunCheck(ctx context.Context, topicID, lessonID string, opts CheckOptions) (Attempt, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	s, dir, err := c.replayTopic(ctx, topicID)
	if err != nil {
		return Attempt{}, err
	}
	if _, err := requireStudiedLesson(s, topicID, lessonID); err != nil {
		return Attempt{}, err
	}
	// The folder stays open until the Attempt is recorded, in this folder or
	// not at all: a Topic removed or replaced while its Check runs must not
	// get the Attempt of work it does not hold.
	home, topic, err := c.openTopicFolder(topicID)
	if err != nil {
		return Attempt{}, err
	}
	defer home.Close()
	defer topic.Close()
	check, version, err := readCheck(topic, lessonID)
	if err != nil {
		return Attempt{}, err
	}
	var runnable []Criterion
	for _, crit := range check {
		if crit.Kind != CriterionRubric {
			runnable = append(runnable, crit)
		}
	}
	if len(runnable) == 0 {
		return Attempt{}, &Error{Code: CodeFailedPrecondition, Message: "the Check of " + lessonID +
			" has only rubric items, so there is nothing to run: grade each one with rubric_record"}
	}
	practice := practiceFolder(lessonID)
	workDir := filepath.Join(dir, filepath.FromSlash(practice))
	if info, err := os.Lstat(workDir); err != nil || !info.IsDir() {
		return Attempt{}, &Error{Code: CodeFailedPrecondition, Message: practice +
			"/ does not exist in " + topicID + ": the Lesson's exercise lives there"}
	}

	d := attemptRecordedData{Lesson: lessonID, CheckVersion: version, Outcome: OutcomePassed}
	before, err := checkpoint.SnapshotWork(ctx, dir, practice)
	switch {
	case blindSpot(err):
		// Nothing runs on work the snapshot cannot see whole.
		d.Outcome, d.Reason = OutcomeErrored, blindSpotReason(practice, err)
		return c.recordAttempt(ctx, home, topic, topicID, d, nil)
	case err != nil:
		return Attempt{}, checkpointError(topicID, dir, err)
	}
	d.Snapshot = before.Hash
	heldOutDir := filepath.Join(dir, heldOutFolder(lessonID))
	every := opts.ProgressEvery
	if every <= 0 {
		every = DefaultProgressEvery
	}
	// Run criteria go first, then held_out criteria, so a held_out command
	// that changes the work never changes what the run criteria judged.
	slices.SortStableFunc(runnable, func(a, b Criterion) int {
		return cmp.Compare(boolInt(a.Kind == CriterionHeldOut), boolInt(b.Kind == CriterionHeldOut))
	})
	env := checkEnv(topicID, lessonID)
	runs := 0
	var prev checkpoint.Work
	for i, crit := range runnable {
		if ctx.Err() != nil {
			break
		}
		report := progressReporter(opts.Progress, crit, i+1, len(runnable), every)
		if crit.Kind == CriterionRun {
			runs++
			stop := report.start()
			r := runCriterion(ctx, crit, workDir, timeout, env)
			stop()
			report.finish(r.Outcome)
			d.Outcome = worse(d.Outcome, r.Outcome)
			d.Criteria = append(d.Criteria, r)
			continue
		}
		if prev.Hash == "" {
			// The run criteria are done: the work must still be the work
			// they judged.
			prev, err = c.workAfter(ctx, topicID, dir, practice, before, &d)
			if err != nil {
				return Attempt{}, err
			}
		}
		var r CriterionResult
		if info, err := os.Stat(heldOutDir); err != nil || !info.IsDir() {
			r = CriterionResult{ID: crit.ID, Kind: crit.Kind, Outcome: OutcomeErrored, ExitCode: -1,
				Reason: "there is no Held-out data in " + heldOutFolder(lessonID) + "/: write it there before practicing starts"}
		} else {
			stop := report.start()
			r = runCriterion(ctx, crit, workDir, timeout, append(env, "STUDY_HELDOUT_DIR="+heldOutDir))
			stop()
			if ctx.Err() == nil {
				prev = c.heldOutChangedWork(ctx, dir, practice, prev, &r)
			}
		}
		report.finish(r.Outcome)
		d.Criteria = append(d.Criteria, r)
	}
	if ctx.Err() != nil {
		return Attempt{}, &Error{Code: CodeCanceled, Err: ctx.Err(), Message: "the Check of " + lessonID +
			" was stopped before it finished, and every program it started was stopped too; no Attempt was recorded"}
	}
	if prev.Hash == "" {
		if _, err := c.workAfter(ctx, topicID, dir, practice, before, &d); err != nil {
			return Attempt{}, err
		}
	}
	if runs == 0 && d.Outcome != OutcomeErrored {
		// A Check without run criteria (rubric items and held_out
		// criteria) never decides anything through an Attempt: its outcome
		// reports the held_out evaluations, and is never passed when one of
		// them failed or errored.
		for _, r := range d.Criteria {
			d.Outcome = worse(d.Outcome, r.Outcome)
		}
	}
	return c.recordAttempt(ctx, home, topic, topicID, d, d.Criteria)
}

// workAfter snapshots the work after the run criteria, and makes the Attempt
// errored when they changed it or left it with a blind spot.
func (c *Core) workAfter(ctx context.Context, topicID, dir, practice string, before checkpoint.Work,
	d *attemptRecordedData) (checkpoint.Work, error) {
	after, err := checkpoint.SnapshotWork(ctx, dir, practice)
	switch {
	case blindSpot(err):
		d.Outcome, d.Reason = OutcomeErrored, "while the Check ran, "+blindSpotReason(practice, err)
		return before, nil
	case err != nil:
		return checkpoint.Work{}, checkpointError(topicID, dir, err)
	case after.Hash != before.Hash:
		d.Outcome = OutcomeErrored
		d.Reason = fmt.Sprintf("the Check changed %s in %s/ while it ran; if a Check writes files, such as "+
			"build outputs, list them in %s/.gitignore, then run it again", strings.Join(after.Changed(before), ", "),
			practice, practice)
	}
	return after, nil
}

// heldOutChangedWork makes a held_out criterion errored when its command
// changed the work, so its run never counts, and returns the work as it is
// now. The run criteria already judged the work as it was, so the Attempt
// itself stands.
func (c *Core) heldOutChangedWork(ctx context.Context, dir, practice string, prev checkpoint.Work, r *CriterionResult) checkpoint.Work {
	now, err := checkpoint.SnapshotWork(ctx, dir, practice)
	switch {
	case err != nil:
		r.Outcome, r.Reason = OutcomeErrored, "the work could not be snapshotted after it ran: "+clip(err.Error(), 200)
		r.Results = nil
		return prev
	case now.Hash != prev.Hash:
		r.Outcome = OutcomeErrored
		r.Reason = fmt.Sprintf("it changed %s in %s/; a held_out command must not write into the work: write "+
			"elsewhere, such as $TMPDIR, or list those files in %s/.gitignore", clip(strings.Join(now.Changed(prev), ", "), 200),
			practice, practice)
		r.Results = nil
	}
	return now
}

// checkEnv is the environment every criterion's command gets: study's own,
// without any STUDY_ variable it inherited, plus the Topic and the Lesson.
func checkEnv(topicID, lessonID string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "STUDY_") {
			env = append(env, kv)
		}
	}
	return append(env, "STUDY_TOPIC="+topicID, "STUDY_LESSON="+lessonID)
}

// worse is the worse of two outcomes: errored, then failed, then passed.
func worse(a, b string) string {
	rank := map[string]int{OutcomePassed: 0, OutcomeFailed: 1, OutcomeErrored: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// recordAttempt records an Attempt in the Topic folder its Check was read
// from, and returns it with the output of each run criterion, which is shown
// but never recorded: it could reveal test data. Whether each held_out
// result is the counted measurement is decided under the Topic's lock, from
// the History as it stands.
func (c *Core) recordAttempt(ctx context.Context, home, topic *os.Root, topicID string, d attemptRecordedData,
	shown []CriterionResult) (Attempt, error) {
	d.Criteria = make([]CriterionResult, len(shown))
	for i, r := range shown {
		r.Output = ""
		d.Criteria[i] = r
	}
	ev, err := c.writeOpenedTopic(ctx, home, topic, topicID, func(s *replayed, _ *topicView) (*change, error) {
		ls := s.study.lessons[d.Lesson]
		for i := range d.Criteria {
			r := &d.Criteria[i]
			r.Counted, r.NotCounted = countHeldOut(ls, d, *r)
		}
		return &change{Type: eventAttemptRecorded, Data: d}, nil
	}, false)
	if err != nil {
		return Attempt{}, err
	}
	criteria := make([]CriterionResult, len(shown))
	for i, r := range shown {
		r.Counted, r.NotCounted = d.Criteria[i].Counted, d.Criteria[i].NotCounted
		if r.kind() == CriterionHeldOut {
			r.Output = ""
		}
		criteria[i] = r
	}
	return Attempt{ID: ev.ID, Lesson: d.Lesson, CheckVersion: d.CheckVersion, Snapshot: d.Snapshot, Outcome: d.Outcome,
		Criteria: criteria, Reason: d.Reason, At: ev.Wall}, nil
}

// Why a held_out run is not counted.
const (
	notCountedEarlier   = "an earlier run is the counted measurement"
	notCountedErrored   = "the Attempt errored"
	notCountedNotShown  = "the Check was not shown to the learner yet"
	notCountedOtherShow = "this version of the Check is not the one shown to the learner"
)

// countHeldOut decides whether a held_out result is its criterion's counted
// measurement, and why not when it is not. The same rule decides when an
// Attempt is recorded and when it is replayed: the run counts when it
// produced results without erroring, the Attempt as a whole did not error,
// the Check was shown to the learner and the run used the shown version,
// and no earlier run of the criterion in the Lesson counted. Results that
// are not a measurement, such as an errored run's, get no answer at all.
// The count is kept per Lesson and criterion id, so renaming a held_out
// criterion starts a new count; the History shows it.
func countHeldOut(ls *lessonState, d attemptRecordedData, r CriterionResult) (*bool, string) {
	if r.kind() != CriterionHeldOut || r.Results == nil || r.Outcome == OutcomeErrored {
		return nil, ""
	}
	no := false
	switch {
	case d.Outcome == OutcomeErrored:
		return &no, notCountedErrored
	case ls == nil || ls.shownCheck == "":
		return &no, notCountedNotShown
	case d.CheckVersion != ls.shownCheck:
		return &no, notCountedOtherShow
	case ls.heldOut[r.ID] != "":
		return &no, notCountedEarlier
	}
	yes := true
	return &yes, ""
}

func practiceFolder(lessonID string) string { return "practice/" + lessonID }

// heldOutFolder is where a Lesson's Held-out data lives in its Topic. It is
// committed with the Topic, so every machine measures on the same data, and
// it is synthetic or public, never personal data. Lamplight never shows its
// contents, or the raw output of a held_out command.
func heldOutFolder(lessonID string) string { return ".heldout/" + lessonID }

// progress reports one criterion of a running Check to CheckOptions.Progress.
type progress struct {
	report       func(CheckProgress)
	crit         Criterion
	index, total int
	every        time.Duration
	started      time.Time
}

func progressReporter(report func(CheckProgress), crit Criterion, index, total int, every time.Duration) *progress {
	return &progress{report: report, crit: crit, index: index, total: total, every: every}
}

func (p *progress) send(state, outcome string) {
	if p.report == nil {
		return
	}
	var elapsed time.Duration
	if !p.started.IsZero() {
		elapsed = time.Since(p.started)
	}
	p.report(CheckProgress{Criterion: p.crit.ID, Kind: p.crit.Kind, Index: p.index, Total: p.total,
		State: state, Elapsed: elapsed, Outcome: outcome})
}

// start reports the criterion started, then reports it running at every
// tick until the returned stop is called; stop waits for the last report.
func (p *progress) start() (stop func()) {
	p.started = time.Now()
	p.send(ProgressStarted, "")
	if p.report == nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(p.every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				p.send(ProgressRunning, "")
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

func (p *progress) finish(outcome string) { p.send(ProgressFinished, outcome) }

// blindSpot reports whether err is work a snapshot cannot see whole.
func blindSpot(err error) bool {
	for _, e := range []error{checkpoint.ErrHiddenEntries, checkpoint.ErrNestedRepository,
		checkpoint.ErrLinkOutside, checkpoint.ErrAllIgnored} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// blindSpotReason explains work a snapshot cannot see whole, and what to do.
func blindSpotReason(practice string, err error) string {
	switch {
	case errors.Is(err, checkpoint.ErrHiddenEntries):
		return fmt.Sprintf("%v; clear the flag with git update-index --no-skip-worktree --no-assume-unchanged", err)
	case errors.Is(err, checkpoint.ErrNestedRepository):
		return fmt.Sprintf("%v; a Check can only judge work whose every file the Topic's own repository sees, "+
			"so move the other repository out of %s/", err, practice)
	case errors.Is(err, checkpoint.ErrLinkOutside):
		return fmt.Sprintf("%v; copy what it points to into %s/ instead", err, practice)
	default:
		return fmt.Sprintf("every file in %s/ is ignored, so there is no work to judge; check its .gitignore", practice)
	}
}

// checkGrace is how long a Check's programs get to stop after SIGTERM before
// they are killed, and how long study waits for output from programs a
// finished criterion left running.
const checkGrace = 2 * time.Second

// runCriterion runs one criterion's command in a process group of its own.
// The command never inherits stdin, and its output is kept, bounded, for the
// agent to read. When it exits, anything left running in its group is
// killed, and the criterion is errored: a Check must finish what it starts.
// The command may write a results file to STUDY_RESULTS, in a folder of its
// own outside the practice folder, so writing it never changes the work.
func runCriterion(ctx context.Context, crit Criterion, dir string, timeout time.Duration, env []string) CriterionResult {
	r := CriterionResult{ID: crit.ID, Kind: crit.Kind}
	resultsDir, err := os.MkdirTemp("", "study-results-")
	if err != nil {
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1, "study could not make a folder for its results file: "+err.Error()
		return r
	}
	defer os.RemoveAll(resultsDir)
	resultsPath := filepath.Join(resultsDir, "results.json")

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	name := crit.Command[0]
	if strings.Contains(name, "/") && !filepath.IsAbs(name) {
		name = filepath.Join(dir, name) // a script in the practice folder
	}
	cmd := exec.CommandContext(ctx, name, crit.Command[1:]...)
	cmd.Dir = dir
	cmd.Env = append(slices.Clip(env), "STUDY_RESULTS="+resultsPath) // env holds study's own, without STUDY_ variables
	ownProcessGroup(cmd)
	out := &tailBuffer{max: maxOutputBytes}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	leftover := cmd.Process != nil && stopGroup(cmd.Process.Pid)
	r.Output = out.String()
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1, fmt.Sprintf("it ran for longer than %s", timeout)
		return r
	case ctx.Err() != nil:
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1, "it was stopped"
		return r
	case leftover || errors.Is(err, exec.ErrWaitDelay):
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1,
			"it left programs running in the background, which were stopped; a Check must finish everything it starts"
		return r
	case err == nil:
		r.Outcome = OutcomePassed
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		r.Outcome, r.ExitCode = OutcomeFailed, exit.ExitCode()
	case errors.As(err, &exit):
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1, "it was stopped by a signal: "+exit.String()
		return r
	default:
		r.Outcome, r.ExitCode, r.Reason = OutcomeErrored, -1, "it could not start: "+err.Error()
		return r
	}

	scores, present, problem := readResults(resultsPath)
	switch {
	case problem != "":
		r.Outcome, r.Reason = OutcomeErrored, "its results file is not valid: "+problem
	case !present && crit.Kind == CriterionHeldOut:
		r.Outcome, r.Reason = OutcomeErrored, "it wrote no results file: a held_out command writes its scores to "+
			"the file named by STUDY_RESULTS"
	case crit.Kind == CriterionHeldOut && scores.Score == nil:
		r.Outcome, r.Reason = OutcomeErrored, "its results file has no score: a held_out command's results file "+
			"needs one, such as {\"score\": 0.87}"
	case present:
		r.Results = scores
		if scores.Passed != nil && !*scores.Passed && r.Outcome == OutcomePassed {
			r.Outcome = OutcomeFailed
		}
	}
	return r
}

// Limits of a results file.
const (
	maxResultsBytes        = 64 << 10
	maxMetrics             = 20
	maxResultsSummaryRunes = 1000
	resultsFormatMax       = 1
)

var metricNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,39}$`)

// resultsFile is the JSON a criterion's command may write to STUDY_RESULTS:
//
//	{"format": 1, "passed": true, "score": 17, "max": 20,
//	 "metrics": {"precision": 0.91}, "summary": "17 of 20 cases"}
//
// Every field is optional; other fields are ignored and never recorded.
type resultsFile struct {
	Format  *int               `json:"format"`
	Passed  *bool              `json:"passed"`
	Score   *float64           `json:"score"`
	Max     *float64           `json:"max"`
	Metrics map[string]float64 `json:"metrics"`
	Summary *string            `json:"summary"`
}

// readResults reads and checks a results file. present is false when the
// command wrote none; problem explains an invalid one.
func readResults(path string) (scores *CriterionScores, present bool, problem string) {
	// The file is opened without following a link and without blocking on
	// a FIFO, checked on the open file, and read through a bound, so a file
	// swapped in after a check is never read whole.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, ""
	case errors.Is(err, syscall.ELOOP):
		return nil, true, "it is a link, not a regular file"
	case err != nil:
		return nil, true, "it cannot be opened: " + clip(err.Error(), maxEchoRunes)
	}
	defer f.Close()
	info, err := f.Stat()
	switch {
	case err != nil:
		return nil, true, "it cannot be read: " + clip(err.Error(), maxEchoRunes)
	case !info.Mode().IsRegular():
		return nil, true, "it is not a regular file"
	case info.Size() > maxResultsBytes:
		return nil, true, fmt.Sprintf("it is larger than %d KiB", maxResultsBytes>>10)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxResultsBytes+1))
	switch {
	case err != nil:
		return nil, true, "it cannot be read: " + clip(err.Error(), maxEchoRunes)
	case len(data) > maxResultsBytes:
		return nil, true, fmt.Sprintf("it is larger than %d KiB", maxResultsBytes>>10)
	case !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")):
		return nil, true, "it is not a JSON object"
	}
	var rf resultsFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, true, "it is not a JSON object with the documented fields: " + clip(err.Error(), maxEchoRunes)
	}
	return checkResults(rf)
}

// maxEchoRunes is how much text from a results file, such as a metric name
// or a JSON error, a reason may quote.
const maxEchoRunes = 80

// checkResults validates a decoded results file.
func checkResults(f resultsFile) (scores *CriterionScores, present bool, problem string) {
	switch {
	case f.Format != nil && *f.Format > resultsFormatMax:
		return nil, true, fmt.Sprintf("it has format %d, but this version of study understands format %d: upgrade study",
			*f.Format, resultsFormatMax)
	case f.Format != nil && *f.Format < 1:
		return nil, true, "its format must be 1"
	}
	s := &CriterionScores{Passed: f.Passed, Score: f.Score, Max: f.Max}
	if s.Score != nil {
		if s.Max == nil {
			one := 1.0
			s.Max = &one
		}
		if *s.Score < 0 {
			return nil, true, "its score is negative"
		}
	}
	if s.Max != nil {
		if *s.Max <= 0 {
			return nil, true, "its max must be more than 0"
		}
		if s.Score != nil && *s.Score > *s.Max {
			return nil, true, fmt.Sprintf("its score %g is more than its max %g", *s.Score, *s.Max)
		}
	}
	if len(f.Metrics) > maxMetrics {
		return nil, true, fmt.Sprintf("it has more than %d metrics", maxMetrics)
	}
	for name := range f.Metrics {
		if !metricNamePattern.MatchString(name) {
			return nil, true, fmt.Sprintf("the metric name %q is not a name of up to 40 lowercase letters, digits, "+
				"underscores, dots and hyphens, starting with a letter or digit", clip(name, maxEchoRunes))
		}
	}
	if len(f.Metrics) > 0 {
		s.Metrics = f.Metrics
	}
	if f.Summary != nil {
		summary, err := cleanTextBlock("summary", *f.Summary, maxResultsSummaryRunes)
		if err != nil {
			return nil, true, err.Error()
		}
		s.Summary = summary
	}
	return s, true, ""
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if extra := t.buf.Len() - t.max; extra > 0 {
		t.buf.Next(extra)
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	s := strings.ToValidUTF8(t.buf.String(), "")
	if t.truncated {
		s = "…" + s
	}
	return s
}

func replayAttemptRecorded(s *replayed, ev event) error {
	var d attemptRecordedData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return fmt.Errorf("its payload is unreadable: %v", err)
	}
	if err := d.validate(); err != nil {
		return err
	}
	l := s.study.lesson(d.Lesson)
	// Replay decides which held_out run counts (see countHeldOut). A run
	// that claimed the count when it was recorded but does not get it, as
	// when two machines each measured first, is flagged.
	for i := range d.Criteria {
		r := &d.Criteria[i]
		claimed := r.Counted != nil && *r.Counted
		r.Counted, r.NotCounted = countHeldOut(l, d, *r)
		switch {
		case r.Counted != nil && *r.Counted:
			if l.heldOut == nil {
				l.heldOut = map[string]string{}
			}
			l.heldOut[r.ID] = ev.ID
		case claimed && r.NotCounted == notCountedEarlier:
			s.flag(newFlag(FlagConflict, checkItem(d.Lesson), []string{l.heldOut[r.ID], ev.ID}, r.ID,
				fmt.Sprintf("held_out criterion %s of Lesson %s was measured first twice, by Attempts %s and %s, "+
					"probably on two machines: the first counts, and the other is recorded as not counted",
					r.ID, d.Lesson, l.heldOut[r.ID], ev.ID)))
		}
	}
	a := Attempt{ID: ev.ID, Lesson: d.Lesson, CheckVersion: d.CheckVersion,
		Snapshot: d.Snapshot, Outcome: d.Outcome, Criteria: d.Criteria, Reason: d.Reason, At: wallOf(ev)}
	l.attempts = append(l.attempts, a)
	if l.shownCheck != "" && a.CheckVersion == l.shownCheck && a.Outcome != OutcomeErrored && hasRunCriteria(a) {
		l.measured++
		if l.firstTry == nil {
			passed := a.Outcome == OutcomePassed
			l.firstTry = &passed
		}
	}
	// A run criterion that failed on the Check shown to the learner calls
	// for a Next step that names the fix before practicing resumes; a
	// passed Attempt settles it. The agent trying its Check before showing
	// it asks for nothing.
	switch {
	case failedOnRun(a) && l.shownCheck != "" && a.CheckVersion == l.shownCheck:
		l.fixPending = true
	case a.Outcome == OutcomePassed:
		l.fixPending = false
	}
	return nil
}

// validate checks an attempt.recorded payload read back from the History,
// which may come from a hand edit or another machine.
func (d *attemptRecordedData) validate() error {
	if err := validateEntityID("Lesson", d.Lesson); err != nil {
		return err
	}
	outcomes := map[string]bool{OutcomePassed: true, OutcomeFailed: true, OutcomeErrored: true}
	if !outcomes[d.Outcome] {
		return fmt.Errorf("its outcome %q is not passed, failed or errored", clip(d.Outcome, 40))
	}
	for i := range d.Criteria {
		r := &d.Criteria[i]
		if err := validateEntityID("criterion", r.ID); err != nil {
			return err
		}
		if !outcomes[r.Outcome] {
			return fmt.Errorf("criterion %s has the outcome %q", r.ID, clip(r.Outcome, 40))
		}
		if k := r.kind(); k != CriterionRun && k != CriterionHeldOut {
			return fmt.Errorf("criterion %s has the kind %q", r.ID, clip(k, 40))
		}
		if s := r.Results; s != nil && s.Score != nil && s.Max == nil {
			one := 1.0
			s.Max = &one
		}
	}
	return nil
}

// hasRunCriteria reports whether an Attempt ran any run criterion.
func hasRunCriteria(a Attempt) bool {
	for _, r := range a.Criteria {
		if r.kind() == CriterionRun {
			return true
		}
	}
	return false
}

// failedOnRun reports whether an Attempt failed because of a run criterion:
// a held_out result never asks for a fix.
func failedOnRun(a Attempt) bool {
	if a.Outcome != OutcomeFailed {
		return false
	}
	for _, r := range a.Criteria {
		if r.kind() == CriterionRun && r.Outcome == OutcomeFailed {
			return true
		}
	}
	return false
}

// clip shortens text that comes from outside, such as part of a results
// file, before it goes into a reason or the History.
func clip(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// CheckResults is a Lesson's Attempts and whether it can be completed now.
type CheckResults struct {
	Topic    string      `json:"topic"`
	Lesson   string      `json:"lesson"`
	Check    []Criterion `json:"check"`
	Attempts []Attempt   `json:"attempts"`
	// Rubric is each rubric item's latest grade, if any, in the order of
	// the Check.
	Rubric []RubricStatus `json:"rubric"`
	// HeldOut is each held_out criterion's counted measurement and latest
	// results, in the order of the Check. They never decide completion.
	HeldOut []HeldOutStatus `json:"held_out"`
	// CanComplete reports whether the completion rule holds for the
	// current Check and work; Reason says why not.
	CanComplete bool   `json:"can_complete"`
	Reason      string `json:"reason,omitempty"`
	Done        bool   `json:"done"`
	// Next says what to do now, when the Check calls for something: after
	// an Attempt that failed on a run criterion, give feedback, then go
	// back to practicing with a Next step naming the fix.
	Next *NextAction `json:"next,omitempty"`
}

// NextAction is something to do, with a code for programs and a text for
// people.
type NextAction struct {
	Code string `json:"code"`
	Text string `json:"text"`
	// Milestone is the Milestone to assess, for assess_milestone.
	Milestone *MilestoneRef `json:"milestone,omitempty"`
}

// Codes of a NextAction.
const (
	// NextFixStep: an Attempt failed on a run criterion; give feedback, then
	// move the Lesson to practicing with a Next step that names the fix.
	NextFixStep = "name_the_fix"
)

// RubricStatus is a rubric item and its latest grade.
type RubricStatus struct {
	Criterion string       `json:"criterion"`
	Rubric    string       `json:"rubric"`
	Grade     *RubricGrade `json:"grade,omitempty"`
	// Current reports whether the grade is for the Check shown to the
	// learner and the work as it is now, which completion needs.
	Current bool `json:"current"`
}

// HeldOutStatus is a held_out criterion's counted measurement, which stays
// as it was, and its latest results.
type HeldOutStatus struct {
	Criterion string `json:"criterion"`
	// Counted is the counted measurement: the first run that produced
	// results, and the Attempt that recorded it.
	Counted        *CriterionScores `json:"counted,omitempty"`
	CountedAttempt string           `json:"counted_attempt,omitempty"`
	// Latest is the latest run's results, when it is not the counted one;
	// it is recorded as not counted, and LatestNotCounted says why.
	Latest           *CriterionScores `json:"latest,omitempty"`
	LatestAttempt    string           `json:"latest_attempt,omitempty"`
	LatestNotCounted string           `json:"latest_not_counted,omitempty"`
}

// CheckResultsOf returns a Lesson's Attempts, oldest first, and whether the
// completion rule holds for the current Check and work.
func (c *Core) CheckResultsOf(ctx context.Context, topicID, lessonID string) (CheckResults, error) {
	s, dir, err := c.replayTopic(ctx, topicID)
	if err != nil {
		return CheckResults{}, err
	}
	if _, err := requireLesson(s, topicID, lessonID); err != nil {
		return CheckResults{}, err
	}
	res := CheckResults{Topic: topicID, Lesson: lessonID, Check: []Criterion{}, Attempts: []Attempt{},
		Rubric: []RubricStatus{}, HeldOut: []HeldOutStatus{}}
	ls := s.study.lessons[lessonID]
	if ls != nil {
		res.Attempts = append(res.Attempts, ls.attempts...)
		res.Done = ls.completed != nil
	}
	cur, err := c.currentWork(ctx, topicID, dir, lessonID, lookedAtPaths(ls)...)
	if err != nil {
		return CheckResults{}, err
	}
	res.Check = cur.check
	for _, crit := range cur.check {
		switch crit.Kind {
		case CriterionRubric:
			st := RubricStatus{Criterion: crit.ID, Rubric: crit.Rubric}
			if ls != nil && ls.grades[crit.ID] != nil {
				g := *ls.grades[crit.ID]
				st.Grade = &g
				st.Current = staleGrade(ls, crit.ID, cur) == ""
			}
			res.Rubric = append(res.Rubric, st)
		case CriterionHeldOut:
			res.HeldOut = append(res.HeldOut, heldOutStatus(ls, crit.ID))
		}
	}
	if _, err := completionRule(s, lessonID, cur); err != nil {
		res.Reason = err.Error()
	} else {
		res.CanComplete = !res.Done
		if res.Done {
			res.Reason = "Lesson " + lessonID + " is done"
		}
	}
	if ls != nil && !res.Done && ls.fixPending {
		res.Next = &NextAction{Code: NextFixStep, Text: "give the learner feedback on the failed Attempt, then move " +
			"the Lesson to practicing with phase_set and a Next step that names the fix"}
	}
	return res, nil
}

// heldOutStatus finds a held_out criterion's counted measurement and its
// latest results in a Lesson's Attempts.
func heldOutStatus(ls *lessonState, criterion string) HeldOutStatus {
	st := HeldOutStatus{Criterion: criterion}
	if ls == nil {
		return st
	}
	for _, a := range ls.attempts {
		for _, r := range a.Criteria {
			if r.ID != criterion || r.kind() != CriterionHeldOut || r.Results == nil || r.Outcome == OutcomeErrored {
				continue
			}
			if r.Counted != nil && *r.Counted {
				st.Counted, st.CountedAttempt = r.Results, a.ID
				st.Latest, st.LatestAttempt, st.LatestNotCounted = nil, "", ""
			} else {
				st.Latest, st.LatestAttempt, st.LatestNotCounted = r.Results, a.ID, r.NotCounted
			}
		}
	}
	return st
}

// work is a Lesson's Check and work as they are now.
type work struct {
	check        []Criterion
	checkVersion string
	snapshot     string
	// files are the files of the work the snapshot sees, relative to the
	// practice folder: those not ignored.
	files map[string]string
	// lookedAt holds the content hash, now, of each file a rubric grade
	// looked at, by its path in the Topic; empty for a file that is gone or
	// no longer part of the work.
	lookedAt map[string]string
	// missing explains why the Check or the work cannot be judged.
	missing error
}

// lookedAtPaths lists the files a Lesson's rubric grades looked at.
func lookedAtPaths(ls *lessonState) []string {
	if ls == nil {
		return nil
	}
	var paths []string
	for _, g := range ls.grades {
		for _, f := range g.LookedAt {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

// staleGrade explains why a rubric item's latest grade does not count for
// the Check shown to the learner and the work as it is now; empty when it
// counts.
func staleGrade(ls *lessonState, criterion string, w work) string {
	g := ls.grades[criterion]
	switch {
	case g == nil:
		return "rubric item " + criterion + " has no grade: grade it with rubric_record, after the learner checks " +
			"their work against it"
	case g.CheckVersion != ls.shownCheck || g.CheckVersion != w.checkVersion:
		return "rubric item " + criterion + " was graded for another version of the Check: grade it again"
	case g.Snapshot != w.snapshot:
		return "the work changed since rubric item " + criterion + " was graded: grade it again"
	}
	for _, f := range g.LookedAt {
		if w.lookedAt[f.Path] != f.Hash {
			return "the file " + f.Path + ", which rubric item " + criterion + " was graded on, changed: grade it again"
		}
	}
	return ""
}

// currentWork reads a Lesson's Check and snapshots its practice folder,
// writing nothing, not even git objects. It also hashes the files named in
// lookedAt, by their paths in the Topic.
func (c *Core) currentWork(ctx context.Context, topicID, dir, lessonID string, lookedAt ...string) (work, error) {
	w := work{check: []Criterion{}}
	home, topic, err := c.openTopicFolder(topicID)
	if err != nil {
		return w, err
	}
	check, version, err := readCheck(topic, lessonID)
	topic.Close()
	home.Close()
	if CodeOf(err) == CodeFailedPrecondition || CodeOf(err) == CodeCorrupt {
		w.missing = err
		return w, nil
	}
	if err != nil {
		return w, err
	}
	w.check, w.checkVersion = check, version
	practice := practiceFolder(lessonID)
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(practice))); errors.Is(err, fs.ErrNotExist) {
		w.missing = &Error{Code: CodeFailedPrecondition, Message: practice + "/ does not exist"}
		return w, nil
	}
	snap, err := checkpoint.SnapshotWork(ctx, dir, practice)
	if blindSpot(err) {
		w.missing = &Error{Code: CodeFailedPrecondition, Message: blindSpotReason(practice, err)}
		return w, nil
	}
	if err != nil {
		return w, checkpointError(topicID, dir, err)
	}
	w.snapshot, w.files = snap.Hash, snap.Files
	if len(lookedAt) > 0 {
		w.lookedAt = map[string]string{}
		for _, p := range lookedAt {
			w.lookedAt[p], _ = c.hashWorkFile(topicID, lessonID, p, w.files)
		}
	}
	return w, nil
}

// completion is what the completion rule relied on: the passing Attempt, if
// the Check has run criteria, and the rubric grades, if it has rubric items.
type completion struct {
	attempt Attempt
	grades  []string
}

// completionRule finds what allows completing a Lesson: for the Check
// version shown to the learner when practicing last started, which must
// still be the current one, every run criterion passed on an Attempt of that
// Check whose snapshot matches the current work, and every rubric item has a
// grade for that Check and that work, whose files it looked at are
// unchanged. Held-out criteria never decide it.
// Changing the work or the Check after a pass means running the Check, and
// grading, again, and a changed Check must be shown again through phase_set
// practicing.
func completionRule(s *replayed, lessonID string, w work) (completion, error) {
	var done completion
	if w.missing != nil {
		return done, w.missing
	}
	cannot := func(reason string) error {
		return &Error{Code: CodeFailedPrecondition, Message: fmt.Sprintf("Lesson %s cannot be completed yet: %s", lessonID, reason)}
	}
	ls := s.study.lessons[lessonID]
	if ls == nil || ls.shownCheck == "" {
		return done, cannot("its Check was never shown to the learner: move it to practicing with phase_set first")
	}
	if w.checkVersion != ls.shownCheck {
		return done, cannot("the Check changed since it was shown to the learner when practicing started; " +
			"show the learner the new Check with phase_set practicing, then run it again")
	}
	var runs, rubric []Criterion
	for _, crit := range w.check {
		switch crit.Kind {
		case CriterionRun:
			runs = append(runs, crit)
		case CriterionRubric:
			rubric = append(rubric, crit)
		}
	}
	if len(runs) > 0 {
		a, err := passingAttempt(ls, lessonID, w, cannot)
		if err != nil {
			return done, err
		}
		done.attempt = a
	}
	for _, crit := range rubric {
		if why := staleGrade(ls, crit.ID, w); why != "" {
			return done, cannot(why)
		}
		done.grades = append(done.grades, ls.grades[crit.ID].Event)
	}
	return done, nil
}

// passingAttempt finds the latest Attempt of the shown Check, on the current
// work, whose run criteria all passed: an Attempt's outcome comes from its
// run criteria alone.
func passingAttempt(ls *lessonState, lessonID string, w work, cannot func(string) error) (Attempt, error) {
	if len(ls.attempts) == 0 {
		return Attempt{}, cannot("its Check has never run: run study check " + lessonID + " in your shell")
	}
	for i := len(ls.attempts) - 1; i >= 0; i-- {
		a := ls.attempts[i]
		if a.Outcome == OutcomePassed && a.CheckVersion == ls.shownCheck && a.Snapshot == w.snapshot {
			return a, nil
		}
	}
	last := ls.attempts[len(ls.attempts)-1]
	reason := "no Attempt passed"
	switch {
	case last.Outcome != OutcomePassed:
		reason = "the last Attempt " + last.Outcome
	case last.CheckVersion != ls.shownCheck:
		reason = "the Check that last passed is not the one shown to the learner"
	case last.Snapshot != w.snapshot:
		reason = "the work changed since the Check last passed"
	}
	return Attempt{}, cannot(reason + "; run study check " + lessonID + " in your shell on the current work")
}

// cleanTextBlock is cleanText for multi-line text: line breaks and tabs are
// allowed, other control characters are not.
func cleanTextBlock(field, s string, maxRunes int) (string, error) {
	if !utf8.ValidString(s) {
		return "", invalidf("the %s is not valid UTF-8 text", field)
	}
	s = strings.TrimSpace(s)
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", invalidf("the %s contains a control character", field)
		}
		if isBidiControl(r) {
			return "", invalidf("the %s contains a bidirectional control character (U+%04X), which can make text "+
				"display differently from what it says", field, r)
		}
	}
	if n := len([]rune(s)); n > maxRunes {
		return "", invalidf("the %s is longer than %d characters", field, maxRunes)
	}
	return s, nil
}
