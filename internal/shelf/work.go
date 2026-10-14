package shelf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordor-forge/lamplight/v2/internal/plugincontract"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/cut"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/endpoint"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/index"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/pdf"
	"github.com/mordor-forge/lamplight/v2/internal/shelf/web"
)

// job is one Source being indexed.
type job struct {
	// source is the Source as index was given it.
	source plugincontract.Source
	// vectorsOnly says that the Source's content is indexed already, and
	// that the work is to give its Passages vectors: the Source is not
	// read.
	vectorsOnly bool
	// ctx is done once the work is told to stop, which stop does.
	ctx  context.Context
	stop context.CancelFunc
	// begun is closed once the work is shown to the other copies of Shelf,
	// or could not be.
	begun chan struct{}
	// done is closed when the work has ended, and stopped then says that it
	// ended because it was told to, with nothing of it in the index.
	done    chan struct{}
	stopped bool
	// done64 is how much of the work is done, from 0 to 1, as the bits of a
	// float64, once known is set: a file's pages are counted, and a web
	// page is fetched or not.
	done64 atomic.Uint64
	known  atomic.Bool
}

// indexes reports whether the job is indexing the content that is asked
// for: the same file content, or the same address.
func (j *job) indexes(asked plugincontract.Source) bool {
	if j.source.Kind != asked.Kind {
		return false
	}
	if asked.Kind == plugincontract.SourceURL {
		// Work that fetches nothing is not the work of fetching a page
		// again.
		return j.source.URL == asked.URL && !(j.vectorsOnly && asked.Refetch)
	}
	return j.source.Hash == asked.Hash
}

func (j *job) progress() (float64, bool) {
	return math.Float64frombits(j.done64.Load()), j.known.Load()
}

func (j *job) advance(to float64) {
	j.done64.Store(math.Float64bits(to))
	j.known.Store(true)
}

// problem is what is wrong with a Source, in words for the learner: the
// reason status gives for it.
type problem struct{ reason string }

func (p *problem) Error() string { return p.reason }

func wrong(format string, args ...any) error {
	return &problem{reason: fmt.Sprintf(format, args...)}
}

// work indexes one Source, after index has answered. Whatever it comes to
// is in the index before the job leaves the list of what is being indexed:
// the Source's new content, in the place of what was there, or the reason it
// could not be indexed, beside what was there. Told to stop, it leaves the
// index as it was.
func (s *Shelf) work(k key, in plugincontract.IndexInput, j *job) {
	ctx := j.ctx
	source := index.Source{ID: k.source, Kind: in.Source.Kind, Title: in.Source.Title}
	// What the indexing was asked with, which a failure is recorded with.
	asked := in.Source.Hash
	if in.Source.Kind == plugincontract.SourceURL {
		source.Address, asked = in.Source.URL, in.Source.URL
	}

	// From here on the other copies of Shelf see the Source being indexed,
	// and no longer as the one whose last indexing failed: while it waits
	// for its turn too, so that a study status in another terminal does not
	// offer to index a Source that is next.
	var c *index.Collection
	var shown *index.Work
	err := error(errStopped)
	turn := s.beginner(k.collection)
	select {
	case turn <- struct{}{}:
		// One work begins at a time in a collection. Beginning is a write,
		// which waits for another copy of Shelf that is writing a book into
		// the collection, on one of the few connections it has: the others
		// are kept for what this copy reads of it meanwhile, status and
		// index among it.
		if c, err = s.collection(ctx, k.collection, true); err == nil {
			shown, err = c.Begin(ctx, k.source)
		}
		<-turn
	case <-ctx.Done():
	}
	close(j.begun)
	if err == nil {
		select {
		case s.slots <- struct{}{}:
			// One of the few Sources that are worked on at once.
			defer func() { <-s.slots }()
			if j.vectorsOnly {
				err = s.embedIndexed(ctx, c, in, j, shown)
			} else {
				err = s.run(ctx, in, source, j, shown)
			}
		case <-ctx.Done():
			err = errStopped
		}
	}

	stopped := err != nil && ctx.Err() != nil
	unwritten := ""
	if err != nil && !stopped {
		reason := reasonOf(err)
		// Told to the index, where the next Shelf finds it: study starts one
		// for every question, and the learner reads the reason long after
		// the Shelf that met it has exited. An index that cannot be told is
		// why the Source failed, or will say so itself; the reason is then
		// kept here for as long as this copy runs.
		told, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		failed := err
		switch {
		case shown != nil:
			failed = shown.Fail(told, source, asked, reason)
		case c != nil:
			failed = c.Fail(told, source, asked, reason)
		}
		cancel()
		if failed != nil {
			unwritten = reason
		}
	}
	if shown != nil {
		// Before the job leaves the list: once cancel has answered, no copy
		// of Shelf says the Source is being indexed.
		shown.End()
	}
	s.mu.Lock()
	j.stopped = stopped
	if unwritten != "" {
		s.unwritten[k] = unwritten
	}
	if s.jobs[k] == j {
		delete(s.jobs, k)
	}
	s.mu.Unlock()
	j.stop()
	close(j.done)
}

