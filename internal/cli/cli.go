// Package cli is the study command line: a thin adapter over the core.
//
// It follows the conventions in docs/cli.md: human output by default, a stable
// JSON envelope with --json (JSON on stdout only, diagnostics on stderr), and
// documented exit codes.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/fang"
	mango "github.com/muesli/mango-cobra"
	"github.com/muesli/roff"
	"github.com/spf13/cobra"

	"github.com/mordor-forge/lamplight/v2/internal/core"
	"github.com/mordor-forge/lamplight/v2/internal/mcpserver"
)

// Version is the study version. Release builds set it with -ldflags.
var Version = ""

// Exit codes. See docs/cli.md.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Run executes the study command line with args (without the program name)
// and returns the process exit code. stdin is read by study mcp, and by
// prompts when it is a terminal.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, opts core.Options) int {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	a := &app{opts: opts, stdin: stdin, stdout: stdout, stderr: stderr,
		out: colorprofile.NewWriter(stdout, environ(opts.Getenv))}
	defer func() { a.logs.close() }()
	root := a.rootCommand()
	// Decide the output mode before parsing, so errors in earlier flags are
	// still reported as JSON when --json appears later on the command line.
	// This must follow rootCommand: registering the flag resets a.json.
	a.json = wantsJSON(args)
	if args == nil {
		args = []string{} // cobra reads os.Args when given nil
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	var err error
	if a.json {
		// fang styles help and errors for people, and to pick colours it
		// queries the terminal on stdout, which would corrupt the JSON
		// document. JSON runs use cobra directly.
		root.SilenceUsage, root.SilenceErrors = true, true
		root.Version = version()
		if err = root.ExecuteContext(ctx); err != nil {
			a.handleError(a.stderr, fang.Styles{}, err)
		}
	} else {
		err = fang.Execute(ctx, root,
			fang.WithVersion(version()),
			fang.WithErrorHandler(a.handleError),
			// study has its own man command, which writes where it is told.
			fang.WithoutManpage(),
		)
	}
	switch {
	case err == nil:
		return a.exit
	case isUsage(err):
		return ExitUsage
	default:
		return ExitError
	}
}

// CommandTree returns the study command tree without running anything, so
// tools and tests can check which commands and flags exist, as the lamplight
// skill's test does for the commands the skill shows.
func CommandTree() *cobra.Command {
	a := &app{opts: core.Options{Getenv: os.Getenv}, stdin: strings.NewReader(""),
		stdout: io.Discard, stderr: io.Discard, out: io.Discard}
	return a.rootCommand()
}

