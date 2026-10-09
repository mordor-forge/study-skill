// Package registrar changes the Knowledge base plugin registry: it registers
// a plugin and removes one (ADR-0012).
//
// Only the command line imports it. Registering, changing and removing a
// plugin are the learner's to do, with study knowledge-base, and the MCP
// server runs outside the agent's sandbox: a tool that reached this package,
// directly or through a wrapper in the core, would let an agent choose a
// program for study to start there. So the core does not import it either,
// and a test in internal/mcpserver fails when the server's dependencies
// include it. Reading the registry is in the package above, which they do
// link.
package registrar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry"
	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regfile"
)

// Spec describes a Knowledge base plugin to register.
type Spec struct {
	Name string
	// Command is the program and its arguments. The program is a path, taken
	// from the folder study started in when it is relative, or a bare name
	// to find on PATH. Give a Command or a URL.
	Command []string
	URL     string
	// AllowStudyHomeArguments registers the command although an argument
	// names something inside the Study home, which the agent can change.
	AllowStudyHomeArguments bool
	// Replace allows the name to be registered already as something else,
	// which this registration then replaces.
	Replace bool
	DryRun  bool
}

// Registration reports a Knowledge base plugin registered.
type Registration struct {
	// Plugin is the plugin as registered: a command's program is the
	// absolute path it was resolved to.
	Plugin   pluginregistry.Plugin `json:"plugin"`
	Registry string                `json:"registry"`
	// Replaced is what the name stood for before, when Replace changed it.
	Replaced *pluginregistry.Plugin `json:"replaced,omitempty"`
	// Changed is false when the plugin was registered that way already, so
	// nothing was written.
	Changed bool `json:"changed"`
	DryRun  bool `json:"dry_run,omitempty"`
}

// Removal reports a Knowledge base plugin removed from the registry.
type Removal struct {
	// Plugin is what was registered under the name.
	Plugin   pluginregistry.Plugin `json:"plugin"`
	Registry string                `json:"registry"`
	DryRun   bool                  `json:"dry_run,omitempty"`
}

// pointLocked is where a test can interrupt a change, as a crash would: the
// registry's lock is held and the registry read, nothing written.
const pointLocked = "locked"

// Add registers a Knowledge base plugin on this computer under a name.
//
// A command's program is resolved to an absolute path, which is what the
// registry holds. It is refused when it is inside the Study home or is
// reached through it, and so is an argument that names something there
// unless the Spec allows it: study starts a plugin outside the agent's
// sandbox, so what it starts must be what the agent cannot change. An
// argument that names a file relative to the folder study started in is
// refused as well: the plugin is started from the registry's folder and
// would not find it. A URL must be http or https.
//
// Registering a plugin the way it is registered already changes nothing. A
// name that stands for something else is refused, unless the Spec says to
// replace it. It records no Event.
func Add(ctx context.Context, env pluginregistry.Env, spec Spec) (Registration, error) {
	return add(ctx, env, spec, nil)
}

func add(ctx context.Context, env pluginregistry.Env, spec Spec, hook func(point string) error) (Registration, error) {
	folder, err := regfile.Open(env)
	if err != nil {
		return Registration{}, err
	}
	defer folder.Close()
	plugin, err := newPlugin(env, folder.Walker(), spec)
	if err != nil {
		return Registration{}, err
	}
	out := Registration{Plugin: plugin, Registry: folder.Registry, DryRun: spec.DryRun}
	err = change(ctx, folder, spec.DryRun, hook, func(plugins []pluginregistry.Plugin) ([]pluginregistry.Plugin, bool, error) {
		out.Replaced, out.Changed = nil, false
		i := slices.IndexFunc(plugins, func(p pluginregistry.Plugin) bool { return p.Name == plugin.Name })
		switch {
		case i < 0:
			out.Changed = true
			return append(plugins, plugin), true, nil
		case plugins[i].URL == plugin.URL && slices.Equal(plugins[i].Command, plugin.Command) &&
			plugins[i].AllowStudyHomeArguments == plugin.AllowStudyHomeArguments:
			return nil, false, nil
		case !spec.Replace:
			return nil, false, regfile.Errorf(regfile.CodeAlreadyExists, "a Knowledge base plugin named %s is registered "+
				"already, as something else (study knowledge-base list shows it): pass --replace to register this one "+
				"in its place, or give this one another name", plugin.Name)
		}
		replaced := plugins[i]
		out.Replaced, out.Changed = &replaced, true
		plugins[i] = plugin
		return plugins, true, nil
	})
	if err != nil {
		return Registration{}, err
	}
	return out, nil
}