// run reads a Source, cuts it into Passages and puts them in the index. It
// returns nil once the new content is there, and otherwise why it is not.
func (s *Shelf) run(ctx context.Context, in plugincontract.IndexInput, source index.Source, j *job, shown *index.Work) error {
	// How far the work is, for whoever asks this Shelf and for the others.
	advance := func(to float64) {
		j.advance(to)
		shown.Progress(ctx, to)
	}
	// Reading is all of the work but the writing, without an embedding
	// endpoint. With one it is the lesser part: a book is read in seconds,
	// and its Passages are embedded in a minute or in several.
	reading := 0.95
	if s.settings.Embedder != nil {
		reading = 0.1
	}
	var pages []cut.Page
	var err error
	switch in.Source.Kind {
	case plugincontract.SourceFile:
		pages, source.Hash, err = s.readFile(ctx, in.Source, func(share float64) { advance(reading * share) })
	case plugincontract.SourceURL:
		reading = 0
		pages, source.Hash, err = s.fetchPage(ctx, in.Source.URL)
	}
	if err != nil {
		return err
	}
	var passages []index.Passage
	var texts []string
	for _, p := range cut.Passages(pages, s.settings.PassageSize) {
		passages = append(passages, index.Passage{Text: p.Text, First: p.First, Last: p.Last})
		texts = append(texts, p.Text)
	}
	if len(passages) == 0 {
		return wrong("there is no text in it")
	}
	// The vectors before anything is written: a Source is in the index
	// whole, with a vector for each of its Passages, or not at all. An
	// endpoint that fails leaves what was indexed before, and the reason.
	var vectors [][]float32
	if s.settings.Embedder != nil {
		if vectors, source.Made, err = s.embed(ctx, in.Source.Title, texts, func(share float64) { advance(reading + (0.98-reading)*share) }); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return errStopped
	}
	// From here on nothing stops it: the write is one short transaction,
	// and a cancel that arrives now is told, truthfully, that it stopped
	// nothing.
	return shown.Replace(context.WithoutCancel(ctx), source, passages, vectors)
}

// embed asks the embedding endpoint for a vector for each text of a Source
// with that title, a batch at a time, and says how far it is: share goes
// from 0 to 1. It returns the vectors and what they were made with.
func (s *Shelf) embed(ctx context.Context, title string, texts []string, advance func(share float64)) ([][]float32, index.Made, error) {
	e := s.settings.Embedder
	vectors, err := e.Documents(ctx, title, texts, func(done int) { advance(float64(done) / float64(len(texts))) })
	var failed *endpoint.Problem
	switch {
	case errors.As(err, &failed):
		// What went wrong, and no more: study shows a reason up to three
		// hundred bytes, and gives the command that has it tried again.
		return nil, index.Made{}, wrong("%s", failed.Reason)
	case err != nil:
		return nil, index.Made{}, err
	}
	return vectors, index.Made{Model: e.Model(), Document: e.Document(), Dimensions: len(vectors[0])}, nil
}

