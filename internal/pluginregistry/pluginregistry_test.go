package pluginregistry_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry"
	"github.com/mordor-forge/lamplight/v2/internal/pluginregistry/internal/regtest"
)

func envOf(c *regtest.Computer) pluginregistry.Env {
	return pluginregistry.Env{StudyHome: c.StudyHome(), Dir: c.Home, Getenv: c.Getenv}
}

func TestReadListsTheRegistryByName(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)

	list, err := pluginregistry.Read(ctx, envOf(c))
	if err != nil || list.Registry != c.Registry() || list.Plugins == nil || len(list.Plugins) != 0 {
		t.Fatalf("Read without a registry = %+v, %v", list, err)
	}
	c.WriteRegistry(t, `{"format": 1, "plugins": [
		{"name": "shelf", "command": ["/usr/local/bin/study-shelf", "--stdio"]},
		{"name": "remote", "url": "http://localhost:8765/mcp"}]}`)
	before := regtest.Snapshot(t, c.Base)
	list, err = pluginregistry.Read(ctx, envOf(c))
	want := pluginregistry.List{Registry: c.Registry(), Plugins: []pluginregistry.Plugin{
		{Name: "remote", URL: "http://localhost:8765/mcp"},
		{Name: "shelf", Command: []string{"/usr/local/bin/study-shelf", "--stdio"}},
	}}
	if err != nil || !reflect.DeepEqual(list, want) {
		t.Errorf("Read = %+v, %v; want %+v", list, err, want)
	}
	// It reads only: nothing is made, not even the registry's lock.
	if regtest.Snapshot(t, c.Base) != before {
		t.Error("reading wrote something")
	}

	stopped, stop := context.WithCancel(ctx)
	stop()
	if _, err := pluginregistry.Read(stopped, envOf(c)); err == nil {
		t.Error("Read went on after it was stopped")
	}
}

// What study does not use as it is registered, the listing says, each time
// it is read: the Study home can be another than when a plugin was
// registered, and a link can lead elsewhere.
func TestReadSaysWhatStudyDoesNotUse(t *testing.T) {
	ctx := context.Background()
	c := regtest.New(t)
	python := regtest.Program(t, filepath.Join(c.Bin, "python3"))
	inside := regtest.Program(t, filepath.Join(c.Study, "topic", "kb"))
	script := filepath.Join(c.Study, "topic", "kb.py")
	regtest.WriteFile(t, script, "print()")
	hop := regtest.Symlink(t, python, filepath.Join(c.Study, "topic", "hop"))
	through := regtest.Symlink(t, hop, filepath.Join(c.Bin, "through"))
	entry := func(name string, allow bool, command ...string) string {
		words := []string{}
		for _, w := range command {
			words = append(words, fmt.Sprintf("%q", w))
		}
		allowed := ""
		if allow {
			allowed = `, "allow_study_home_arguments": true`
		}
		return fmt.Sprintf(`{"name": %q, "command": [%s]%s}`, name, strings.Join(words, ", "), allowed)
	}
	c.WriteRegistry(t, `{"format": 1, "plugins": [`+strings.Join([]string{
		entry("a-fine", false, python, "-m", "kb", "--port=8765"),
		entry("b-program-inside", false, inside),
		entry("c-program-through", false, through, "serve"),
		entry("d-argument-inside", false, python, "-u", script),
		entry("e-argument-after-equals", false, python, "--script="+script),
		entry("f-argument-allowed", true, python, script),
		entry("g-program-inside-though-allowed", true, inside, script),
		entry("h-program-gone", false, filepath.Join(c.Bin, "gone"), "serve"),
		`{"name": "i-remote", "url": "http://localhost:8765/mcp"}`,
	}, ",")+`]}`)

	list, err := pluginregistry.Read(ctx, envOf(c))
	if err != nil || len(list.Plugins) != 9 {
		t.Fatalf("Read = %+v, %v", list, err)
	}
	for i, want := range []string{
		"",
		"its program is inside the Study home",
		"its program is inside the Study home",
		`argument 2 of its command, "` + script + `", names something inside the Study home`,
		`argument 1 of its command, "--script=` + script + `", names something inside the Study home`,
		"",
		"its program is inside the Study home",
		"",
		"",
	} {
		p := list.Plugins[i]
		if (want == "") != (p.Problem == "") || !strings.Contains(p.Problem, want) {
			t.Errorf("%s: problem %q; want %q", p.Name, p.Problem, want)
		}
		if p.Problem != "" && !strings.Contains(p.Problem, "study knowledge-base add "+p.Name+" --replace") {
			t.Errorf("%s: the problem does not say how to put it right: %q", p.Name, p.Problem)
		}

		// OpenProgram is what starting the plugin asks: it refuses what the
		// listing flags, and what is no program to start.
		file, err := pluginregistry.OpenProgram(envOf(c), p)
		switch {
		case i == 0 || i == 5:
			if err != nil {
				t.Errorf("%s: OpenProgram: %v", p.Name, err)
				continue
			}
			want, _ := os.Stat(python)
			if got, err := file.Stat(); err != nil || !os.SameFile(want, got) {
				t.Errorf("%s: OpenProgram opened another file than %s", p.Name, python)
			}
			if data, err := io.ReadAll(file); err != nil || !strings.HasPrefix(string(data), "#!/bin/sh") {
				t.Errorf("%s: the open program reads %q, %v", p.Name, data, err)
			}
			file.Close()
		case err == nil || file != nil:
			t.Errorf("%s: OpenProgram = %v, %v; want it refused", p.Name, file, err)
		}
	}
	// The problem is said, never stored.
	if strings.Contains(regtest.Text(t, c.Registry()), "problem") {
		t.Error("the registry holds a problem")
	}

	// With another Study home, the same registry has other problems.
	elsewhere := c.With(map[string]string{"STUDY_HOME": regtest.Mkdir(t, filepath.Join(c.Base, "elsewhere"))})
	list, err = pluginregistry.Read(ctx, envOf(elsewhere))
	if err != nil || len(list.Plugins) != 9 {
		t.Fatalf("Read = %+v, %v", list, err)
	}
	for _, p := range list.Plugins {
		if p.Problem != "" {
			t.Errorf("with the Study home elsewhere, %s has the problem %q", p.Name, p.Problem)
		}
	}
}

func TestCodeOf(t *testing.T) {
	c := regtest.New(t)
	c.WriteRegistry(t, `{"format": 2}`)
	_, err := pluginregistry.Read(context.Background(), envOf(c))
	if pluginregistry.CodeOf(err) != "newer_format" {
		t.Errorf("CodeOf(%v) = %s", err, pluginregistry.CodeOf(err))
	}
	if pluginregistry.CodeOf(io.EOF) != "internal" {
		t.Errorf("CodeOf of another error = %s", pluginregistry.CodeOf(io.EOF))
	}
}
