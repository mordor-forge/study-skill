package mcpserver_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The Knowledge base plugin registry says which program study starts for a
// plugin's name, or which address it contacts, and it is the learner's to
// change, with study knowledge-base (ADR-0012). The MCP server runs outside
// the agent's sandbox, so a tool that registered, changed or removed a
// plugin, or took a command line or a URL for one, would let the agent
// choose a program for study to run there.
//
// What keeps it from happening is how the program is built, and the first
// test below checks that: what changes the registry is in a package the
// server does not link, so no tool can reach it, however it is named and
// through however many wrappers. The other two are a second line. They look
// at names, and a name can be chosen to pass them.

const (
	module = "github.com/mordor-forge/lamplight/v2"
	// registrar is the one package that changes the registry, and
	// registryReader the one that reads it.
	registrar      = module + "/internal/pluginregistry/registrar"
	registryReader = module + "/internal/pluginregistry"
)

// goList runs go list from the module's root and returns its lines.
func goList(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("go list %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// TestOnlyTheCommandLineLinksWhatChangesThePluginRegistry lists what the
// server is built from. The package that registers and removes plugins is
// not in it, nor in the core, which the server links whole: a wrapper around
// it in either would not compile without importing it, and importing it
// fails this test. Only the command line imports it.
func TestOnlyTheCommandLineLinksWhatChangesThePluginRegistry(t *testing.T) {
	for _, pkg := range []string{"./internal/mcpserver", "./internal/core"} {
		deps := goList(t, "-deps", pkg)
		if slices.Contains(deps, registrar) {
			t.Errorf("%s links %s: nothing the MCP server is built from may change the Knowledge base plugin registry "+
				"(ADR-0012). Registering and removing a plugin stay in the command line", pkg, registrar)
		}
		// The list is what it seems: it holds what reads the registry.
		if !slices.Contains(deps, registryReader) || !slices.Contains(deps, module+"/internal/core") {
			t.Fatalf("go list -deps %s does not list %s, so this test proves nothing:\n%s", pkg, registryReader, strings.Join(deps, "\n"))
		}
	}
	// The package is there, under that name, and the command line links it.
	if deps := goList(t, "-deps", "./internal/cli"); !slices.Contains(deps, registrar) {
		t.Fatalf("the command line does not link %s: this test names the wrong package", registrar)
	}

	// And nothing but the command line imports it.
	var importers []string
	for _, line := range goList(t, "-f", `{{.ImportPath}} {{join .Imports ","}}`, "./...") {
		pkg, imports, _ := strings.Cut(line, " ")
		if slices.Contains(strings.Split(imports, ","), registrar) {
			importers = append(importers, strings.TrimPrefix(pkg, module+"/"))
		}
	}
	if !slices.Equal(importers, []string{"internal/cli"}) {
		t.Errorf("%s is imported by %v: only internal/cli may", registrar, importers)
	}
}

// words splits a name into its words, in lower case: at underscores, hyphens
// and dots, and where camelCase changes case, so that pluginCommand,
// plugin_command and PluginURL all hold the word plugin.
func words(name string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, strings.ToLower(string(word)))
			word = nil
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.':
			flush()
			continue
		case unicode.IsUpper(r) && i > 0:
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			// A capital after a small letter starts a word, and so does the
			// last capital of a run when a small letter follows: URLPath.
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()
	return out
}

func TestWords(t *testing.T) {
	for name, want := range map[string]string{
		"plugin_command":    "plugin command",
		"pluginCommand":     "plugin command",
		"PluginURL":         "plugin url",
		"pluginURLPath":     "plugin url path",
		"SetSourceReader":   "set source reader",
		"kb-plugin.name":    "kb plugin name",
		"knowledgeBase":     "knowledge base",
		"knowledge_base":    "knowledge base",
		"AddPlugin":         "add plugin",
		"HTTPEndpoint":      "http endpoint",
		"next_step":         "next step",
		"hours_per_week":    "hours per week",
		"PluginRegistryEnv": "plugin registry env",
		"args2":             "args2",
	} {
		if got := strings.Join(words(name), " "); got != want {
			t.Errorf("words(%q) = %q, want %q", name, got, want)
		}
	}
}

// pluginReads are the names about plugins that the server may use from the
// core: what reads the registry, and what a reading returns.
var pluginReads = []string{"Plugins", "Plugin", "PluginList"}

// aboutTheRegistry reports whether a name the server's source uses speaks of
// plugins or of the registry, and is not one of the names that only read.
func aboutTheRegistry(name string) bool {
	if slices.Contains(pluginReads, name) {
		return false
	}
	return slices.ContainsFunc(words(name), func(word string) bool {
		return slices.Contains([]string{"plugin", "plugins", "registry", "registrar", "registrars"}, word)
	})
}

// TestTheServerNamesNothingThatChangesThePluginRegistry reads the server's
// own source, as a second line behind the test above. The server reaches
// files only through the core, so it cannot write the registry itself; it
// imports nothing of the registry's packages; and it uses no name about
// plugins or the registry but the ones that only read.
func TestTheServerNamesNothingThatChangesThePluginRegistry(t *testing.T) {
	for _, name := range []string{"AddPlugin", "RemovePlugin", "PluginSpec", "PluginRegistryEnv", "registrar", "SetPluginReader", "pluginRegistry"} {
		if !aboutTheRegistry(name) {
			t.Fatalf("%s is not refused, so this test proves nothing", name)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "os" || p == "io/ioutil" || p == "os/exec" || p == "syscall" || strings.HasPrefix(p, "golang.org/x/sys") {
				t.Errorf("%s imports %s: the server reaches files only through the core, so that no tool can write "+
					"the Knowledge base plugin registry itself (ADR-0012)", name, p)
			}
			if strings.HasPrefix(p, registryReader) {
				t.Errorf("%s imports %s: the server asks the core about plugins, and has nothing to do with the "+
					"registry's own packages (ADR-0012)", name, p)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && aboutTheRegistry(sel.Sel.Name) {
				t.Errorf("%s uses %s: no tool registers, changes or removes a Knowledge base plugin (ADR-0012). "+
					"If %s only reads the registry, add it to pluginReads", name, sel.Sel.Name, sel.Sel.Name)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no source files were checked")
	}
}

// programOrAddressWords are the words, in the name of an input, that could
// mean what a plugin's registration holds: a program, its arguments or an
// address; or that say the input is about a plugin or the registry.
var programOrAddressWords = []string{
	"command", "commands", "cmd", "program", "exec", "executable", "binary", "bin", "argv", "args", "arguments",
	"shell", "script", "url", "urls", "uri", "endpoint", "address", "host", "port", "server",
	"plugin", "plugins", "registry", "kb",
}

// namesAProgramOrAnAddress reports whether an input's name holds one of
// those words, or speaks of the Knowledge base.
func namesAProgramOrAnAddress(name string) bool {
	parts := words(name)
	if strings.Contains(strings.Join(parts, ""), "knowledgebase") {
		return true
	}
	return slices.ContainsFunc(parts, func(word string) bool { return slices.Contains(programOrAddressWords, word) })
}

// programOrAddressInputs are the inputs with such a name that were looked at
// and are not a plugin's command line or URL: what each is, by tool and
// path.
var programOrAddressInputs = map[string]string{
	"source_add.url":              "the address of a web page that is a Source",
	"topic_update.knowledge_base": "the Topic's Knowledge base: its kind alone",
}

// TestNoToolTakesAPluginsCommandOrURL looks at what every tool accepts, as a
// second line too. An input whose name could be a program, an address or a
// plugin must be one this test knows, and every object a tool accepts must
// list its fields, so that none arrives under a name this test never saw.
// And no tool is named after the registry.
func TestNoToolTakesAPluginsCommandOrURL(t *testing.T) {
	tools, err := connect(t, t.TempDir()).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	inputs := 0
	for _, tool := range tools.Tools {
		if namesAProgramOrAnAddress(tool.Name) {
			t.Errorf("the server offers %s: the Knowledge base plugin registry has no tool (ADR-0012). "+
				"If the tool is about something else, teach this test the difference", tool.Name)
		}
		// The schema as a client receives it, whatever type the SDK holds.
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		walkInputs(t, tool.Name, schema, func(path string) {
			inputs++
			if !namesAProgramOrAnAddress(path[strings.LastIndexByte(path, '.')+1:]) {
				return
			}
			seen[path] = true
			if _, ok := programOrAddressInputs[path]; !ok {
				t.Errorf("%s is an input this test does not know. No tool takes a command line or a URL for a "+
					"Knowledge base plugin, and none registers, changes or removes one (ADR-0012): that is the "+
					"learner's to do, with study knowledge-base. If the input is something else, say what in "+
					"programOrAddressInputs", path)
			}
		})
	}
	// The schemas were read: these are inputs the server has had from the start.
	if inputs < 50 || !seen["source_add.url"] || !seen["topic_update.knowledge_base"] {
		t.Fatalf("%d inputs were looked at, and of the known ones only %v: the schemas are not read as this test expects", inputs, seen)
	}
	for path := range programOrAddressInputs {
		if !seen[path] {
			t.Errorf("programOrAddressInputs lists %s, which no tool has any more", path)
		}
	}
	// The check catches what it is for, however the name is cased, and
	// leaves ordinary names alone.
	for _, name := range []string{"command", "plugin_command", "pluginCommand", "PluginURL", "args", "program", "url", "plugin_url",
		"plugin", "endpoint", "knowledge_base", "knowledgeBase", "kb_plugin", "shellScript", "serverAddress"} {
		if !namesAProgramOrAnAddress(name) {
			t.Errorf("an input named %s would pass unnoticed", name)
		}
	}
	for _, name := range []string{"topic", "report", "support", "next_step", "learner_said", "hours_per_week", "dry_run", "reportedBy"} {
		if namesAProgramOrAnAddress(name) {
			t.Errorf("an input named %s is taken for a program or an address", name)
		}
	}
}

// walkInputs calls visit with the path of every field in a tool's input
// schema, such as topic_update.knowledge_base.kind, through objects and
// lists. It fails the test for an object that accepts fields it does not
// list, and for a schema it cannot follow.
func walkInputs(t *testing.T, path string, schema any, visit func(path string)) {
	t.Helper()
	s, ok := schema.(map[string]any)
	if !ok {
		t.Errorf("%s: its schema is %T, not an object", path, schema)
		return
	}
	for _, key := range []string{"$ref", "$defs", "anyOf", "oneOf", "allOf", "not", "if", "then", "else", "patternProperties",
		"dependentSchemas", "prefixItems", "unevaluatedProperties", "propertyNames"} {
		if _, ok := s[key]; ok {
			t.Errorf("%s: its schema uses %s, which this test does not follow: teach it to", path, key)
		}
	}
	props, _ := s["properties"].(map[string]any)
	if isType(s["type"], "object") || props != nil {
		if extra, ok := s["additionalProperties"]; !ok || extra != false {
			t.Errorf("%s accepts fields it does not list (additionalProperties is %v), so anything could arrive in it", path, extra)
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(path + "." + name)
		walkInputs(t, path+"."+name, props[name], visit)
	}
	if items, ok := s["items"]; ok {
		walkInputs(t, path, items, visit)
	}
}

// isType reports whether a schema's type is name, alone or among others.
func isType(v any, name string) bool {
	switch v := v.(type) {
	case string:
		return v == name
	case []any:
		return slices.Contains(v, any(name))
	}
	return false
}