// Remove takes a Knowledge base plugin out of this computer's registry. A
// Topic that names it is afterwards treated as one without a Knowledge
// base. It records no Event.
func Remove(ctx context.Context, env pluginregistry.Env, name string, dryRun bool) (Removal, error) {
	return remove(ctx, env, name, dryRun, nil)
}

func remove(ctx context.Context, env pluginregistry.Env, name string, dryRun bool, hook func(point string) error) (Removal, error) {
	folder, err := regfile.Open(env)
	if err != nil {
		return Removal{}, err
	}
	defer folder.Close()
	if err := regfile.CheckName(name); err != nil {
		return Removal{}, err
	}
	out := Removal{Registry: folder.Registry, DryRun: dryRun}
	err = change(ctx, folder, dryRun, hook, func(plugins []pluginregistry.Plugin) ([]pluginregistry.Plugin, bool, error) {
		i := slices.IndexFunc(plugins, func(p pluginregistry.Plugin) bool { return p.Name == name })
		if i < 0 {
			return nil, false, regfile.Errorf(regfile.CodeNotFound, "no Knowledge base plugin named %s is registered on "+
				"this computer: study knowledge-base list shows the ones that are", name)
		}
		out.Plugin = plugins[i]
		return slices.Delete(plugins, i, i+1), true, nil
	})
	if err != nil {
		return Removal{}, err
	}
	return out, nil
}

// newPlugin checks a registration and returns the plugin as the registry
// will hold it.
func newPlugin(env pluginregistry.Env, w *regfile.Walker, spec Spec) (pluginregistry.Plugin, error) {
	none := pluginregistry.Plugin{}
	if err := regfile.CheckName(spec.Name); err != nil {
		return none, err
	}
	p := pluginregistry.Plugin{Name: spec.Name}
	switch {
	case len(spec.Command) > 0 && spec.URL != "":
		return none, regfile.Errorf(regfile.CodeInvalidArgument, "give Knowledge base plugin %s a command or a URL, not both", spec.Name)
	case len(spec.Command) > 0:
		program, err := w.ResolveProgram(env, spec.Command[0])
		if err != nil {
			return none, err
		}
		p.Command = append([]string{program}, spec.Command[1:]...)
		if err := regfile.CheckCommand(p.Command); err != nil {
			return none, err
		}
		if i, arg := regfile.RelativeArgument(env, p.Command); i > 0 {
			return none, regfile.Errorf(regfile.CodeInvalidArgument, "argument %d of the command, %s, names a file in %s, "+
				"the folder study was started in. A Knowledge base plugin is started from the registry's folder, not "+
				"from there, so it would not find the file: give the file's full path. If the argument is no file's "+
				"name, register the plugin from another folder", i, strconv.Quote(arg), env.Dir)
		}
		// The registration records that the learner allowed it only when
		// there was something to allow.
		if i, arg := w.InsideArgument(env, p.Command); i > 0 {
			if !spec.AllowStudyHomeArguments {
				return none, regfile.Errorf(regfile.CodeInvalidArgument, "argument %d of the command, %s, names something "+
					"inside the Study home, %s, or reached through it, where your agent can write. study starts a "+
					"Knowledge base plugin outside the agent's sandbox, so whatever the plugin loads from there, the "+
					"agent decides. Keep it outside the Study home; or, if the plugin only reads it as data, register "+
					"with --allow-study-home-arguments", i, strconv.Quote(arg), env.StudyHome)
			}
			p.AllowStudyHomeArguments = true
		}
	case spec.URL != "":
		if spec.AllowStudyHomeArguments {
			return none, regfile.Errorf(regfile.CodeInvalidArgument, "--allow-study-home-arguments goes with a command, and "+
				"Knowledge base plugin %s is given a URL", spec.Name)
		}
		url, err := regfile.CleanURL(spec.URL)
		if err != nil {
			return none, err
		}
		p.URL = url
	default:
		return none, regfile.Errorf(regfile.CodeInvalidArgument, "give Knowledge base plugin %s the command that starts "+
			"it, or the URL it answers at", spec.Name)
	}
	return p, nil
}