type app struct {
	opts   core.Options
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// out is stdout for people: styles are downgraded to what the terminal
	// supports, and dropped with NO_COLOR or when stdout is not a terminal.
	out      io.Writer
	json     bool
	logLevel string
	logs     *logs
	// exit is the exit code of a command that reported its own result, such
	// as study doctor finding a failure, without returning an error.
	exit int
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "study",
		Short: "Lamplight: see where you are in your studies, and what to do next",
		Long: "Lamplight keeps your study Topics, Syllabus, Cards and History in plain files in your Study home.\n" +
			"Run study on its own to see where you are.",
		Args: noArgs,
		RunE: a.runStatus,
	}
	// Set before the completion command is built: it keeps the writer it
	// sees then.
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.PersistentFlags().BoolVar(&a.json, "json", false, "print a JSON envelope on stdout (see docs/cli.md)")
	root.PersistentFlags().StringVar(&a.logLevel, "log-level", "",
		"Log detail: debug, info, warn or error (default from STUDY_LOG, else info)")
	_ = root.RegisterFlagCompletionFunc("log-level",
		cobra.FixedCompletions([]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp))

	serve := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server for agents over stdin and stdout",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			a.logs.logger.Debug("serving MCP over stdio", "version", version(), "study_home", c.Home())
			a.refreshSkill(cmd.Context())
			return mcpserver.Serve(cmd.Context(), c, version(), a.stdin, a.stdout, a.logs.logger)
		},
	}

	// Once parsing succeeds, the parsed flag decides the output mode: the
	// up-front scan in Run could mistake a flag value such as --title --json
	// for the flag.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if f := cmd.Flags().Lookup("json"); f == nil || !f.Changed {
			a.json = false
		}
		levelFlag := cmd.Flags().Lookup("log-level")
		logs, err := newLogs(a.opts.Getenv, a.stderr, a.logLevel, levelFlag != nil && levelFlag.Changed, cmd == serve)
		if err != nil {
			return err
		}
		a.logs = logs
		a.opts.Logger = logs.logger
		logs.logger.Debug("running", "command", cmd.CommandPath())
		return nil
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })

	status := &cobra.Command{
		Use:   "status",
		Short: "Show where you are: the Active topic and every Topic",
		Args:  noArgs,
		RunE:  a.runStatus,
	}

	topic := &cobra.Command{
		Use:   "topic",
		Short: "Create, change and remove Topics, and dismiss their flags",
		Args:  noArgs,
		RunE:  a.groupHelp,
	}
	var spec core.TopicSpec
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a Topic in the Study home",
		Example: `  study topic create --title "Linear algebra"
  study topic create --title "C" --id c --goal "Write and debug small C programs"`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			created, err := c.CreateTopic(cmd.Context(), spec)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: created})
			}
			return writeTopicCreated(a.out, created, spec.DryRun)
		},
	}
	create.Flags().StringVar(&spec.Title, "title", "", "what you are studying, for example \"Linear algebra\" (required)")
	create.Flags().StringVar(&spec.ID, "id", "", "folder name; derived from the title when omitted")
	create.Flags().StringVar(&spec.Goal, "goal", "", "what you want to be able to do at the end")
	create.Flags().BoolVar(&spec.DryRun, "dry-run", false, "show what would be created without writing anything")
	var changes core.TopicChanges
	var newTitle, newGoal, deadline, state, level, approach string
	var pace []string
	var clearPace bool
	var newCards int
	var kb core.KnowledgeBase
	update := &cobra.Command{
		Use:   "update <topic>",
		Short: "Change a Topic's title, goal, Knowledge base, deadline, Pace, Level, Approach or state",
		Example: `  study topic update linear-algebra --goal "Pass the June exam" --deadline 2027-06-01
  study topic update c --title "Systems programming in C" --dry-run
  study topic update c --knowledge-base notebooklm --notebook 4f2a9c1e
  study topic update c --pace 10 --pace 3@2026-11-16
  study topic update c --level intermediate --approach project
  study topic update c --state paused`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("title") {
				changes.Title = &newTitle
			}
			if cmd.Flags().Changed("goal") {
				changes.Goal = &newGoal
			}
			if cmd.Flags().Changed("knowledge-base") || cmd.Flags().Changed("notebook") {
				changes.KnowledgeBase = &kb
			}
			if cmd.Flags().Changed("deadline") {
				changes.Deadline = &deadline
			}
			if clearPace && len(pace) > 0 {
				return a.fail(usageError{fmt.Errorf("give --pace or --clear-pace, not both")})
			}
			if len(pace) > 0 || clearPace {
				periods, err := parsePace(pace)
				if err != nil {
					return a.fail(err)
				}
				changes.Pace = &periods
			}
			if cmd.Flags().Changed("new-cards-per-day") {
				changes.NewCardsPerDay = &newCards
			}
			if cmd.Flags().Changed("state") {
				changes.State = &state
			}
			if cmd.Flags().Changed("level") {
				changes.Level = &level
			}
			if cmd.Flags().Changed("approach") {
				changes.Approach = &approach
			}
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			updated, err := c.UpdateTopic(cmd.Context(), args[0], changes)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: updated})
			}
			return writeTopicUpdate(a.out, updated, changes.DryRun)
		},
	}
	update.Flags().StringVar(&newTitle, "title", "", "the new title")
	update.Flags().StringVar(&newGoal, "goal", "", "the new goal; an empty goal removes it")
	update.Flags().StringVar(&kb.Kind, "knowledge-base", "", "where the Topic's Sources are searched: notebooklm or none")
	update.Flags().StringVar(&kb.Notebook, "notebook", "", "the NotebookLM notebook's id, with --knowledge-base notebooklm")
	_ = update.RegisterFlagCompletionFunc("knowledge-base", cobra.FixedCompletions(
		[]string{core.KnowledgeBaseNotebookLM, core.KnowledgeBaseNone}, cobra.ShellCompDirectiveNoFileComp))
	update.Flags().StringVar(&deadline, "deadline", "", "the Goal's deadline, YYYY-MM-DD; an empty value removes it")
	update.Flags().StringArrayVar(&pace, "pace", nil,
		"hours a week, such as 10; repeat with HOURS@YYYY-MM-DD for a period starting that day; replaces the Pace")
	update.Flags().BoolVar(&clearPace, "clear-pace", false, "remove the Pace")
	update.Flags().IntVar(&newCards, "new-cards-per-day", core.NewCardsPerDay, "the daily cap on new Cards decided")
	update.Flags().StringVar(&state, "state", "", "active, paused or finished")
	_ = update.RegisterFlagCompletionFunc("state", cobra.FixedCompletions(
		[]string{core.TopicActive, core.TopicPaused, core.TopicFinished}, cobra.ShellCompDirectiveNoFileComp))
	update.Flags().StringVar(&level, "level", "", "beginner, intermediate, advanced or expert; holds until the next Assessment")
	_ = update.RegisterFlagCompletionFunc("level", cobra.FixedCompletions(
		[]string{core.LevelBeginner, core.LevelIntermediate, core.LevelAdvanced, core.LevelExpert},
		cobra.ShellCompDirectiveNoFileComp))
	update.Flags().StringVar(&approach, "approach", "",
		"how the Lessons relate: concepts, project (one project built step by step) or challenges")
	_ = update.RegisterFlagCompletionFunc("approach", cobra.FixedCompletions(
		[]string{core.ApproachConcepts, core.ApproachProject, core.ApproachChallenges}, cobra.ShellCompDirectiveNoFileComp))
	update.Flags().BoolVar(&changes.DryRun, "dry-run", false, "show the result without writing anything")
	var dismissDryRun bool
	dismiss := &cobra.Command{
		Use:   "dismiss-flag <topic> <flag-id>",
		Short: "Dismiss a flag in a Topic once you have looked at it",
		Long: "Dismiss a flag that study status shows, once you have looked at it and accepted it.\n" +
			"Dismissing records your decision in the History; it never changes any content.",
		Example: "  study topic dismiss-flag linear-algebra 3f9c2a71b0",
		Args:    exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := c.DismissFlag(cmd.Context(), args[0], args[1], dismissDryRun)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writeFlagDismissal(a.out, res)
		},
	}
	dismiss.Flags().BoolVar(&dismissDryRun, "dry-run", false, "show the flag that would be dismissed without recording anything")
	var removeDryRun bool
	remove := &cobra.Command{
		Use:   "remove <topic>",
		Short: "Move a Topic out of the Study home, deleting nothing",
		Long: "Move a Topic's folder, whole, into the Study home's .lamplight/removed folder, as\n" +
			"<YYYYMMDD-HHMMSS>-<topic> in UTC. Nothing is deleted: study prints the exact command\n" +
			"that restores it, mv <moved to> <Study home>/<topic>, to run while no other Topic has\n" +
			"that id. Use it to import a v1 workspace again, for example with --not-done.\n\n" +
			"It acts on this computer only: the Topic's git remote and other computers keep their\n" +
			"copies. It waits for a write in progress, and refuses while an interrupted one waits\n" +
			"to be finished.",
		Example: "  study topic remove go-concurrency --dry-run\n  study topic remove go-concurrency",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := c.RemoveTopic(cmd.Context(), args[0], removeDryRun)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writeTopicRemoval(a.out, res)
		},
	}
	remove.Flags().BoolVar(&removeDryRun, "dry-run", false, "show where the Topic would go without moving it")
	topic.AddCommand(create, update, dismiss, remove)

	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose your setup: Study home, git, Topics, Library, Log and completions",
		Long: "Diagnose your setup. doctor works even when the rest of study cannot, and tells you how to\n" +
			"fix what it finds. It exits with 1 when something must be fixed, and 0 otherwise.",
		Args: noArgs,
		RunE: a.runDoctor,
	}

	root.AddCommand(status, topic, a.checkpointCommand(), a.checkCommand(), a.libraryCommand(), doctor, serve)
	root.AddCommand(a.sourceCommand(), a.evidenceCommand(), a.syllabusCommand(), a.revisionCommand(), a.cardCommand(), a.reviewCommand())
	root.AddCommand(a.importCommand())
	root.AddCommand(a.sessionCommand())
	root.AddCommand(a.taskCommand())
	root.AddCommand(a.rubricCommand(), a.resultsCommand(), a.lessonCommand(), a.historyCommand())
	root.AddCommand(a.assessmentCommand(), a.hintCommand(), a.signalsCommand())
	root.AddCommand(a.setupCommand(), a.claudePluginPathCommand(), a.claudeHookCommand())
	root.AddCommand(a.manCommand())
	a.completionCommands(root)
	return root
}