// embedIndexed gives vectors to the Passages that are indexed for a Source,
// in the place of the ones they had, and reads no Source: the content is
// the one that was asked for, and it is there. It is what indexing a Source
// again comes to when the learner gave Shelf an endpoint, or another model,
// after the Source was indexed. The Passages and their ids stay.
func (s *Shelf) embedIndexed(ctx context.Context, c *index.Collection, in plugincontract.IndexInput, j *job, shown *index.Work) error {
	s.at(PointGivingVectors)
	hash, texts, err := c.Texts(ctx, in.Source.ID)
	if err != nil {
		return err
	}
	const anew = "its content was indexed anew while its Passages were being embedded. Index it again"
	switch {
	case hash == "" || len(texts) == 0:
		return wrong("what was indexed of it is no longer there. Index it again")
	case in.Source.Kind == plugincontract.SourceFile && hash != in.Source.Hash:
		// Another copy of Shelf put other content in its place since index
		// answered: that is not the content that was asked for.
		return wrong(anew)
	}
	vectors, made, err := s.embed(ctx, in.Source.Title, texts, func(share float64) {
		j.advance(0.98 * share)
		shown.Progress(ctx, 0.98*share)
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errStopped
	}
	err = shown.Embed(context.WithoutCancel(ctx), hash, texts, made, vectors)
	if errors.Is(err, index.ErrChanged) {
		return wrong(anew)
	}
	return err
}

// readFile reads the PDF a file Source names and returns the text of its
// pages, cleaned, and the content hash of the bytes that were read: the
// hash it was handed with, or the file is not indexed. It says how much of
// the file is read, from 0 to 1, as it goes.
func (s *Shelf) readFile(ctx context.Context, asked plugincontract.Source, advance func(float64)) ([]cut.Page, string, error) {
	// Opened without waiting: a path that names a pipe would otherwise hold
	// the work until someone wrote to it.
	file, err := os.OpenFile(asked.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, "", wrong("the file is not there: it was moved or removed since it was added")
	case errors.Is(err, fs.ErrPermission):
		return nil, "", wrong("the file cannot be read: Shelf is not allowed to open it")
	case err != nil:
		return nil, "", wrong("the file cannot be read: %v", cause(err))
	}
	defer file.Close()
	info, err := file.Stat()
	switch {
	case err != nil:
		return nil, "", wrong("the file cannot be read: %v", cause(err))
	case info.IsDir():
		return nil, "", wrong("it is a folder, not a file")
	case !info.Mode().IsRegular():
		return nil, "", wrong("it is not a file")
	}
	if ok, err := pdf.HasHeader(file); err != nil {
		return nil, "", wrong("the file cannot be read: %v", cause(err))
	} else if !ok {
		return nil, "", notPDF()
	}

	ctx, cancel := context.WithTimeout(ctx, s.settings.FileTime)
	defer cancel()
	slow := func(err error) error {
		// The file's own time is over, not the work's.
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return wrong("reading the file took longer than %s, the most Shelf gives one file", s.settings.FileTime)
		}
		return err
	}
	// The hash is of the bytes Shelf read, from the one file it holds open,
	// and never the one it was handed: study counts a Source as indexed when
	// the two are the same. So a file that is no longer what study read is
	// not indexed at all, and the learner is told to ask again: indexed
	// under its new hash it would be a Source study does not count, and
	// under the old one a lie.
	before, size, err := hashOf(ctx, file)
	if err != nil {
		return nil, "", slow(err)
	}
	if before != asked.Hash {
		return nil, "", wrong("the file changed since study read it. Index it again")
	}
	// It is taken again once the pages are read: a file that changed in
	// between is one whose pages belong to no hash. And the reader is given
	// as many bytes as were hashed, and no other count.
	// The pages as the reader gives them first: how a word that was divided
	// at the end of a line is written is read from all of the file's words
	// (cut.WordsOf), so the pages are tidied once every one of them is read.
	var pages []cut.Page
	err = pdf.Read(ctx, file, size, func(number, of int, raw string) error {
		pages = append(pages, cut.Page{Number: number, Text: raw})
		advance(float64(number) / float64(of))
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, pdf.ErrNotPDF):
		return nil, "", notPDF()
	case errors.Is(err, pdf.ErrProtected):
		return nil, "", wrong("the file is protected by a password, and Shelf cannot open it. Save a copy without the password and add that")
	case errors.Is(err, pdf.ErrDamaged):
		return nil, "", wrong("the file is damaged: it is a PDF that cannot be read to its end. Download or export it again")
	case errors.Is(err, pdf.ErrTooMuch):
		return nil, "", wrong("the file holds more than %d MB of text, the most Shelf takes from one file", pdf.MostText>>20)
	default:
		return nil, "", slow(err)
	}
	s.at(PointPagesRead)
	after, _, err := hashOf(ctx, file)
	if err != nil {
		return nil, "", slow(err)
	}
	if after != before {
		return nil, "", wrong("the file changed while it was being read. Index it again")
	}
	raw := make([]string, len(pages))
	for i := range pages {
		raw[i] = pages[i].Text
	}
	words, text := cut.WordsOf(raw...), false
	for i := range pages {
		pages[i].Text = cut.Clean(pages[i].Text, words)
		text = text || pages[i].Text != ""
	}
	if !text {
		return nil, "", wrong("the file has no text layer: its pages are pictures, as in a scanned book, and Shelf reads text. " +
			"Run it through a program that recognises the text (OCR), and add the file it makes")
	}
	return pages, before, nil
}

