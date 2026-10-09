package regfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The registry's content: a format number, and the plugins as a list ordered
// by name.
//
//	{"format": 1, "plugins": [
//	  {"name": "shelf", "command": ["/usr/local/bin/study-shelf"]},
//	  {"name": "remote", "url": "http://localhost:8765/mcp"}]}
//
// Only study writes it, so the reader accepts exactly what the writer
// writes and nothing more. encoding/json alone is more forgiving than that:
// it matches keys whatever their letter case, takes the last of two keys
// with one name, reads null as nothing and turns bytes that are not text
// into a replacement character. Each of those would let a file mean one
// thing to this reader and another to the next, so the file is read into a
// plain tree first and checked key by key.

// damaged is the error for a registry this version cannot vouch for whole:
// where it is, what is wrong, and the two ways out.
func damaged(path, format string, args ...any) *Error {
	return Errorf(CodeCorrupt, "%s is damaged (%s): fix it by hand, or delete it and register your Knowledge base plugins "+
		"again with study knowledge-base add", path, fmt.Sprintf(format, args...))
}

// object is a JSON object as it is written: its keys in order, and each
// key's value. twice names a key that is there more than once.
type object struct {
	values map[string]any
	twice  string
}

// maxDepth is how deep the registry's JSON may nest. The registry needs
// four levels.
const maxDepth = 16

// tree is a JSON document read as it is written, and what in it this reader
// does not take: the first key given twice and the first null.
type tree struct {
	dec     *json.Decoder
	problem string
}

func (t *tree) note(format string, args ...any) {
	if t.problem == "" {
		t.problem = fmt.Sprintf(format, args...)
	}
}

func (t *tree) value(depth int) (any, error) {
	tok, err := t.dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		if tok == nil {
			t.note("it holds a null")
		}
		return tok, nil
	}
	if depth >= maxDepth {
		return nil, errors.New("it nests deeper than a registry does")
	}
	switch delim {
	case '{':
		obj := &object{values: map[string]any{}}
		for t.dec.More() {
			keyTok, err := t.dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := keyTok.(string)
			val, err := t.value(depth + 1)
			if err != nil {
				return nil, err
			}
			if _, seen := obj.values[key]; seen {
				if obj.twice == "" {
					obj.twice = key
				}
				t.note("it has the key %q twice", key)
			}
			obj.values[key] = val
		}
		_, err := t.dec.Token()
		return obj, err
	case '[':
		list := []any{}
		for t.dec.More() {
			val, err := t.value(depth + 1)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		_, err := t.dec.Token()
		return list, err
	}
	return nil, fmt.Errorf("unexpected %v", delim)
}

// Decode reads a registry's content and returns its plugins, ordered by
// name. path names the registry in errors.
//
// The format is read first, on its own: a newer version of study may have
// changed the shape of everything else, and a registry this version cannot
// read for that reason is newer, not damaged. Only when the format is one
// this version knows is anything else judged. A registry it cannot vouch
// for whole is damaged, and none of it is used.
func Decode(path string, data []byte) ([]Plugin, error) {
	if !utf8.Valid(data) {
		return nil, damaged(path, "it is not valid UTF-8 text")
	}
	t := &tree{dec: json.NewDecoder(bytes.NewReader(data))}
	t.dec.UseNumber()
	root, err := t.value(0)
	if err == nil {
		if _, more := t.dec.Token(); more != io.EOF {
			err = errors.New("more follows the registry")
		}
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, damaged(path, "it is not JSON: %v", err)
	}
	top, ok := root.(*object)
	if !ok {
		return nil, damaged(path, "it is not a JSON object")
	}

	if top.twice == "format" {
		return nil, damaged(path, "it gives its format twice")
	}
	number, ok := top.values["format"].(json.Number)
	if !ok {
		return nil, damaged(path, "it has no format number")
	}
	format, ok := wholeNumber(number)
	if !ok || format < 1 {
		return nil, damaged(path, "its format, %s, is not a whole number from 1 up", number)
	}
	if format > Format {
		return nil, Errorf(CodeNewerFormat, "%s has format %s, but this version of study only understands format %d: upgrade study",
			path, number, Format)
	}

	if t.problem != "" {
		return nil, damaged(path, "%s", t.problem)
	}
	if key := unknownKey(top, "format", "plugins"); key != "" {
		return nil, damaged(path, "it has a key this version of study does not know, %q", key)
	}
	entries, ok := top.values["plugins"].([]any)
	if !ok {
		return nil, damaged(path, "it has no list of plugins")
	}
	plugins := make([]Plugin, 0, len(entries))
	seen := map[string]bool{}
	for i, entry := range entries {
		obj, ok := entry.(*object)
		if !ok {
			return nil, damaged(path, "entry %d of its plugins is not an object", i+1)
		}
		p, why := decodeEntry(obj)
		if why != "" {
			if p.Name != "" {
				return nil, damaged(path, "%s: %s", p.Name, why)
			}
			return nil, damaged(path, "entry %d of its plugins: %s", i+1, why)
		}
		if seen[p.Name] {
			return nil, damaged(path, "it registers %s twice", p.Name)
		}
		seen[p.Name] = true
		plugins = append(plugins, p)
	}
	slices.SortFunc(plugins, func(a, b Plugin) int { return strings.Compare(a.Name, b.Name) })
	return plugins, nil
}

