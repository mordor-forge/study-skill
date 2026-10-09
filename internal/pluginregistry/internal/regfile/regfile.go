// Package regfile is what reading the Knowledge base plugin registry and
// writing it have in common: finding Lamplight's configuration folder and
// opening it, the rule that keeps the Study home out of it, and the file's
// own shape.
//
// It is an internal package of internal/pluginregistry so that only two
// packages can use it: pluginregistry, which reads the registry and which
// the core and the MCP server link, and pluginregistry/registrar, which
// changes it and which only the command line links. What it gives them is
// more than either may pass on: the configuration folder, open and writable.
// Nothing outside those two packages can reach it.
package regfile

import (
	"errors"
	"fmt"
)

const (
	// FileName is the registry in Lamplight's configuration folder, and
	// LockName the file beside it whose lock a change holds. The lock cannot
	// be the registry's own: a change replaces the registry with a new file.
	FileName = "knowledge-base-plugins.json"
	LockName = "knowledge-base-plugins.lock"

	// Format is the format number this version writes, and the newest it
	// reads.
	Format = 1

	// MaxBytes is how large a registry may be, MaxArgs how many arguments a
	// plugin's command may have, and MaxWordRunes how long its program's
	// path and each argument may be: far more than a registry needs.
	MaxBytes     = 1 << 20
	MaxArgs      = 100
	MaxWordRunes = 4096

	maxURLRunes = 2000
)

// Env is what the registry needs to know of one run of study.
type Env struct {
	// StudyHome is the Study home's absolute path.
	StudyHome string
	// Dir is the folder study was started in, as an absolute path.
	Dir string
	// Getenv reads the environment: XDG_CONFIG_HOME, HOME and PATH.
	Getenv func(string) string
}

// Plugin is a Knowledge base plugin registered on this computer: a name, and
// either the command that starts it or the URL it answers at.
type Plugin struct {
	// Name is what a Topic calls the plugin: lowercase letters, digits and
	// single hyphens, up to 64 characters, like a Topic's id.
	Name string `json:"name"`
	// Command is the program, as an absolute path, and its arguments. It is
	// an argument list, never a shell string.
	Command []string `json:"command,omitempty"`
	// URL is the http or https address of a plugin the learner runs as a
	// service.
	URL string `json:"url,omitempty"`
	// AllowStudyHomeArguments records that the learner registered the
	// command although an argument names something inside the Study home,
	// which the agent can change.
	AllowStudyHomeArguments bool `json:"allow_study_home_arguments,omitempty"`
	// Problem says, in a listing, why study does not use the plugin as it
	// is registered, and what to do. It is never stored.
	Problem string `json:"problem,omitempty"`
}

// Codes of the registry's errors. They are the codes docs/cli.md documents,
// under the names the core gives them.
const (
	CodeInvalidArgument    = "invalid_argument"
	CodeAlreadyExists      = "already_exists"
	CodeNotFound           = "not_found"
	CodeNewerFormat        = "newer_format"
	CodeCorrupt            = "corrupt"
	CodeFailedPrecondition = "failed_precondition"
	CodeBusy               = "busy"
	CodeCanceled           = "canceled"
	CodeInternal           = "internal"
)

// Error is an error of the registry, with one of the documented codes.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode is the error's code. The core's CodeOf reads it, so the command
// line reports a registry error like any other.
func (e *Error) ErrorCode() string { return e.Code }

// Errorf returns an Error with the code and message given.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the code of err, or CodeInternal for an error that is not
// the registry's.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

func invalidf(format string, args ...any) *Error {
	return Errorf(CodeInvalidArgument, format, args...)
}

func notReadyf(format string, args ...any) *Error {
	return Errorf(CodeFailedPrecondition, format, args...)
}

func internalError(doing string, err error) *Error {
	return &Error{Code: CodeInternal, Message: fmt.Sprintf("%s: %v", doing, err), Err: err}
}