// change makes one change to the registry. plan is given the plugins
// registered, by name, and returns what the registry should hold instead,
// or false when nothing changes.
//
// It is planned once without the lock, and everything the change needs is
// looked at without making anything: that the registry it would write is
// one study reads back, that the folder can be written in or made, that the
// lock can be opened. So a change that cannot be made, one that changes
// nothing and a dry run all answer at once and alike, and leave neither a
// folder nor a lock file behind. A change is then planned again under the
// lock, against the registry as it is by then, and written by replacing the
// file, so a crash leaves the registry as it was or as it should be.
//
// Everything is done through the configuration folder as it was opened and
// checked, never through its name.
func change(ctx context.Context, folder *regfile.Folder, dryRun bool, hook func(string) error,
	plan func([]pluginregistry.Plugin) ([]pluginregistry.Plugin, bool, error)) error {
	err := planAndChange(ctx, folder, dryRun, hook, plan)
	if err != nil && ctx.Err() != nil {
		return &regfile.Error{Code: regfile.CodeCanceled, Err: ctx.Err(),
			Message: "the command was stopped before it changed the Knowledge base plugin registry, so nothing was changed"}
	}
	return err
}

func planAndChange(ctx context.Context, folder *regfile.Folder, dryRun bool, hook func(string) error,
	plan func([]pluginregistry.Plugin) ([]pluginregistry.Plugin, bool, error)) error {
	plugins, err := folder.Read()
	if err != nil {
		return err
	}
	next, changed, err := plan(plugins)
	if err != nil || !changed {
		return err
	}
	if _, err := regfile.Encode(folder.Registry, next); err != nil {
		return err
	}
	if err := canChange(folder); err != nil || dryRun {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := folder.Create(); err != nil {
		return err
	}
	dir := folder.Dir()
	unlock, err := lock(ctx, dir, folder.Path)
	if err != nil {
		return err
	}
	defer unlock()

	// And again under the lock, which is the plan that counts: another study
	// may have changed the registry while this one waited.
	if plugins, err = folder.Read(); err != nil {
		return err
	}
	if next, changed, err = plan(plugins); err != nil || !changed {
		return err
	}
	data, err := regfile.Encode(folder.Registry, next)
	if err != nil {
		return err
	}
	if hook != nil {
		if err := hook(pointLocked); err != nil {
			return err
		}
	}
	// Only the holder of the lock writes here, so a temporary file of the
	// registry's is what a study that crashed left.
	if entries, err := fs.ReadDir(dir.FS(), "."); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), tempPrefix) {
				_ = dir.Remove(e.Name())
			}
		}
	}
	return write(dir, folder.Registry, data)
}

// canChange says, without making anything, why a change would fail on what
// it finds on this computer: nothing when it would not. The real run asks it
// too, before it makes anything, so both report the same.
func canChange(folder *regfile.Folder) error {
	if !folder.Exists() {
		return folder.CanCreate()
	}
	if !regfile.Writable(folder.Dir()) {
		return regfile.Errorf(regfile.CodeFailedPrecondition, "%s is not a folder you can write in, so the Knowledge base "+
			"plugin registry in it cannot be changed: check the folder's permissions", folder.Path)
	}
	lockFile, err := openLock(folder.Dir(), folder.Path, false)
	if lockFile != nil {
		_ = lockFile.Close()
	}
	return err
}

// tempPrefix is how the temporary file a change writes begins. The file is
// hidden, and named as the core names its own.
const tempPrefix = "." + regfile.FileName + ".lamplight-tmp-"