// manCommand prints study's man page in roff, for packages to install as
// study.1. It replaces fang's own, which writes to the process's stdout
// rather than the command's. The page is roff, so --json is a usage error.
// Its date is the build's (see manDate), so a package built twice from one
// commit holds the same page.
func (a *app) manCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "man",
		Short:                 "Print the man page, for packagers",
		Hidden:                true,
		DisableFlagsInUseLine: true,
		Args:                  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.json {
				return a.fail(usageError{errors.New("study man prints the man page in roff, never JSON: run it without --json")})
			}
			date, err := a.manDate()
			if err != nil {
				return a.fail(err)
			}
			page, err := mango.NewManPage(1, cmd.Root())
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), page.Build(datedPage{roff.NewDocument(), date}))
			return err
		},
	}
}

// manDate is the date the man page carries: the one SOURCE_DATE_EPOCH
// names, in seconds since 1970, which packagers set to the time of the
// source so that builds are reproducible, or else today's. Both in UTC.
func (a *app) manDate() (time.Time, error) {
	if v := a.opts.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		sec, err := strconv.ParseInt(v, 10, 64)
		if err != nil || sec < 0 {
			return time.Time{}, usageError{fmt.Errorf("SOURCE_DATE_EPOCH is %q, not a time in seconds since 1970: "+
				"unset it, or set it to the time of the source, such as git log -1 --format=%%ct", v)}
		}
		return time.Unix(sec, 0).UTC(), nil
	}
	now := time.Now
	if a.opts.Now != nil {
		now = a.opts.Now
	}
	return now().UTC(), nil
}

