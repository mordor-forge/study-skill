//go:build unix

package shelf_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"

	"github.com/mordor-forge/lamplight/v2/internal/plugincontract"
	"github.com/mordor-forge/lamplight/v2/internal/plugintest"
	"github.com/mordor-forge/lamplight/v2/internal/shelf"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/shelftest"
)

const module = "github.com/mordor-forge/lamplight/v2"

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

// TestStudyLinksNothingOfShelf lists what study is built from. Shelf is a
// second program (ADR-0012): study starts it and speaks the contract with
// it, and links none of its code, nor SQLite, nor the PDF reader, nor what
// reads web pages. A package of study's that came to import any of them
// would put a document parser inside the program that holds the learner's
// records, and inside the MCP server, which runs outside the agent's
// sandbox.
func TestStudyLinksNothingOfShelf(t *testing.T) {
	// What only Shelf is built from: its own packages, and each library it
	// brought into the module.
	shelfs := []string{
		module + "/internal/shelf",
		module + "/cmd/study-shelf",
		"modernc.org/",                  // SQLite, and the C library it stands on
		"github.com/klippa-app/",        // PDFium
		"github.com/tetratelabs/wazero", // the WebAssembly runtime PDFium runs in
		"codeberg.org/readeck/",         // the readable text of a web page
		"golang.org/x/net/html",         // the HTML it is read from
	}
	// of names which of them a package is, or is under.
	of := func(dep string) string {
		for _, prefix := range shelfs {
			if dep == prefix || strings.HasPrefix(dep, strings.TrimSuffix(prefix, "/")+"/") {
				return prefix
			}
		}
		return ""
	}
	for _, pkg := range []string{"./cmd/study", "./internal/mcpserver", "./internal/core", "./internal/cli", "./internal/pluginclient"} {
		deps := goList(t, "-deps", pkg)
		for _, dep := range deps {
			if prefix := of(dep); prefix != "" {
				t.Errorf("%s links %s: study links nothing of Shelf's (%s). Shelf is the study-shelf program, which study starts "+
					"and speaks the Knowledge base plugin contract with (ADR-0012)", pkg, dep, prefix)
			}
		}
		// The list is what it seems: it holds the contract both speak.
		if !slices.Contains(deps, module+"/internal/plugincontract") {
			t.Fatalf("go list -deps %s does not list the contract's package, so this test proves nothing:\n%s", pkg, strings.Join(deps, "\n"))
		}
	}
	// And the names above are the ones Shelf is built from: each is in what
	// study-shelf links.
	deps := goList(t, "-deps", "./cmd/study-shelf")
	for _, prefix := range shelfs {
		if !slices.ContainsFunc(deps, func(dep string) bool { return of(dep) == prefix }) {
			t.Errorf("study-shelf links nothing under %s: this test names what Shelf no longer uses, or names it wrongly", prefix)
		}
	}
	// Nothing but Shelf and its own tests imports Shelf's packages.
	for _, line := range goList(t, "-f", `{{.ImportPath}} {{join .Imports ","}} {{join .TestImports ","}} {{join .XTestImports ","}}`, "./...") {
		pkg, imports, _ := strings.Cut(line, " ")
		if strings.HasPrefix(pkg, module+"/internal/shelf") || pkg == module+"/cmd/study-shelf" {
			continue
		}
		for _, imported := range strings.FieldsFunc(imports, func(r rune) bool { return r == ',' || r == ' ' }) {
			// The end-to-end tests build study-shelf, and install it.
			if strings.HasPrefix(imported, module+"/internal/shelf") && !(pkg == module+"/internal/e2e" && imported == module+"/internal/shelf/shelftest") {
				t.Errorf("%s imports %s: only Shelf imports Shelf", strings.TrimPrefix(pkg, module+"/"), imported)
			}
		}
	}
}

// The program the other tests start is study-shelf, built from this
// checkout: it says so, on its standard output and nowhere else, and exits.
func TestTheBuiltStudyShelfRuns(t *testing.T) {
	cmd := exec.Command(shelftest.Shelf(t), "--version")
	cmd.Env = []string{"HOME=" + t.TempDir()}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.String() != "study-shelf version dev\n" || stderr.Len() != 0 {
		t.Errorf("study-shelf --version: %v\nstdout: %q\nstderr: %q", err, stdout.String(), stderr.String())
	}
}