// wholeNumber reads a JSON number written as digits alone. One too large to
// matter is larger than any format this version knows.
func wholeNumber(n json.Number) (int, bool) {
	s := n.String()
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	if len(s) > 9 {
		return 1 << 30, true
	}
	v, err := strconv.Atoi(s)
	return v, err == nil
}

// unknownKey returns a key of obj that is none of the known ones, spelled
// exactly as they are.
func unknownKey(obj *object, known ...string) string {
	keys := make([]string, 0, len(obj.values))
	for key := range obj.values {
		if !slices.Contains(known, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// decodeEntry reads one plugin of the registry and checks it as a
// registration is checked. why says what is wrong with it; the plugin's name
// is returned whenever it is a name.
func decodeEntry(obj *object) (p Plugin, why string) {
	name, ok := obj.values["name"].(string)
	if !ok || name == "" {
		return p, "it has no name"
	}
	if err := CheckName(name); err != nil {
		return p, err.Error()
	}
	p.Name = name
	if key := unknownKey(obj, "name", "command", "url", "allow_study_home_arguments"); key != "" {
		return p, fmt.Sprintf("it has a key this version of study does not know, %q", key)
	}
	command, hasCommand := obj.values["command"]
	url, hasURL := obj.values["url"]
	allow, hasAllow := obj.values["allow_study_home_arguments"]
	switch {
	case hasCommand && hasURL:
		return p, "it has both a command and a URL"
	case hasCommand:
		words, ok := command.([]any)
		if !ok {
			return p, "its command is not a list of words"
		}
		for _, word := range words {
			s, ok := word.(string)
			if !ok {
				return p, "its command is not a list of words"
			}
			p.Command = append(p.Command, s)
		}
		if len(p.Command) == 0 {
			return p, "its command is empty"
		}
		// As study writes it: absolute, and with nothing a folder's name
		// would resolve otherwise than the operating system does.
		if program := p.Command[0]; !filepath.IsAbs(program) || filepath.Clean(program) != program {
			return p, "its program is not an absolute path as study writes one"
		}
		if err := CheckCommand(p.Command); err != nil {
			return p, err.Error()
		}
		if hasAllow {
			if p.AllowStudyHomeArguments, ok = allow.(bool); !ok {
				return p, "allow_study_home_arguments is neither true nor false"
			}
		}
	case hasURL:
		s, ok := url.(string)
		if !ok || s == "" {
			return p, "its URL is not text"
		}
		clean, err := CleanURL(s)
		if err != nil {
			return p, err.Error()
		}
		if clean != s {
			return p, "its URL is not written as study writes one"
		}
		p.URL = s
		if hasAllow {
			return p, "allow_study_home_arguments goes with a command, and it has a URL"
		}
	default:
		return p, "it has neither a command nor a URL"
	}
	return p, ""
}

// file is the registry as it is written.
type file struct {
	Format  int     `json:"format"`
	Plugins []entry `json:"plugins"`
}

type entry struct {
	Name                    string   `json:"name"`
	Command                 []string `json:"command,omitempty"`
	URL                     string   `json:"url,omitempty"`
	AllowStudyHomeArguments bool     `json:"allow_study_home_arguments,omitempty"`
}

// Encode writes a registry holding plugins, ordered by name, and refuses
// one this version would not read back: a change must never leave a
// registry that the next command calls damaged. path names the registry in
// errors.
func Encode(path string, plugins []Plugin) ([]byte, error) {
	out := file{Format: Format, Plugins: make([]entry, 0, len(plugins))}
	for _, p := range plugins {
		out.Plugins = append(out.Plugins, entry{Name: p.Name, Command: p.Command, URL: p.URL,
			AllowStudyHomeArguments: p.AllowStudyHomeArguments})
	}
	slices.SortFunc(out.Plugins, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, internalError("encoding the Knowledge base plugin registry", err)
	}
	if buf.Len() > MaxBytes {
		return nil, notReadyf("with this plugin the Knowledge base plugin registry, %s, would be larger than %d bytes, "+
			"which is more than study reads: remove a plugin you no longer use, or shorten the command", path, MaxBytes)
	}
	if _, err := Decode(path, buf.Bytes()); err != nil {
		return nil, &Error{Code: CodeInternal, Err: err, Message: "study would write a Knowledge base plugin registry it " +
			"could not read back, so it wrote nothing: " + err.Error()}
	}
	return buf.Bytes(), nil
}