// datedPage is a roff document whose heading carries date: mango passes the
// time the page is built.
type datedPage struct {
	*roff.Document
	date time.Time
}

func (p datedPage) Heading(section uint, title, description string, _ time.Time) {
	p.Document.Heading(section, title, description, p.date)
}

func (a *app) checkpointCommand() *cobra.Command {
	var spec core.CheckpointSpec
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Save the work in a Topic as a git commit",
		Long: "Save the work in a Topic as a git commit, at the end of a turn: the learner's or the agent's.\n" +
			"Nothing is committed when nothing changed. Checkpoints never run programs named in the Topic's git configuration.",
		Example: `  study checkpoint --topic linear-algebra --role learner --message "Gaussian elimination exercise"
  study checkpoint --topic c --role agent --dry-run`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			res, err := c.Checkpoint(cmd.Context(), spec)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: res})
			}
			return writeCheckpoint(a.out, res)
		},
	}
	cmd.Flags().StringVar(&spec.Topic, "topic", "", "the Topic's id (required)")
	cmd.Flags().StringVar(&spec.Role, "role", "", "whose turn ended: agent or learner (required)")
	cmd.Flags().StringVarP(&spec.Message, "message", "m", "", "what happened in the turn")
	cmd.Flags().BoolVar(&spec.DryRun, "dry-run", false, "show whether a Checkpoint would be made, without committing")
	return cmd
}