// Registered with --stdio, as docs/cli.md's examples register it, study-shelf
// is the same plugin: the flag says outright how it is reached.
func TestStudyShelfTakesStdioSaidOutright(t *testing.T) {
	link := plugintest.AsCommand(plugintest.NewComputer(t), shelftest.Shelf(t), "--stdio")(t)
	s := dial(t, link.Transport)
	var contract plugincontract.Contract
	s.ok(plugincontract.ToolContract, struct{}{}, &contract)
	if contract.Version != plugincontract.Version {
		t.Errorf("contract = %+v", contract)
	}
	if got := s.cs.InitializeResult().ServerInfo; got.Name != "study-shelf" || got.Version != "dev" {
		t.Errorf("it calls itself %+v, want study-shelf, in a version without a v before it", got)
	}
}

// A learner's home may lie below folders they can pass through and not list,
// as /home with mode 711 is on a shared computer. study starts a plugin
// there, and Shelf keeps its index there: it opens its own folder, and asks
// to list none above it.
func TestShelfWorksBelowFoldersThatCannotBeListed(t *testing.T) {
	computer := plugintest.NewComputer(t)
	plugintest.PassThrough(t, computer.Base(), computer.Home)
	s, _ := run(t, computer)
	book := file(t, "book.a1", pdf([]string{"A book on a shared computer speaks of sheaves."}))
	s.indexed("shared-computer", book)
	if found := s.search("shared-computer", "sheaves", 5); len(found) != 1 || found[0].Location == nil || found[0].Location.First != 1 {
		t.Errorf("found %+v", found)
	}
	// Started again, it finds its index where it left it.
	again, _ := run(t, computer)
	if now := again.status("shared-computer")[book.ID]; now.State != plugincontract.StateIndexed || now.Hash != book.Hash {
		t.Errorf("a study-shelf started again says %+v", now)
	}
}

// whole checks, through a Shelf that was started after another was stopped,
// that a Source is in the index all of it or not at all: listed as indexed,
// with every one of its pages found, or not listed, with none.
func whole(t *testing.T, s *session, collection string, book plugincontract.Source, pages int, word string) (indexed bool) {
	t.Helper()
	status, listed := s.status(collection)[book.ID]
	if listed && status.State != plugincontract.StateIndexed {
		t.Fatalf("a Shelf that was just started says %+v of a Source nobody asked it to index", status)
	}
	found := 0
	for _, n := range []int{1, pages / 4, pages / 2, pages - 1, pages} {
		w := fmt.Sprintf("%s%d", word, n)
		found += len(holding(s.search(collection, w, 5, book.ID), w+" "))
	}
	switch {
	case listed && (status.Hash != book.Hash || found != 5):
		t.Fatalf("the Source is listed as indexed (%+v), and a search finds %d of 5 of its pages: half a Source is found as if it were whole", status, found)
	case !listed && found != 0:
		t.Fatalf("the Source is not listed, and a search finds %d of its pages", found)
	}
	return listed
}

// integrity has SQLite check a collection's file.
func integrity(t *testing.T, folder, collection string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(folder, collection+".sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Errorf("the integrity check of %s says %q (%v)", collection, result, err)
	}
	var passages, texts int
	if err := db.QueryRow("SELECT (SELECT count(*) FROM passages), (SELECT count(*) FROM passages_text)").Scan(&passages, &texts); err != nil || passages != texts {
		t.Errorf("%s holds %d Passages and %d texts (%v)", collection, passages, texts, err)
	}
	// A Source that is said to have vectors has one for each of its
	// Passages, and no vector is of a Source that is said to have none.
	var short, stray int
	if err := db.QueryRow(`SELECT
		(SELECT count(*) FROM embeddings e WHERE 4 * e.dimensions * (SELECT count(*) FROM passages p WHERE p.source = e.source)
			!= (SELECT coalesce(sum(length(v.vectors)), 0) FROM vectors v WHERE v.source = e.source)),
		(SELECT count(*) FROM vectors WHERE source NOT IN (SELECT source FROM embeddings))`).Scan(&short, &stray); err != nil || short != 0 || stray != 0 {
		t.Errorf("%s holds %d Sources whose vectors are not one for each Passage, and %d rows of vectors that are no Source's (%v)",
			collection, short, stray, err)
	}
}

// study-shelf is killed while it indexes a book, as a kill -9 or a power cut
// would kill it, at several moments: before it has read the book, while it
// reads it, and while it writes it into the index. What was indexed before
// is there for the next study-shelf, the book is there whole or not at all,
// and indexed again it is whole.
//
// With an embedding endpoint, most of the work is asking for the Passages'
// vectors, and the kill comes there too: the book is in the index with a
// vector for each of its Passages, or not at all.
func TestAStudyShelfThatIsKilledLeavesItsIndexWhole(t *testing.T) {
	// Beside the conformance suite: each of the two below has a computer of
	// its own, and nothing here is measured by the clock.
	t.Parallel()
	for name, tc := range map[string]struct {
		embedding bool
		// How far the indexing is when the kill comes: at once, in the
		// reading, among the requests for vectors when there are any, and
		// at its end, which is where the writing begins.
		until []float64
	}{
		"by keyword alone":           {false, []float64{-1, 0.4, 0.95, 0.95}},
		"with an embedding endpoint": {true, []float64{-1, 0.05, 0.5, 0.98, 0.98}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			killedAtSeveralMoments(t, tc.embedding, tc.until)
		})
	}
}