func notPDF() error {
	return wrong("the file is not a PDF: Shelf reads PDF files and web pages")
}

// hashOf returns the content hash of a file, read from its start to its
// end, and how many bytes that was.
func hashOf(ctx context.Context, file *os.File) (hash string, size int64, err error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, wrong("the file cannot be read: %v", cause(err))
	}
	sum := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		n, err := file.Read(buffer)
		sum.Write(buffer[:n])
		size += int64(n)
		if errors.Is(err, io.EOF) {
			return "sha256:" + hex.EncodeToString(sum.Sum(nil)), size, nil
		}
		if err != nil {
			return "", 0, wrong("the file cannot be read: %v", cause(err))
		}
	}
}

// fetchPage fetches a web page and returns its text, cleaned, as the one
// page a Source without pages is, and the content hash of what was fetched.
func (s *Shelf) fetchPage(ctx context.Context, address string) ([]cut.Page, string, error) {
	page, err := s.fetcher.Fetch(ctx, address)
	if err != nil {
		return nil, "", err
	}
	text := cut.Clean(page.Text, cut.WordsOf(page.Text))
	if text == "" {
		return nil, "", wrong("the page has no text to read")
	}
	return []cut.Page{{Text: text}}, page.Hash, nil
}

// cause is an operating system error without the path it names: the path
// is the learner's, and the reason is read beside the Source it is about.
func cause(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// reasonOf is why a Source could not be indexed, as status says it.
func reasonOf(err error) string {
	var p *problem
	var fetch *web.Problem
	reason := ""
	switch {
	case errors.As(err, &p):
		reason = p.reason
	case errors.As(err, &fetch):
		reason = fetch.Reason
	default:
		// Something of Shelf's own, the index most likely, which words its
		// errors for the learner.
		reason = cause(err).Error()
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "the Source could not be indexed"
	}
	return plugincontract.Cut(reason, plugincontract.MaxTextBytes)
}