// runDoctor reports the diagnosis itself, so an unhealthy setup sets the exit
// code without returning an error: the report is the whole output.
func (a *app) runDoctor(cmd *cobra.Command, _ []string) error {
	d := core.Diagnose(cmd.Context(), a.opts)
	d.Add(a.diagnoseLogs())
	d.Add(a.diagnoseCompletion())
	d.Add(a.diagnoseSetup(cmd.Context()))
	if !d.Healthy {
		a.exit = ExitError
		if a.json {
			failed := d.Failed()
			msg := fmt.Sprintf("%d %s failed: %s", len(failed), plural(len(failed), "finding", "findings"), strings.Join(failed, ", "))
			return a.writeJSON(envelope{Data: d, Error: &errorBody{Code: codeUnhealthy, Message: msg}})
		}
	} else if a.json {
		return a.writeJSON(envelope{OK: true, Data: d})
	}
	return writeDiagnosis(a.out, d)
}

// codeUnhealthy is the error code of study doctor when a Finding failed.
const codeUnhealthy = "unhealthy"

func (a *app) diagnoseLogs() core.Finding {
	f := core.Finding{Name: "log"}
	if err := a.logs.probe(); err != nil {
		f.Status, f.Message = core.FindingWarn, "the Log cannot be written: "+err.Error()
		f.Fix = "make the folder writable, or set XDG_STATE_HOME to a writable folder"
		return f
	}
	if a.logs.badEnv != "" {
		f.Status, f.Message = core.FindingWarn, fmt.Sprintf("STUDY_LOG=%q is not a level, so study uses info", a.logs.badEnv)
		f.Fix = "set STUDY_LOG to debug, info, warn or error"
		return f
	}
	f.Status, f.Message = core.FindingOK, fmt.Sprintf("%s (level %s)", a.logs.path(), levelName(a.logs.level))
	return f
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (a *app) libraryCommand() *cobra.Command {
	lib := &cobra.Command{
		Use:   "library",
		Short: "Index and search your Library of books",
		Args:  noArgs,
		RunE:  a.groupHelp,
	}
	build := &cobra.Command{
		Use:     "build <folder>",
		Short:   "Index the books in a folder, replacing the previous index",
		Example: "  study library build ~/Books",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			summary, err := c.BuildLibrary(cmd.Context(), args[0])
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: summary})
			}
			return writeLibrarySummary(a.out, summary)
		},
	}
	var limit int
	search := &cobra.Command{
		Use:   "search <query>",
		Short: "Find books in your Library",
		Example: `  study library search "linear algebra"
  study library search C --limit 5`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := core.Open(a.opts)
			if err != nil {
				return a.fail(err)
			}
			results, err := c.SearchLibrary(cmd.Context(), strings.Join(args, " "), limit)
			if err != nil {
				return a.fail(err)
			}
			if a.json {
				return a.writeJSON(envelope{OK: true, Data: map[string]any{"results": results}})
			}
			return writeSearchResults(a.out, results)
		},
	}
	search.Flags().IntVar(&limit, "limit", core.DefaultSearchLimit,
		fmt.Sprintf("most results to show, up to %d", core.MaxSearchLimit))
	lib.AddCommand(build, search)
	return lib
}