func killedAtSeveralMoments(t *testing.T, embedding bool, moments []float64) {
	const pages = 1500
	computer := plugintest.NewComputer(t)
	folder := computer.DataFolder("conformance")
	var at *shelftest.Endpoint
	if embedding {
		at = shelftest.NewEndpoint(t)
		writeSettings(t, computer, at, "embeddinggemma-2", false)
	}
	kept := file(t, "kept.a1", pdf([]string{"A book that was indexed before speaks of determinants."}))
	book := file(t, "long.b2", long(pages, "page"))
	// vectors checks that a Source has a vector for each of its Passages,
	// or none: the long book's Passages are its pages.
	vectors := func(source plugincontract.Source, indexed bool, passages int) {
		t.Helper()
		got, want := vectorsOf(t, folder, "crash", source.ID).count, 0
		if embedding && indexed {
			want = passages
		}
		if got != want {
			t.Fatalf("the index holds %d vectors for %s (indexed: %v), want %d: a Source has a vector for each Passage, or none", got, source.ID, indexed, want)
		}
	}

	s, _ := run(t, computer)
	s.indexed("crash", kept)
	vectors(kept, true, 1)

	indexedOnce := false
	for round, until := range moments {
		s, command := run(t, computer)
		if whole(t, s, "crash", book, pages, "page") {
			// A kill came after the book was in the index. From nothing
			// again: under another id the same book is new.
			indexedOnce = true
			book.ID = fmt.Sprintf("long.c%d", round)
		}
		s.index("crash", book)
		for deadline := time.Now().Add(patience); until >= 0 && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			now, listed := s.status("crash")[book.ID]
			if !listed || now.State != plugincontract.StateIndexing || (now.Progress != nil && *now.Progress >= until) {
				break
			}
		}
		command.Kill()
		// The kill is asked for, and the program is gone a moment later:
		// until then it still holds its lock, and the next study-shelf
		// would rightly say that the book is being indexed.
		command.Stop()
		integrity(t, folder, "crash")

		next, _ := run(t, computer)
		if now := next.status("crash")[kept.ID]; now.State != plugincontract.StateIndexed || now.Hash != kept.Hash {
			t.Fatalf("round %d: after study-shelf was killed, the next says %+v of a Source that was indexed before", round, now)
		}
		if found := next.search("crash", "determinants", 5, kept.ID); len(found) != 1 {
			t.Fatalf("round %d: after study-shelf was killed, what was indexed before is no longer found", round)
		}
		vectors(kept, true, 1)
		vectors(book, whole(t, next, "crash", book, pages, "page"), pages)
	}
	// Asked again, the book is indexed, all of it.
	s, _ = run(t, computer)
	s.indexed("crash", book)
	if !whole(t, s, "crash", book, pages, "page") {
		t.Error("indexed again after the kills, the book is not in the index")
	}
	vectors(book, true, pages)
	integrity(t, folder, "crash")
	if indexedOnce {
		t.Log("one kill came after the book was written: the index held it whole")
	}
	if embedding {
		// The kills came while this endpoint was being asked, or about to
		// be: it is the one the settings name, asked as they say.
		heldToSettings(t, at, "google/embeddinggemma-2", "[input model]")
	}
}

