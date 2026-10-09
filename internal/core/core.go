// Package core owns Lamplight's study state and rules.
//
// The CLI and the MCP server are thin adapters over this package: every domain
// operation lives here, and tests exercise it in process with a fixed clock and
// a temporary Study home. Terms follow GLOSSARY.md.
package core

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// FormatVersion is the format number written to every file the core owns.
// A binary refuses files with a newer format than it understands.
const FormatVersion = 1

// Options configures a Core. Zero values select the production defaults.
type Options struct {
	// Getenv reads environment variables such as STUDY_HOME, XDG_CONFIG_HOME
	// and HOME. Defaults to os.Getenv.
	Getenv func(string) string
	// Dir is the folder the learner or agent started in. It decides the
	// Active topic and anchors relative paths. Defaults to the process
	// working directory.
	Dir string
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
	// NewID returns a new unique Event ID. Defaults to a random ID.
	NewID func() string
	// Logger receives the Log: diagnostics, never the learner's activity.
	// Defaults to discarding everything.
	Logger *slog.Logger
	// Crash is only for tests that simulate a crash, and must be nil
	// otherwise: it is called at each point of a write where one could
	// happen (CrashAfterIntent and the other Crash points), and returning an
	// error stops the write there without any cleanup, as a crash would.
	Crash func(point string) error
}

// Points of a write where Options.Crash can simulate a crash.
const (
	// CrashAfterIntent: the intent marker is written, the Event is not.
	CrashAfterIntent = crashAfterIntent
	// CrashAfterEvent: the Event is in the History, no content is written.
	CrashAfterEvent = crashAfterEvent
	// CrashAfterItem: after each item the Event edits is replaced.
	CrashAfterItem = crashAfterItem
	// CrashBeforeClear: all content is written, the marker remains.
	CrashBeforeClear = crashBeforeClear
	// CrashRecoveryAfterItem: recovery replaced an item of an interrupted
	// write.
	CrashRecoveryAfterItem = crashRecoveryAfterItem
	// CrashRecoveryBeforeClear: recovery is done, the marker remains.
	CrashRecoveryBeforeClear = crashRecoveryBeforeClear
)

// Core is the Lamplight core for one Study home.
type Core struct {
	home   string
	dir    string
	getenv func(string) string
	now    func() time.Time
	newID  func() string
	log    *slog.Logger
	// gating reports whether an item approves or gates progress; see
	// isGating.
	gating func(item string) bool
	// crash, when set by a test, is called at each point of a write where
	// a crash could happen; returning an error stops the write there
	// without any cleanup, as a crash would.
	crash func(point string) error
}

// Open resolves the Study home and returns a Core for it. The Study home is
// created on the first write, not by Open.
func Open(opts Options) (*Core, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	home, err := resolveHome(getenv)
	if err != nil {
		return nil, err
	}
	dir := opts.Dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return nil, internalError("reading the working directory", err)
		}
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return nil, internalError("resolving the working directory", err)
	}
	c := &Core{home: home, dir: dir, getenv: getenv, now: opts.Now, newID: opts.NewID, log: opts.Logger, gating: isGating, crash: opts.Crash}
	if c.now == nil {
		c.now = time.Now
	}
	if c.newID == nil {
		c.newID = randomID
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c, nil
}

// Home returns the absolute path of the Study home.
func (c *Core) Home() string { return c.home }

// Now returns the current time by the Core's clock, so adapters describe
// times such as a Card's due date the way the core decided them.
func (c *Core) Now() time.Time { return c.now() }

// expandPath makes a path the learner or agent gave absolute: a leading ~
// is the home folder, as in STUDY_HOME, and a relative path is relative to
// the folder study started in.
func (c *Core) expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home := ""
		if c.getenv != nil {
			home = c.getenv("HOME")
		}
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.dir, path)
	}
	return filepath.Clean(path)
}

// config is the global configuration file, config.toml.
type config struct {
	Format    int    `toml:"format"`
	StudyHome string `toml:"study_home"`
}

