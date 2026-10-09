package core_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/core"
)

// registryCore returns a Core for a computer whose home folder, with
// Lamplight's configuration folder in it, is apart from its Study home, and
// where the Knowledge base plugin registry is on it.
func registryCore(t *testing.T, vars map[string]string) (c *core.Core, registry string) {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	home, study := filepath.Join(base, "home"), filepath.Join(base, "study")
	for _, dir := range []string{home, study} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"HOME": home, "STUDY_HOME": study}
	for key, value := range vars {
		env[key] = strings.ReplaceAll(value, "$STUDY", study)
	}
	c, err := core.Open(core.Options{Getenv: envOf(env), Dir: home, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(home, ".config", "lamplight", "knowledge-base-plugins.json")
}

func plantRegistry(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The core reads the Knowledge base plugin registry and has nothing that
// changes it: that is the command line's, in a package the core does not
// import.
func TestPluginsReadsTheRegistry(t *testing.T) {
	ctx := context.Background()
	c, registry := registryCore(t, nil)
	list, err := c.Plugins(ctx)
	if err != nil || list.Registry != registry || list.Plugins == nil || len(list.Plugins) != 0 {
		t.Fatalf("Plugins without a registry = %+v, %v", list, err)
	}
	plantRegistry(t, registry, `{"format": 1, "plugins": [
		{"name": "shelf", "command": ["/usr/local/bin/study-shelf", "--stdio"]},
		{"name": "remote", "url": "http://localhost:8765/mcp"}]}`)
	list, err = c.Plugins(ctx)
	want := []core.Plugin{
		{Name: "remote", URL: "http://localhost:8765/mcp"},
		{Name: "shelf", Command: []string{"/usr/local/bin/study-shelf", "--stdio"}},
	}
	if err != nil || !reflect.DeepEqual(list.Plugins, want) {
		t.Errorf("Plugins = %+v, %v; want %+v", list, err, want)
	}

	// Its errors carry the documented codes, as the core's own do.
	for content, code := range map[string]core.ErrorCode{
		`{"format": 2}`: core.CodeNewerFormat,
		`{`:             core.CodeCorrupt,
	} {
		plantRegistry(t, registry, content)
		if _, err := c.Plugins(ctx); core.CodeOf(err) != code {
			t.Errorf("Plugins with the registry %s = %v (%s); want %s", content, err, core.CodeOf(err), code)
		}
	}
}

// A registry the agent could write is not read: the core refuses it without
// reading, and everything else in the core works as before.
func TestPluginsRefusesARegistryInsideTheStudyHome(t *testing.T) {
	ctx := context.Background()
	c, _ := registryCore(t, map[string]string{"XDG_CONFIG_HOME": "$STUDY/.config"})
	planted := filepath.Join(c.Home(), ".config", "lamplight", "knowledge-base-plugins.json")
	plantRegistry(t, planted, `{"format": 1, "plugins": [{"name": "planted", "command": ["/bin/sh"]}]}`)
	list, err := c.Plugins(ctx)
	if core.CodeOf(err) != core.CodeFailedPrecondition || !strings.Contains(err.Error(), "is inside the Study home") ||
		strings.Contains(err.Error(), "planted") || len(list.Plugins) != 0 {
		t.Errorf("Plugins = %+v, %v (%s); want failed_precondition, unread", list, err, core.CodeOf(err))
	}
	if _, err := c.Status(ctx); err != nil {
		t.Errorf("Status: %v", err)
	}
}

// codedError is an error of a package the core builds on.
type codedError struct{ code string }

func (e codedError) Error() string     { return "coded: " + e.code }
func (e codedError) ErrorCode() string { return e.code }

func TestCodeOfReadsTheCodeAnErrorCarries(t *testing.T) {
	if got := core.CodeOf(codedError{"busy"}); got != core.CodeBusy {
		t.Errorf("CodeOf of an error with a code = %s", got)
	}
	if got := core.CodeOf(fmt.Errorf("while reading: %w", codedError{"corrupt"})); got != core.CodeCorrupt {
		t.Errorf("CodeOf of a wrapped error with a code = %s", got)
	}
	// The core's own code wins when its error wraps another.
	wrapped := &core.Error{Code: core.CodeInternal, Message: "x", Err: codedError{"busy"}}
	if got := core.CodeOf(wrapped); got != core.CodeInternal {
		t.Errorf("CodeOf of a core error that wraps one with a code = %s", got)
	}
	if got := core.CodeOf(os.ErrNotExist); got != core.CodeInternal {
		t.Errorf("CodeOf of a plain error = %s", got)
	}
}