// study-shelf takes what a learner wrote in its settings file: the endpoint,
// the Recipe's model and marks, the key. With another Recipe in the file,
// the next study-shelf says what that makes of what is indexed, and
// indexing again puts it right. Without the file it searches by keyword.
func TestTheBuiltStudyShelfDoesWhatItsSettingsFileSays(t *testing.T) {
	computer := plugintest.NewComputer(t)
	folder := computer.DataFolder("conformance")
	at := shelftest.NewEndpoint(t)
	at.Same("automobile", "car")
	settings := writeSettings(t, computer, at, "embeddinggemma-2", false)

	s, command := run(t, computer)
	var contract plugincontract.Contract
	s.ok(plugincontract.ToolContract, struct{}{}, &contract)
	if fmt.Sprint(contract.Search) != "[keyword embedding]" {
		t.Errorf("with an endpoint in its settings file, study-shelf says it searches by %v", contract.Search)
	}
	book := file(t, "book.a1", pdf([]string{"A garden needs water and sunlight."}, []string{"The car needs fuel and a road."}))
	s.indexed("settings", book)
	if got := s.find("settings", "automobile", 1); len(got.Passages) != 1 || !strings.Contains(got.Passages[0].Text, "The car needs fuel") ||
		fmt.Sprint(got.Search) != "[keyword embedding]" || got.Notice != "" {
		t.Errorf("a search by what a word means finds %q by %v, with the notice %q", texts(got.Passages), got.Search, got.Notice)
	}
	asked := at.Asked()
	if len(asked) != 2 || fmt.Sprint(asked[0].Texts()) != "[title: The book.a1 book | text: A garden needs water and sunlight. title: The book.a1 book | text: The car needs fuel and a road.]" ||
		fmt.Sprint(asked[1].Texts()) != "[task: search result | query: automobile]" {
		t.Fatalf("the endpoint was asked %+v; want the two Passages, and then the question, each marked as the Recipe marks it", asked)
	}
	for n, a := range asked {
		if a.Path != "/v1/embeddings" || a.Key != testKey || a.Agent != "study-shelf/dev" || a.Body["model"] != "google/embeddinggemma-2" || fmt.Sprint(a.Keys()) != "[input model]" {
			t.Errorf("request %d went to %s as %q, with the key %q: %v", n, a.Path, a.Agent, a.Key, a.Body)
		}
	}
	if got, want := vectorsOf(t, folder, "settings", book.ID), (kept{"google/embeddinggemma-2", "title: {title} | text: {text}", 512, 2}); got != want {
		t.Errorf("the collection keeps %+v for the book, want %+v", got, want)
	}
	command.Stop()
	if said := command.Wrote(); said != "" {
		t.Errorf("study-shelf wrote on its standard error: %q", said)
	}

	// The learner's server now applies the model's prompts itself, and they
	// name the Recipe for that. The vectors that are there were made of
	// text Shelf marked: the next study-shelf says so, and indexing again
	// has the server mark the Passages.
	at.Prompts("query", "task: search result | query: ", "document", "title: none | text: ")
	writeSettings(t, computer, at, "embeddinggemma-2-prompts", false)
	at.Forget()
	s, _ = run(t, computer)
	if now := s.status("settings")[book.ID]; now.State != plugincontract.StateOutdated || now.Hash != book.Hash ||
		!strings.Contains(now.Reason, "marked for the model google/embeddinggemma-2 in another way") {
		t.Errorf("after the Recipe was changed, study-shelf says %+v of the book", now)
	}
	s.indexed("settings", book)
	if got := s.find("settings", "automobile", 1); len(got.Passages) != 1 || !strings.Contains(got.Passages[0].Text, "The car needs fuel") || got.Notice != "" {
		t.Errorf("with the server marking the texts, a search finds %q, with the notice %q", texts(got.Passages), got.Notice)
	}
	asked = at.Asked()
	if len(asked) != 2 {
		t.Fatalf("the endpoint was asked %d times, want once for the Passages and once for the question", len(asked))
	}
	for n, want := range []struct{ prompt, texts string }{
		{"document", "[A garden needs water and sunlight. The car needs fuel and a road.]"},
		{"query", "[automobile]"},
	} {
		if a := asked[n]; fmt.Sprint(a.Keys()) != "[dimensions input model prompt]" || a.Body["prompt"] != want.prompt ||
			a.Body["dimensions"] != float64(512) || fmt.Sprint(a.Texts()) != want.texts || a.Key != testKey {
			t.Errorf("request %d is %v, with the key %q; want the prompt %s, 512 numbers, and the texts as they are", n, a.Body, a.Key, want.prompt)
		}
	}

	// Without the file, study-shelf is the one that searches by keyword,
	// and has nothing to say of vectors.
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	at.Forget()
	s, _ = run(t, computer)
	s.ok(plugincontract.ToolContract, struct{}{}, &contract)
	if fmt.Sprint(contract.Search) != "[keyword]" {
		t.Errorf("without a settings file, study-shelf says it searches by %v", contract.Search)
	}
	if now := s.status("settings")[book.ID]; now.State != plugincontract.StateIndexed {
		t.Errorf("without a settings file, study-shelf says %+v of the book", now)
	}
	if got := s.find("settings", "fuel", 1); len(got.Passages) != 1 || fmt.Sprint(got.Search) != "[keyword]" {
		t.Errorf("without a settings file, a search finds %q by %v", texts(got.Passages), got.Search)
	}
	if asked := at.Asked(); len(asked) != 0 {
		t.Errorf("without a settings file, study-shelf asked the endpoint %+v", asked)
	}
}

