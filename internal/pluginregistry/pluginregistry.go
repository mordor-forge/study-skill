// Package pluginregistry reads the Knowledge base plugin registry
// (ADR-0012): the Knowledge base plugins this computer has, each under a
// name, with the command that starts it or the URL it answers at. A Topic
// names a plugin and never says what the name stands for, so nothing a Topic
// holds, which syncs between computers and which the agent can write,
// decides which program study starts.
//
// The registry is one file in Lamplight's configuration folder, beside
// config.toml and outside the Study home. It is this computer's and the
// learner's to change. It is no Topic's state, so changing it records no
// Event.
//
// This package only reads. What changes the registry is in the package
// registrar, below this one, which the command line alone links: the core
// and the MCP server link this package and cannot reach the other, so no
// wrapper in either can register a plugin for an agent. A test in
// internal/mcpserver keeps it so. What the two share, the open
// configuration folder included, is in an internal package neither the core
// nor the server can import.
package pluginregistry

import (
	"context"
	"os"
	"strconv"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regfile"
)

type (
	// Env is what the registry needs to know of one run of study: the Study
	// home, the folder study started in, and the environment.
	Env = regfile.Env
	// Plugin is a Knowledge base plugin registered on this computer.
	Plugin = regfile.Plugin
	// Error is an error of the registry, with one of the codes docs/cli.md
	// documents. The core's CodeOf reads the code.
	Error = regfile.Error
)

// List is the Knowledge base plugins registered on this computer, ordered by
// name.
type List struct {
	// Registry is the file that holds them, whether or not it exists yet.
	Registry string   `json:"registry"`
	Plugins  []Plugin `json:"plugins"`
}

// Read lists the Knowledge base plugins registered on this computer.
//
// The registry is read only from where the agent cannot write it: when
// Lamplight's configuration folder is the Study home, is inside it or is
// reached through it, Read fails and reads nothing.
//
// A plugin study does not use as it is registered carries a Problem: its
// program, or an argument the learner did not allow, is inside the Study
// home this study uses or is reached through it. That is checked at each
// reading, because the Study home can be another than when the plugin was
// registered, and a link can lead elsewhere.
func Read(ctx context.Context, env Env) (List, error) {
	if err := ctx.Err(); err != nil {
		return List{}, err
	}
	folder, err := regfile.Open(env)
	if err != nil {
		return List{}, err
	}
	defer folder.Close()
	plugins, err := folder.Read()
	if err != nil {
		return List{}, err
	}
	list := List{Registry: folder.Registry, Plugins: []Plugin{}}
	for _, p := range plugins {
		p.Problem = problem(folder.Walker(), env, p)
		list.Plugins = append(list.Plugins, p)
	}
	return list, nil
}

// problem says why study does not use a plugin as it is registered, or
// nothing when it does.
func problem(w *regfile.Walker, env Env, p Plugin) string {
	if len(p.Command) == 0 {
		return ""
	}
	if regfile.IsInside(w.CheckProgram(p.Command[0])) {
		return "its program is inside the Study home, " + env.StudyHome + ", or is reached through it, where your agent " +
			"can write, so study does not use this plugin: install the program outside the Study home, then register " +
			"it again with study knowledge-base add " + p.Name + " --replace -- <command>"
	}
	if p.AllowStudyHomeArguments {
		return ""
	}
	if i, arg := w.InsideArgument(env, p.Command); i > 0 {
		return "argument " + strconv.Itoa(i) + " of its command, " + strconv.Quote(arg) + ", names something inside the " +
			"Study home, " + env.StudyHome + ", or reached through it, where your agent can write, and the plugin was " +
			"not registered to allow that, so study does not use it: register it again with study knowledge-base add " +
			p.Name + " --replace, without that argument or with --allow-study-home-arguments"
	}
	return ""
}

// OpenProgram opens the program of a plugin registered by a command, to
// start it, and checks it at that moment: the program is a regular file that
// the user study runs as may run, reached without going through the Study
// home at any hop, and no argument the learner did not allow names something
// there. A Study home that another study made a moment ago counts.
//
// Start the file that is returned, not the path the plugin is registered
// by: on Linux, by its descriptor (/proc/self/fd). The path must not be
// resolved again after this check. Between the check and a second
// resolution the name can come to lead elsewhere, and the program that runs
// would be one nobody checked. The caller closes the file.
func OpenProgram(env Env, p Plugin) (*os.File, error) {
	if len(p.Command) == 0 {
		return nil, regfile.Errorf(regfile.CodeInvalidArgument, "Knowledge base plugin %s is reached by URL: it has no "+
			"program to start", p.Name)
	}
	w := regfile.NewWalker(env.StudyHome)
	defer w.Close()
	file, err := w.OpenProgram(p.Command[0])
	if err != nil {
		return nil, err
	}
	if why := problem(w, env, p); why != "" {
		_ = file.Close()
		return nil, regfile.Errorf(regfile.CodeFailedPrecondition, "Knowledge base plugin %s is not started: %s", p.Name, why)
	}
	return file, nil
}

// CodeOf returns the code of a registry error, or "internal" for any other
// error.
func CodeOf(err error) string { return regfile.CodeOf(err) }