func (a *app) runStatus(cmd *cobra.Command, _ []string) error {
	c, err := core.Open(a.opts)
	if err != nil {
		return a.fail(err)
	}
	status, err := c.Status(cmd.Context())
	if err != nil {
		return a.fail(err)
	}
	if a.json {
		return a.writeJSON(envelope{OK: true, Data: status})
	}
	return writeStatus(a.out, status, c.Now())
}

// envelope is the JSON shape of every --json result. See docs/cli.md.
type envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a *app) writeJSON(v envelope) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// fail reports err. With --json the error envelope goes to stdout and the
// returned error is marked as already reported, so fang prints nothing more.
func (a *app) fail(err error) error {
	code := string(core.CodeOf(err))
	if isUsage(err) {
		code = "usage"
	} else if core.CodeOf(err) == core.CodeInvalidArgument {
		err = usageError{err}
	}
	if !a.json {
		return err
	}
	if werr := a.writeJSON(envelope{Error: &errorBody{Code: code, Message: err.Error()}}); werr != nil {
		return werr
	}
	return reported{err}
}

// handleError prints errors that were not already reported as JSON. Human
// errors are printed as written: fang's own handler would re-case them
// ("/Tmp/…", "--Log-Level").
func (a *app) handleError(_ io.Writer, _ fang.Styles, err error) {
	var r reported
	if errors.As(err, &r) {
		return
	}
	if a.json {
		code := string(core.CodeInternal)
		if isUsage(err) {
			code = "usage"
		}
		_ = a.writeJSON(envelope{Error: &errorBody{Code: code, Message: err.Error()}})
		return
	}
	w := colorprofile.NewWriter(a.stderr, environ(a.opts.Getenv))
	fmt.Fprintf(w, "%s %s\n", styleFail.Render("Error:"), err.Error())
	if isUsage(err) {
		fmt.Fprintln(w, styleDim.Render("Run the command with --help for usage."))
	}
}

// reported marks an error whose JSON envelope was already written.
type reported struct{ err error }

func (r reported) Error() string { return r.err.Error() }
func (r reported) Unwrap() error { return r.err }

// usageError marks invalid arguments or flags, which exit with ExitUsage.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

func isUsage(err error) bool {
	var u usageError
	return errors.As(err, &u)
}

// groupHelp prints help for command groups such as study topic. Unknown
// subcommands arrive as arguments and are rejected by noArgs first. Help is
// text, so with --json a group without a subcommand is a usage error.
func (a *app) groupHelp(cmd *cobra.Command, _ []string) error {
	if !a.json {
		return cmd.Help()
	}
	var names []string
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			names = append(names, sub.Name())
		}
	}
	return a.fail(usageError{fmt.Errorf("%q needs a subcommand: %s", cmd.CommandPath(), strings.Join(names, ", "))})
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{fmt.Errorf("unexpected argument %q for %q", args[0], cmd.CommandPath())}
	}
	return nil
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usageError{fmt.Errorf("%q takes %d argument(s), got %d", cmd.CommandPath(), n, len(args))}
		}
		return nil
	}
}

func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return usageError{fmt.Errorf("%q takes at most %d argument(s), got %d", cmd.CommandPath(), n, len(args))}
		}
		return nil
	}
}

func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return usageError{fmt.Errorf("%q needs at least %d argument(s)", cmd.CommandPath(), n)}
		}
		return nil
	}
}

func wantsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}

var (
	releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	pseudoVersion  = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)
)

// version returns the release version set at build time, or the module
// version for a tagged go install (release candidates included), and "dev"
// for anything else. Pseudo-versions are not shown: they derive from the v1
// skill's tags and would mislead.
func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if releaseVersion.MatchString(v) && !pseudoVersion.MatchString(v) {
			return v
		}
	}
	return "dev"
}