// A settings file study-shelf cannot use ends it at once, with the reason
// last on its standard error, where study reads it, and never on its
// standard output, which is study's. It does not search by keyword instead:
// the learner who wrote an endpoint into the file would think it in use.
// Nothing of the file is repeated, since a line of it may be the key.
func TestStudyShelfDoesNotStartOnSettingsItCannotUse(t *testing.T) {
	const good = "format = 1\n[embedding]\nendpoint = \"http://127.0.0.1:1/v1\"\nrecipe = \"embeddinggemma-2\"\nkey = \"" + testKey + "\"\n"
	for name, tc := range map[string]struct {
		text string
		// inFolder writes the file in the folder study-shelf keeps its
		// index in, and links to it from where the settings are.
		inFolder bool
		want     []string
	}{
		"a key that is not closed":   {"format = 1\n[embedding]\nendpoint = \"http://127.0.0.1:1/v1\"\nrecipe = \"embeddinggemma-2\"\nkey = \"" + testKey + "\n", false, []string{"is not valid TOML at line 5"}},
		"a Recipe it does not have":  {strings.Replace(good, "embeddinggemma-2", "embeddinggemma-9", 1), false, []string{"names a Recipe this Shelf does not have", "embeddinggemma-2 and embeddinggemma-2-prompts"}},
		"a newer format":             {strings.Replace(good, "format = 1", "format = 2", 1), false, []string{"is in format 2", "this one reads format 1", "update Lamplight"}},
		"no format":                  {strings.TrimPrefix(good, "format = 1\n"), false, []string{"does not say which format"}},
		"a file in the index folder": {good, true, []string{"is inside the folder Shelf keeps its index in, or the folder of the plugins' folders above it"}},
	} {
		t.Run(name, func(t *testing.T) {
			computer := plugintest.NewComputer(t)
			folder := computer.DataFolder("conformance")
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(computer.ConfigDir, "shelf.toml")
			if tc.inFolder {
				planted := filepath.Join(folder, "planted.toml")
				if err := os.WriteFile(planted, []byte(tc.text), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(planted, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(shelftest.Shelf(t))
			cmd.Env = []string{"HOME=" + computer.Home, plugincontract.DataFolderEnv + "=" + folder}
			cmd.Dir = computer.ConfigDir
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				t.Fatalf("study-shelf ended with %v, want it to exit with 1\nstderr: %s", err, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("study-shelf wrote on its standard output, which is study's: %q", stdout.String())
			}
			said := stderr.String()
			for _, word := range append(tc.want, "study-shelf: Shelf's settings file, "+path) {
				if !strings.Contains(said, word) {
					t.Errorf("it does not say %q: %s", word, said)
				}
			}
			if strings.Contains(said, testKey) {
				t.Errorf("it repeats the key: %s", said)
			}
			// study shows the last 600 bytes of what a plugin wrote: the
			// reason fits.
			if len(said) > 600 {
				t.Errorf("it wrote %d bytes, more than study repeats: %s", len(said), said)
			}
			if left, _ := os.ReadDir(folder); len(left) != map[bool]int{false: 0, true: 1}[tc.inFolder] {
				t.Errorf("a study-shelf that did not start left %v in its folder", left)
			}
			// Asked what version it is, it still says: that reads no
			// settings.
			version := exec.Command(shelftest.Shelf(t), "--version")
			version.Env = cmd.Env
			if out, err := version.Output(); err != nil || string(out) != "study-shelf version dev\n" {
				t.Errorf("study-shelf --version: %q, %v", out, err)
			}
		})
	}
}

// study keeps no plugin running: it starts a study-shelf for every question
// it has, a glance at what is indexed among them, while the study-shelf that
// a study source index started in another terminal is at work. The one that
// is asked says what the other is doing: that the Source is being indexed,
// how far, and what a search still finds of it meanwhile. And once the other
// was killed, it says so no longer: a Source that was being indexed by a
// program that is gone is not being indexed.
func TestAGlanceSeesWhatAnotherStudyShelfIsIndexing(t *testing.T) {
	const pages = 3000
	computer := plugintest.NewComputer(t)
	earlier := file(t, "book.a1", pdf([]string{"The first edition speaks of spectra."}))
	later := earlier
	data := long(pages, "leaf")
	later.Path = filepath.Join(t.TempDir(), "second-edition.pdf")
	if err := os.WriteFile(later.Path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	later.Hash = hashOf(data)
	never := file(t, "new.b2", long(pages, "sheet"))

	indexer, command := run(t, computer)
	indexer.indexed("glanced", earlier)
	// New content of the book is asked for.
	indexer.index("glanced", later)

	// Glances, each a study-shelf of its own, until one has seen the work
	// say how far it is.
	var seen plugincontract.SourceStatus
	for deadline := time.Now().Add(patience); seen.Progress == nil || *seen.Progress == 0; {
		glance, stop := run(t, computer)
		status := glance.status("glanced")
		now, listed := status[earlier.ID]
		if !listed || now.State != plugincontract.StateIndexing {
			t.Fatalf("a glance says %+v (listed: %v) of a Source another study-shelf is indexing; want it being indexed", now, listed)
		}
		// What a search finds meanwhile is the earlier content, and the
		// glance gives its hash.
		if now.Hash != earlier.Hash {
			t.Fatalf("a glance gives the hash %q for a Source whose earlier content is still found, want %s", now.Hash, earlier.Hash)
		}
		if found := glance.search("glanced", "spectra", 5); len(found) != 1 {
			t.Fatalf("while another study-shelf indexes new content, a glance finds the earlier content %d times", len(found))
		}
		// It is not this one's to stop.
		if glance.cancel("glanced", earlier.ID) {
			t.Fatal("a study-shelf says it stopped what another is indexing")
		}
		seen = now
		stop.Stop()
		if time.Now().After(deadline) {
			t.Fatalf("no glance saw how far the indexing is: the last said %+v", now)
		}
	}
	if *seen.Progress <= 0 || *seen.Progress >= 1 {
		t.Errorf("a glance says the work is %v done", *seen.Progress)
	}
	// A Source that was never indexed is asked for while the book is being
	// read: it waits for the one reader, and a glance says it is being
	// indexed too, with no hash, since nothing of it is found yet. It is
	// asked for only now, with the reader taken: asked for at once, it
	// could be the one to get the reader, and be indexed before a glance
	// looked.
	indexer.index("glanced", never)
	waiting, stop := run(t, computer)
	if other, listed := waiting.status("glanced")[never.ID]; !listed || other.State != plugincontract.StateIndexing || other.Hash != "" {
		t.Fatalf("a glance says %+v (listed: %v) of a Source another study-shelf is about to index", other, listed)
	}
	stop.Stop()

	// The one that indexes is killed. Once it is gone, no Source is being
	// indexed.
	command.Kill()
	command.Stop()
	glance, _ := run(t, computer)
	status := glance.status("glanced")
	if now := status[earlier.ID]; now.State != plugincontract.StateIndexed || now.Hash != earlier.Hash {
		t.Errorf("after the study-shelf that indexed was killed, a glance says %+v of the Source; want its earlier content indexed", now)
	}
	if now, listed := status[never.ID]; listed {
		t.Errorf("after the study-shelf that indexed was killed, a glance says %+v of a Source that never was indexed; want nothing", now)
	}
	// Asked, this one indexes them, and says so itself.
	glance.indexed("glanced", later)
	glance.indexed("glanced", never)
	if found := glance.search("glanced", "leaf3000 sheet3000", 5); len(found) != 2 {
		t.Errorf("indexed after the kill, a search finds %d of the two last pages", len(found))
	}
	integrity(t, computer.DataFolder("conformance"), "glanced")
}

// A Source that waits for its turn is being indexed, for every copy of
// Shelf, from the moment index answers: a study status in another terminal,
// while a study source index has more Sources than it works on at once,
// would otherwise offer to index the ones that wait. index waits a moment
// for the other copies to be told, and no longer: it answers at once while
// another copy writes a book into the index. And once cancel has answered,
// no copy says the Source is being indexed.
func TestAGlanceSeesASourceThatWaitsForItsTurn(t *testing.T) {
	warm(t)
	computer := plugintest.NewComputer(t)
	folder := computer.DataFolder("conformance")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	// One Source at a time, and the first is held once its pages are read,
	// until the test lets it go: the one slot is taken for as long as the
	// test needs it.
	reading, held := make(chan struct{}), make(chan struct{})
	read := sync.OnceFunc(func() { close(reading) })
	plugin := shelf.New(shelf.Options{Folder: folder, Settings: shelf.Settings{Workers: 1}, Crash: func(point string) {
		if point == shelf.PointPagesRead {
			read()
			<-held
		}
	}})
	t.Cleanup(plugin.Close)
	let := sync.OnceFunc(func() { close(held) })
	t.Cleanup(let)
	indexer := connect(t, plugin)

	first := file(t, "first.a1", pdf([]string{"The first book speaks of spectra."}))
	waiting := file(t, "waiting.b2", pdf([]string{"The second book speaks of sheaves."}))
	indexer.index("turns", first)
	select {
	case <-reading:
	case <-time.After(patience):
		t.Fatal("the first book was never read")
	}
	// The glance is there before the Source is asked for, and looks right
	// after index has answered.
	glance, _ := run(t, computer)

	// Another copy writes into the index for a moment as the Source is
	// asked for, twice, from two sessions: telling the others waits for the
	// writing, and both answers wait for that, the one that finds the work
	// under way too.
	done := writing(t, folder, "turns")
	go func() {
		time.Sleep(500 * time.Millisecond)
		done()
	}()
	var earlier error
	var once sync.WaitGroup
	once.Go(func() { _, earlier = indexing(indexer, "turns", waiting) })
	time.Sleep(150 * time.Millisecond)
	if _, err := indexing(connect(t, plugin), "turns", waiting); err != nil {
		t.Fatalf("index of %s, asked for again: %v", waiting.ID, err)
	}
	status := glance.status("turns")
	if now, listed := status[waiting.ID]; !listed || now.State != plugincontract.StateIndexing || now.Hash != "" {
		t.Fatalf("a glance says %+v (listed: %v) of a Source another study-shelf has been asked for and works on next; want it being indexed, with no hash", now, listed)
	}
	if now, listed := status[first.ID]; !listed || now.State != plugincontract.StateIndexing {
		t.Errorf("a glance says %+v (listed: %v) of the Source another study-shelf is reading; want it being indexed", now, listed)
	}
	if once.Wait(); earlier != nil {
		t.Fatalf("index of %s: %v", waiting.ID, earlier)
	}
	// Stopped while it waits, it was being indexed, and no copy says so any
	// longer.
	if !indexer.cancel("turns", waiting.ID) {
		t.Error("cancel says it stopped nothing of a Source that waited for its turn")
	}
	if now, listed := glance.status("turns")[waiting.ID]; listed {
		t.Errorf("once cancel has answered, a glance says %+v of the Source it stopped; want nothing", now)
	}

	// Another copy writes for longer than an answer may take, while several
	// sessions ask for Sources at once. Each is answered all the same, well
	// before SQLite would give up waiting for the writer, and none waits for
	// another's wait. The other copies are told once the writing is over.
	asked := []plugincontract.Source{waiting}
	for n := range 7 {
		asked = append(asked, file(t, fmt.Sprintf("more.c%d", n), pdf([]string{"A book that waits for its turn."})))
	}
	done = writing(t, folder, "turns")
	took, refused := make([]time.Duration, len(asked)), make([]error, len(asked))
	var asking sync.WaitGroup
	for n, source := range asked {
		session := connect(t, plugin)
		asking.Go(func() { took[n], refused[n] = indexing(session, "turns", source) })
	}
	asking.Wait()
	for n, err := range refused {
		if err != nil {
			t.Fatalf("index of %s: %v", asked[n].ID, err)
		}
	}
	if slowest := slices.Max(took); slowest > 4*time.Second {
		t.Errorf("of %d Sources asked for at once while another copy wrote into the index, the last was answered after %s: each is answered at once", len(asked), slowest)
	}
	// A Source of another collection is shown at once all the same: the
	// writer that is held is the first collection's.
	apart := file(t, "apart.d4", pdf([]string{"A book of another Topic."}))
	indexer.index("apart", apart)
	if now, listed := glance.status("apart")[apart.ID]; !listed || now.State != plugincontract.StateIndexing {
		t.Errorf("while another copy wrote into one collection, a glance says %+v (listed: %v) of a Source asked for in another; want it being indexed", now, listed)
	}
	if !indexer.cancel("apart", apart.ID) {
		t.Errorf("cancel says it stopped nothing of %s, which waited for its turn", apart.ID)
	}
	done()
	for deadline := time.Now().Add(patience); ; time.Sleep(10 * time.Millisecond) {
		status, shown := glance.status("turns"), 0
		for _, source := range asked {
			now, listed := status[source.ID]
			switch {
			case listed && now.State == plugincontract.StateIndexing:
				shown++
			case listed:
				// Its work could not begin: the wait for the writer was too
				// long, or too many waited at once.
				t.Fatalf("once the other copy's writing was over, a glance says %+v of %s; want it being indexed", now, source.ID)
			}
		}
		if shown == len(asked) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("once the other copy's writing was over, a glance said %d of the %d Sources asked for meanwhile are being indexed", shown, len(asked))
		}
	}
	for _, source := range asked {
		if !indexer.cancel("turns", source.ID) {
			t.Errorf("cancel says it stopped nothing of %s, which waited for its turn", source.ID)
		}
	}

	// The first is indexed once it is let go, and the other when it is asked
	// for again.
	let()
	if now, _ := indexer.settled("turns", first.ID); now.State != plugincontract.StateIndexed {
		t.Errorf("the first book let go of: %+v", now)
	}
	indexer.indexed("turns", waiting)
	if found := glance.search("turns", "sheaves", 5); len(found) != 1 {
		t.Errorf("a glance finds the Source indexed after its cancel %d times", len(found))
	}
}

// indexing asks a Shelf to index a Source, from a goroutine of the test's
// own, and returns how long the answer took. It fails nothing itself: a test
// fails only from its own goroutine.
func indexing(s *session, collection string, source plugincontract.Source) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	began := time.Now()
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: plugincontract.ToolIndex,
		Arguments: plugincontract.IndexInput{Collection: collection, Source: source}})
	took := time.Since(began)
	if err == nil && res.IsError {
		err = fmt.Errorf("refused: %+v", res.StructuredContent)
	}
	return took, err
}