// resolveHome picks the Study home: STUDY_HOME, then study_home in
// config.toml, then ~/study. The result must be absolute (after expanding a
// leading ~), so the same Study home is found from every folder.
func resolveHome(getenv func(string) string) (string, error) {
	userHome := func() (string, error) { return userHomeDir(getenv) }

	home, source := getenv("STUDY_HOME"), "STUDY_HOME"
	if home == "" {
		path, err := configPath(getenv)
		if err != nil {
			return "", err
		}
		cfg, err := loadConfig(path)
		if err != nil {
			return "", err
		}
		home, source = cfg.StudyHome, "study_home in "+path
	}
	if home == "" {
		h, err := userHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, "study"), nil
	}
	if home == "~" || strings.HasPrefix(home, "~/") {
		h, err := userHome()
		if err != nil {
			return "", err
		}
		home = filepath.Join(h, strings.TrimPrefix(home, "~"))
	}
	if !filepath.IsAbs(home) {
		return "", invalidf("%s must be an absolute path or start with ~/, not %q", source, home)
	}
	return filepath.Clean(home), nil
}

// userHomeDir returns the learner's home folder: HOME, or the operating
// system's answer when HOME is unset.
func userHomeDir(getenv func(string) string) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", &Error{Code: CodeInvalidArgument,
			Message: "cannot find your home folder: set STUDY_HOME to the absolute path of your Study home", Err: err}
	}
	return h, nil
}

func configPath(getenv func(string) string) (string, error) {
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := userHomeDir(getenv)
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "lamplight", "config.toml"), nil
}

func loadConfig(path string) (config, error) {
	var cfg config
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, internalError("reading "+path, err)
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, corruptf("%s is not valid TOML: %v", path, err)
	}
	if cfg.Format > FormatVersion {
		return cfg, newerFormat(path, cfg.Format)
	}
	return cfg, nil
}

// gitCommand returns a git command that runs in dir with a clean environment:
// the caller's GIT_* variables are dropped, because GIT_DIR, GIT_WORK_TREE and
// their friends would point git at another repository, and the system
// configuration is not read. stdin is /dev/null.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GIT_CONFIG_NOSYSTEM=1")
	return cmd
}

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.
	return strings.ToLower(idEncoding.EncodeToString(b))
}

// ErrorCode classifies errors so the adapters can report them consistently.
type ErrorCode string

// Error codes reported by the CLI and the MCP server. See docs/cli.md.
const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeAlreadyExists   ErrorCode = "already_exists"
	CodeNotFound        ErrorCode = "not_found"
	CodeNewerFormat     ErrorCode = "newer_format"
	CodeCorrupt         ErrorCode = "corrupt"
	// CodeFailedPrecondition means the request is valid but the Topic is in
	// a state that prevents it, such as a git merge in progress.
	CodeFailedPrecondition ErrorCode = "failed_precondition"
	// CodeBusy means another program holds a lock; retrying shortly may work.
	CodeBusy ErrorCode = "busy"
	// CodeCanceled means the command was stopped, by SIGTERM or Ctrl-C,
	// before it finished; what it would have recorded was not recorded.
	CodeCanceled ErrorCode = "canceled"
	CodeInternal ErrorCode = "internal"
)

// Error is a domain error with a stable code.
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// CodeOf returns the code of err, or CodeInternal for errors the core did not
// classify.
//
// An error of a package the core builds on carries its code itself, through
// an ErrorCode method: the Knowledge base plugin registry's errors do, which
// reach the command line without passing through the core. Their codes are
// among the ones above.
func CodeOf(err error) ErrorCode {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return ErrorCode(coded.ErrorCode())
	}
	return CodeInternal
}

func invalidf(format string, args ...any) error {
	return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func corruptf(format string, args ...any) error {
	return &Error{Code: CodeCorrupt, Message: fmt.Sprintf(format, args...)}
}

func internalError(doing string, err error) error {
	return &Error{Code: CodeInternal, Message: fmt.Sprintf("%s: %v", doing, err), Err: err}
}

func newerFormat(path string, format int) error {
	return &Error{
		Code: CodeNewerFormat,
		Message: fmt.Sprintf("%s has format %d, but this version of study only understands format %d: upgrade study",
			path, format, FormatVersion),
	}
}