// write replaces the registry in the open folder dir with data: it writes a
// temporary file, syncs it and renames it over the registry, so a reader
// sees the old registry or the new one, never a part. The registry is made
// for the learner alone to read: a plugin's URL may carry a key.
func write(dir *os.Root, registry string, data []byte) error {
	suffix := make([]byte, 8)
	_, _ = rand.Read(suffix) // crypto/rand.Read never returns an error.
	tmp := tempPrefix + hex.EncodeToString(suffix)
	f, err := dir.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return writeError(registry, err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = dir.Rename(tmp, regfile.FileName)
	}
	if werr != nil {
		_ = dir.Remove(tmp)
		return writeError(registry, werr)
	}
	// Best effort: some file systems cannot sync a folder, and the data
	// itself is synced.
	if d, err := dir.Open("."); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// writeError explains a write the operating system refused. What the
// learner can put right has advice; anything else is study's to explain.
func writeError(registry string, err error) error {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.ENOSPC) {
		return &regfile.Error{Code: regfile.CodeFailedPrecondition, Err: err, Message: "study could not write the Knowledge " +
			"base plugin registry, " + registry + " (" + regfile.Reason(err) + "), so nothing was changed: check that you " +
			"can write in its folder and that the disk has room"}
	}
	return &regfile.Error{Code: regfile.CodeInternal, Err: err, Message: "writing " + registry + ": " + err.Error()}
}

// lockWait is how long a change waits for another one under way before it
// gives up with busy.
const lockWait = 30 * time.Second

// lock takes the registry's lock, an advisory lock on a file beside it that
// the operating system releases if the process dies, waiting up to lockWait
// for another process to release it.
func lock(ctx context.Context, dir *os.Root, folderPath string) (unlock func(), err error) {
	f, err := openLock(dir, folderPath, true)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	wait := time.Millisecond
	for {
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, &regfile.Error{Code: regfile.CodeInternal, Err: err, Message: "locking the Knowledge base plugin " +
				"registry: " + err.Error()}
		}
		if locked {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, regfile.Errorf(regfile.CodeBusy, "another study process has been changing the Knowledge base "+
				"plugin registry for over %s: try again", lockWait)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(2*wait, 50*time.Millisecond)
	}
}

// openLock opens the registry's lock file in the open folder dir, and makes
// it when create says so; it returns nil when the file is not there and is
// not to be made. The lock is a regular file of its own. A symbolic link is
// not followed: one that led to the registry's own name would have study
// make an empty registry by opening its lock.
func openLock(dir *os.Root, folderPath string, create bool) (*os.File, error) {
	path := filepath.Join(folderPath, regfile.LockName)
	for range 5 {
		f, _, err := regfile.OpenRegular(dir, regfile.LockName, os.O_RDWR)
		switch {
		case errors.Is(err, regfile.ErrChanging):
			continue
		case errors.Is(err, regfile.ErrNotRegular):
			return nil, regfile.Errorf(regfile.CodeFailedPrecondition, "%s is not a regular file. study keeps the lock of "+
				"the Knowledge base plugin registry under that name, and follows no link to it: delete it, or move it "+
				"away", path)
		case err != nil:
			return nil, &regfile.Error{Code: regfile.CodeFailedPrecondition, Err: err, Message: "study cannot open " + path +
				", the lock of the Knowledge base plugin registry (" + regfile.Reason(err) + "): fix its permissions, " +
				"or delete it"}
		case f != nil || !create:
			return f, nil
		}
		// With O_EXCL nothing is followed and nothing is replaced: if a
		// name appeared meanwhile, look at it again.
		f, err = dir.OpenFile(regfile.LockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, &regfile.Error{Code: regfile.CodeFailedPrecondition, Err: err, Message: "study cannot make " + path +
				", the lock of the Knowledge base plugin registry (" + regfile.Reason(err) + "): check that you can " +
				"write in its folder"}
		}
	}
	return nil, regfile.Errorf(regfile.CodeBusy, "%s kept changing while study was opening it: try again", path)
}