// writing holds the one writer of a collection, as another copy of Shelf
// does while it writes a book into the index, until the function it returns
// is called.
func writing(t *testing.T, folder, collection string) (done func()) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(folder, collection+".sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err == nil {
		_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	}
	if err != nil {
		_ = db.Close()
		t.Fatalf("the writer of collection %s could not be held: %v", collection, err)
	}
	done = sync.OnceFunc(func() {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		_ = conn.Close()
		_ = db.Close()
	})
	t.Cleanup(done)
	return done
}

// Two study-shelf programs on one folder, a glance beside a study source
// index or two of either: each finds what the other indexed, and one
// searches while the other indexes.
func TestTwoStudyShelvesShareOneIndex(t *testing.T) {
	computer := plugintest.NewComputer(t)
	searcher, _ := run(t, computer)
	indexer, _ := run(t, computer)

	first := file(t, "first.a1", pdf([]string{"The first book speaks of homology."}))
	indexer.indexed("shared", first)
	if now := searcher.status("shared")[first.ID]; now.State != plugincontract.StateIndexed || now.Hash != first.Hash {
		t.Fatalf("one study-shelf says %+v of what the other indexed", now)
	}
	found := searcher.search("shared", "homology", 5)
	if len(found) != 1 {
		t.Fatalf("one study-shelf does not find what the other indexed: %+v", found)
	}

	// While the one indexes a long book, the other searches, reads a Passage
	// and indexes a book of its own in the same collection.
	book := file(t, "long.b2", long(1500, "sheet"))
	indexer.index("shared", book)
	second := file(t, "second.c3", pdf([]string{"The second book speaks of cohomology."}))
	searcher.index("shared", second)
	searches := 0
	for {
		if during := searcher.search("shared", "homology", 5, first.ID); len(during) != 1 || during[0].ID != found[0].ID {
			t.Fatalf("while the other study-shelf indexes, a search finds %+v", during)
		}
		if again, ok := searcher.passage("shared", found[0].ID); !ok || again.Text != found[0].Text {
			t.Fatalf("while the other study-shelf indexes, Passage %s is %+v (found: %v)", found[0].ID, again, ok)
		}
		searches++
		if now, listed := indexer.status("shared")[book.ID]; listed && now.State != plugincontract.StateIndexing {
			break
		}
	}
	if done, _ := searcher.settled("shared", second.ID); done.State != plugincontract.StateIndexed {
		t.Fatalf("the book one study-shelf indexed while the other was indexing: %+v", done)
	}
	t.Logf("%d searches were answered while the other study-shelf indexed", searches)

	// Both wrote, and a third finds all of it.
	third, _ := run(t, computer)
	status := third.status("shared")
	for _, source := range []plugincontract.Source{first, book, second} {
		if now := status[source.ID]; now.State != plugincontract.StateIndexed || now.Hash != source.Hash {
			t.Errorf("a third study-shelf says %+v of %s", now, source.ID)
		}
	}
	for word, want := range map[string]string{"homology": first.ID, "cohomology": second.ID, "sheet1500": book.ID} {
		if found := third.search("shared", word, 5); len(found) != 1 || found[0].Source != want {
			t.Errorf("a third study-shelf, searching for %s, finds %+v", word, found)
		}
	}
	integrity(t, computer.DataFolder("conformance"), "shared")
}

// A session that ends for a reason study did not give is said, last on
// standard error, where study looks for the reason, and the program does not
// exit as if nothing had happened. A session that study ends, by closing
// the program's input, ends without a word.
func TestAStudyShelfSaysWhyItsSessionEnded(t *testing.T) {
	for name, tc := range map[string]struct {
		input string
		code  int
		says  string
	}{
		"what it is sent is no MCP message": {"this is no MCP message\n", 1, "study-shelf: the session ended:"},
		"its input is closed":               {"", 0, ""},
	} {
		cmd := exec.Command(shelftest.Shelf(t))
		cmd.Env = []string{plugincontract.DataFolderEnv + "=" + t.TempDir()}
		cmd.Stdin = strings.NewReader(tc.input)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if code != tc.code || !strings.Contains(stderr.String(), tc.says) || (tc.says == "" && stderr.Len() != 0) {
			t.Errorf("%s: study-shelf exited with %d and said %q; want %d and %q", name, code, stderr.String(), tc.code, tc.says)
		}
	}
}
