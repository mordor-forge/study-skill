package regfile

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const regPath = "/home/ada/.config/lamplight/knowledge-base-plugins.json"

// registryFor is a registry in this version's format with the entries given
// as JSON.
func registryFor(entries string) string {
	return `{"format": 1, "plugins": [` + entries + `]}`
}

func TestDecodeReadsWhatEncodeWrites(t *testing.T) {
	plugins := []Plugin{
		{Name: "shelf", Command: []string{"/usr/local/bin/study-shelf", "--recipe", "embedding gemma", "", "a=<b>&c"}},
		{Name: "remote", URL: "http://localhost:8765/mcp?key=a&b=<c>"},
		{Name: "notes", Command: []string{"/opt/kb/bin/notes-kb", "/home/ada/study/notes"}, AllowStudyHomeArguments: true},
	}
	data, err := Encode(regPath, plugins)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "format": 1,
  "plugins": [
    {
      "name": "notes",
      "command": [
        "/opt/kb/bin/notes-kb",
        "/home/ada/study/notes"
      ],
      "allow_study_home_arguments": true
    },
    {
      "name": "remote",
      "url": "http://localhost:8765/mcp?key=a&b=<c>"
    },
    {
      "name": "shelf",
      "command": [
        "/usr/local/bin/study-shelf",
        "--recipe",
        "embedding gemma",
        "",
        "a=<b>&c"
      ]
    }
  ]
}
`
	if string(data) != want {
		t.Errorf("Encode:\n%s\nwant:\n%s", data, want)
	}
	got, err := Decode(regPath, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Plugin{plugins[2], plugins[1], plugins[0]}) {
		t.Errorf("Decode = %+v", got)
	}

	for _, content := range []string{
		`{"format":1,"plugins":[]}`,
		"{\"format\": 1,\n \"plugins\": []}\n\n",
		`{"plugins": [], "format": 1}`,
		registryFor(`{"name": "shelf", "command": ["/bin/sh", "-c", "exit 0"], "allow_study_home_arguments": false}`),
		registryFor(`{"url": "http://[::1]:8765/mcp", "name": "v6"}`),
	} {
		if _, err := Decode(regPath, []byte(content)); err != nil {
			t.Errorf("Decode(%s): %v", content, err)
		}
	}
}

// The format is read before anything else is judged, so a registry of a
// newer version is newer whatever else it holds.
func TestDecodeRefusesANewerFormatBeforeAnythingElse(t *testing.T) {
	for _, content := range []string{
		`{"format": 2, "plugins": [{"name": "shelf", "command": ["/usr/bin/study-shelf"]}]}`,
		`{"format": 2, "plugins": {"shelf": {"run": "/usr/bin/study-shelf serve"}}}`,
		`{"format": 3, "plugins": "shelf=/usr/bin/study-shelf"}`,
		`{"format": 2, "version": "2.1", "entries": [{"id": 7}]}`,
		`{"format": 2, "plugins": [{"name": "Shelf", "program": "study-shelf", "url": "unix:///run/kb"}]}`,
		`{"plugins": [{"name": 1}], "format": 9}`,
		`{"plugins":"zzz","format":2}`,
		// What this version would call damaged in a registry of its own.
		`{"format": 2, "plugins": [null], "Plugins": null, "a": 1, "a": 2}`,
		`{"format": 9223372036854775807, "plugins": []}`,
		`{"format": 99999999999999999999, "plugins": []}`,
	} {
		_, err := Decode(regPath, []byte(content))
		if CodeOf(err) != CodeNewerFormat || !strings.Contains(err.Error(), regPath) || !strings.Contains(err.Error(), "upgrade study") {
			t.Errorf("Decode(%s) = %v (%s); want newer_format", content, err, CodeOf(err))
		}
	}
}

func TestDecodeCallsDamagedWhatItCannotVouchForWhole(t *testing.T) {
	sh := `{"name": "a", "command": ["/bin/sh"`
	for _, tc := range []struct{ name, content, want string }{
		{"empty", "", "it is not JSON"},
		{"whitespace", "  \n", "it is not JSON"},
		{"not JSON", "format = 1\n[plugins.shelf]\n", "it is not JSON"},
		{"cut short", `{"format": 1, "plugins": [{"name": "shelf"`, "it is not JSON"},
		{"a comma too many", `{"format": 1, "plugins": [],}`, "it is not JSON"},
		{"two registries", `{"format": 1, "plugins": []} {"format": 1, "plugins": []}`, "it is not JSON"},
		{"something after it", `{"format": 1, "plugins": []} x`, "it is not JSON"},
		{"a byte order mark", "\xef\xbb\xbf" + `{"format": 1, "plugins": []}`, "it is not JSON"},
		{"a NUL after it", `{"format": 1, "plugins": []}` + "\x00", "it is not JSON"},
		{"nested without end", `{"format": 1, "plugins": ` + strings.Repeat("[", 5000) + strings.Repeat("]", 5000) + `}`, "nests deeper"},
		{"a list", `[{"name": "shelf", "command": ["/usr/bin/study-shelf"]}]`, "not a JSON object"},
		{"null", `null`, "not a JSON object"},
		{"a number", `1`, "not a JSON object"},
		{"text", `"x"`, "not a JSON object"},

		{"no format", `{"plugins": []}`, "no format number"},
		{"an empty object", `{}`, "no format number"},
		{"format 0", `{"format": 0, "plugins": []}`, "not a whole number from 1 up"},
		{"a negative format", `{"format": -1, "plugins": []}`, "not a whole number from 1 up"},
		{"a format that is text", `{"format": "1", "plugins": []}`, "no format number"},
		{"a newer format that is text", `{"format": "2", "plugins": []}`, "no format number"},
		{"a format that is null", `{"format": null, "plugins": []}`, "no format number"},
		{"a format that is true", `{"format": true, "plugins": []}`, "no format number"},
		{"a format with a fraction", `{"format": 1.5, "plugins": []}`, "not a whole number"},
		{"a format written 1.0", `{"format": 1.0, "plugins": []}`, "not a whole number"},
		{"a format written 2.0", `{"format": 2.0, "plugins": []}`, "not a whole number"},
		{"a format written 1e0", `{"format": 1e0, "plugins": []}`, "not a whole number"},
		{"a format written 1e400", `{"format": 1e400, "plugins": []}`, "not a whole number"},
		// Which of two is the format? Neither: a reader that took the last
		// would rewrite a newer registry as its own.
		{"the format twice, the known one last", `{"format": 2, "format": 1, "plugins": []}`, "gives its format twice"},
		{"the format twice, the newer one last", `{"format": 1, "format": 2, "plugins": []}`, "gives its format twice"},
		{"the format in another letter case as well", `{"format": 1, "FORMAT": 2, "plugins": []}`, `does not know, "FORMAT"`},
		{"keys in another letter case", `{"FORMAT": 1, "Plugins": [{"NAME": "a", "URL": "http://h/"}]}`, "no format number"},
		{"an entry's keys in another letter case", registryFor(`{"Name": "a", "url": "http://h/"}`), "it has no name"},
		{"an entry's other keys in another letter case", registryFor(`{"name": "a", "URL": "http://h/"}`), `does not know, "URL"`},

		{"a key this version does not know", `{"format": 1, "plugins": [], "default": "shelf"}`, `does not know, "default"`},
		{"a key with an escape sequence in it", `{"format": 1, "plugins": [], "\u001b[31mred": 1}`, `does not know, "\x1b[31mred"`},
		{"an entry with a key this version does not know",
			registryFor(`{"name": "shelf", "command": ["/usr/bin/study-shelf"], "env": {"LD_PRELOAD": "/tmp/x.so"}}`), `does not know, "env"`},
		{"an entry with a problem", registryFor(`{"name": "a", "url": "http://h/", "problem": "x"}`), `does not know, "problem"`},
		{"a key twice", `{"format": 1, "plugins": [], "plugins": []}`, `has the key "plugins" twice`},
		{"a key twice in an entry", registryFor(`{"name": "a", "url": "http://h/", "url": "http://i/"}`), `has the key "url" twice`},
		{"a name twice in an entry", registryFor(`{"name": "a", "name": "b", "url": "http://h/"}`), `has the key "name" twice`},

		{"no plugins", `{"format": 1}`, "no list of plugins"},
		{"plugins that are null", `{"format": 1, "plugins": null}`, "holds a null"},
		{"plugins by name", `{"format": 1, "plugins": {"shelf": {"command": ["/usr/bin/study-shelf"]}}}`, "no list of plugins"},
		{"plugins that are text", `{"format": 1, "plugins": "x"}`, "no list of plugins"},
		{"a plugin that is null", `{"format": 1, "plugins": [null]}`, "holds a null"},
		{"a plugin that is a number", `{"format": 1, "plugins": [1]}`, "entry 1 of its plugins is not an object"},
		{"a plugin that is a list", `{"format": 1, "plugins": [[]]}`, "entry 1 of its plugins is not an object"},

		{"an entry without a name", registryFor(`{"command": ["/usr/bin/study-shelf"]}`), "entry 1 of its plugins: it has no name"},
		{"a name that is a number", registryFor(`{"name": 1, "url": "http://h/"}`), "it has no name"},
		{"a name that is not one", registryFor(`{"name": "../shelf", "command": ["/usr/bin/study-shelf"]}`), "not a valid name"},
		{"a name with a capital", registryFor(`{"name": "Shelf", "url": "http://h/"}`), "not a valid name"},
		{"a name with an accent", registryFor(`{"name": "shélf", "url": "http://h/"}`), "not a valid name"},
		{"a name with an escape sequence", registryFor(`{"name": "a\u001b[2Jb", "url": "http://localhost/"}`), "not a valid name"},
		{"a name with a NUL", registryFor(`{"name": "a\u0000", "url": "http://h/"}`), "not a valid name"},
		{"a name too long", registryFor(`{"name": "` + strings.Repeat("a", 65) + `", "url": "http://h/"}`), "not a valid name"},
		{"the name none", registryFor(`{"name": "none", "url": "http://localhost/"}`), "kind of Knowledge base"},
		{"the name plugin", registryFor(`{"name": "plugin", "url": "http://localhost/"}`), "kind of Knowledge base"},
		{"a name twice", registryFor(`{"name": "shelf", "url": "http://localhost/a"}, {"name": "shelf", "url": "http://localhost/b"}`), "registers shelf twice"},
		{"a name twice, once escaped", registryFor(`{"name": "a", "url": "http://h/"}, {"name": "a", "url": "http://i/"}`), "registers a twice"},

		{"neither a command nor a URL", registryFor(`{"name": "shelf"}`), "shelf: it has neither a command nor a URL"},
		{"both a command and a URL", registryFor(`{"name": "shelf", "command": ["/usr/bin/study-shelf"], "url": "http://localhost/"}`), "both a command and a URL"},
		{"an empty command", registryFor(`{"name": "shelf", "command": []}`), "its command is empty"},
		{"an empty command and a URL", registryFor(`{"name": "a", "command": [], "url": "http://h/"}`), "both a command and a URL"},
		{"a command that is null, and a URL", registryFor(`{"name": "a", "command": null, "url": "http://h/"}`), "holds a null"},
		{"a command that is one string", registryFor(`{"name": "shelf", "command": "/usr/bin/study-shelf serve"}`), "not a list of words"},
		{"a command of numbers", registryFor(`{"name": "a", "command": [1]}`), "not a list of words"},
		{"a command that is null in one word", registryFor(`{"name": "a", "command": [null]}`), "holds a null"},
		// Read as an empty argument, it would be written back as one.
		{"an argument that is null", registryFor(sh + `, null]}`), "holds a null"},
		{"a program that is a bare name", registryFor(`{"name": "shelf", "command": ["study-shelf"]}`), "not an absolute path"},
		{"a program that is a relative path", registryFor(`{"name": "shelf", "command": ["./study-shelf", "serve"]}`), "not an absolute path"},
		{"a program from the home folder", registryFor(`{"name": "a", "command": ["~/sh"]}`), "not an absolute path"},
		{"an empty program", registryFor(`{"name": "shelf", "command": ["", "serve"]}`), "not an absolute path"},
		// By name these lead to /usr/bin/sh; what the operating system runs
		// depends on what /usr/local is.
		{"a program by a path with .. in it", registryFor(`{"name": "a", "command": ["/usr/local/../bin/sh"]}`), "not an absolute path as study writes one"},
		{"a program by a path with a doubled slash", registryFor(`{"name": "a", "command": ["/usr/bin//sh"]}`), "not an absolute path as study writes one"},
		{"a program by a path with . in it", registryFor(`{"name": "a", "command": ["/usr/./bin/sh"]}`), "not an absolute path as study writes one"},
		{"a program by a path that ends in a slash", registryFor(`{"name": "a", "command": ["/usr/bin/sh/"]}`), "not an absolute path as study writes one"},
		{"a program with a NUL", registryFor(`{"name": "a", "command": ["/bin/sh\u0000x"]}`), "the program's path contains a control character"},
		{"an argument with an escape sequence", registryFor(sh + `, "\u001b[2J"]}`), "argument 1 of the command contains a control character"},
		{"an argument with a NUL", registryFor(sh + `, "a\u0000b"]}`), "argument 1 of the command contains a control character"},
		{"an argument that reorders text", registryFor(sh + `, "a\u202eb"]}`), "bidirectional control character (U+202E)"},
		{"an argument with a left-to-right mark", registryFor(sh + `, "a\u200eb"]}`), "bidirectional control character (U+200E)"},
		{"an argument with a right-to-left mark", registryFor(sh + `, "ok", "a\u200fb"]}`), "argument 2 of the command contains a bidirectional control character (U+200F)"},
		{"an argument with an Arabic letter mark", registryFor(sh + `, "a\u061cb"]}`), "bidirectional control character (U+061C)"},
		// Bytes that are no text would be read as U+FFFD and written back so.
		{"an argument that is not text", registryFor(sh + `, "a` + "\xff\xfe" + `b"]}`), "not valid UTF-8"},
		{"a program that is not text", registryFor(`{"name": "a", "command": ["/bin/k` + "\xff" + `b"]}`), "not valid UTF-8"},
		{"half a surrogate pair in an argument", registryFor(sh + `, "\ud800"]}`), "replacement character"},
		{"the replacement character in an argument", registryFor(sh + `, "a` + "�" + `b"]}`), "replacement character"},
		{"an argument too long", registryFor(sh + `, "` + strings.Repeat("x", 4097) + `"]}`), "longer than 4096"},
		{"too many arguments", registryFor(sh + strings.Repeat(`, "x"`, 101) + `]}`), "more than 100 arguments"},
		{"allowed arguments that are text", registryFor(sh + `], "allow_study_home_arguments": "yes"}`), "neither true nor false"},
		{"allowed arguments with a URL", registryFor(`{"name": "a", "url": "http://h/", "allow_study_home_arguments": true}`), "goes with a command"},

		{"a URL that is a number", registryFor(`{"name": "a", "url": 1}`), "its URL is not text"},
		{"an empty URL", registryFor(`{"name": "a", "url": ""}`), "its URL is not text"},
		{"a URL that is not http", registryFor(`{"name": "shelf", "url": "file:///etc/passwd"}`), "not a web address"},
		{"an ftp URL", registryFor(`{"name": "a", "url": "ftp://h/"}`), "not a web address"},
		{"a unix URL", registryFor(`{"name": "a", "url": "unix:///run/x.sock"}`), "not a web address"},
		{"a javascript URL", registryFor(`{"name": "a", "url": "javascript:alert(1)"}`), "not a web address"},
		{"a URL with a password", registryFor(`{"name": "shelf", "url": "http://ada:secret@localhost/"}`), "user name or password"},
		{"a URL with an escape sequence", registryFor(`{"name": "a", "url": "http://h/\u001b[31m"}`), "control character"},
		{"a URL with a fragment", registryFor(`{"name": "a", "url": "http://h/mcp#x"}`), "#fragment"},
		{"a URL with port 0", registryFor(`{"name": "a", "url": "http://h:0/mcp"}`), "port"},
		{"a URL with a port too high", registryFor(`{"name": "a", "url": "http://h:65536/mcp"}`), "port"},
		// A registry holds a URL as study cleans it, or study did not write it.
		{"a URL in capitals", registryFor(`{"name": "a", "url": "HTTP://H:80/x"}`), "not written as study writes one"},
		{"a URL with spaces around it", registryFor(`{"name": "a", "url": "  http://h/  "}`), "not written as study writes one"},
		{"a URL with its default port", registryFor(`{"name": "a", "url": "https://h:443/x"}`), "not written as study writes one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plugins, err := Decode(regPath, []byte(tc.content))
			if CodeOf(err) != CodeCorrupt || !strings.Contains(err.Error(), tc.want) || plugins != nil {
				t.Fatalf("Decode = %v, %v (%s); want corrupt with %q", plugins, err, CodeOf(err), tc.want)
			}
			// The advice: where the file is, and the two ways out.
			for _, advice := range []string{regPath, "fix it by hand", "delete it", "study knowledge-base add"} {
				if !strings.Contains(err.Error(), advice) {
					t.Errorf("the error lacks %q: %v", advice, err)
				}
			}
			// Nothing of the file reaches a terminal as it is, a secret
			// in a URL least of all.
			if strings.ContainsFunc(err.Error(), func(r rune) bool { return r < ' ' || r == 0x7f || r == 0x202e || r == 0x200e }) {
				t.Errorf("the error carries a control character: %q", err.Error())
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("the error repeats a password: %v", err)
			}
		})
	}
}

// What Encode writes, Decode reads: a change must never leave a registry
// that the next command calls damaged. At the limits of a registration that
// means refusing to write.
func TestEncodeRefusesWhatDecodeWouldNot(t *testing.T) {
	// One plugin at the limits of a command: 100 arguments of 4,096
	// characters, each of them three bytes long.
	huge := Plugin{Name: "huge", Command: []string{"/bin/sh"}}
	for range MaxArgs {
		huge.Command = append(huge.Command, strings.Repeat("語", MaxWordRunes))
	}
	if err := CheckCommand(huge.Command); err != nil {
		t.Fatalf("a command at the limits is refused, so this test proves nothing: %v", err)
	}
	data, err := Encode(regPath, []Plugin{huge})
	if CodeOf(err) != CodeFailedPrecondition || !strings.Contains(err.Error(), "larger than 1048576 bytes") ||
		!strings.Contains(err.Error(), "shorten the command") || data != nil {
		t.Errorf("Encode of a registry over the limit = %d bytes, %v; want it refused with advice", len(data), err)
	}

	// Many plugins of about 2,000 bytes each: 480 fit, and are read back;
	// 560 do not, and are not written.
	var plugins []Plugin
	for i := range 560 {
		plugins = append(plugins, Plugin{Name: fmt.Sprintf("p%d", i), URL: "http://localhost:8765/" + strings.Repeat("x", 1900)})
	}
	data, err = Encode(regPath, plugins[:480])
	if err != nil || len(data) > MaxBytes || len(data) < MaxBytes*8/10 {
		t.Fatalf("Encode of 480 plugins = %d bytes, %v; want them to fit, near the limit", len(data), err)
	}
	if got, err := Decode(regPath, data); err != nil || len(got) != 480 {
		t.Fatalf("Decode of 480 plugins = %d, %v", len(got), err)
	}
	if data, err := Encode(regPath, plugins); CodeOf(err) != CodeFailedPrecondition || data != nil {
		t.Fatalf("Encode of 560 plugins = %d bytes, %v; want it refused", len(data), err)
	}

	// And nothing Decode would call damaged, whatever it is asked to write.
	for _, p := range []Plugin{
		{Name: "Shelf", URL: "http://localhost/"},
		{Name: "a", Command: []string{"bin/sh"}},
		{Name: "a", Command: []string{"/bin/sh", "a\xffb"}},
		{Name: "a", URL: "HTTP://LOCALHOST/"},
		{Name: "a"},
	} {
		if data, err := Encode(regPath, []Plugin{p}); err == nil {
			t.Errorf("Encode(%+v) wrote %s", p, data)
		}
	}
}
