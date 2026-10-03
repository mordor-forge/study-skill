package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// A change is one write to a Topic: an Event of type Type with payload Data
// that edits Items (see items.go).
type change struct {
	Type  string
	Data  any
	Items []string
}

// A plan decides the change to make, given the Topic's replayed History and
// a view of its content. It returns nil when there is nothing to do, which
// keeps operations idempotent: completing a Lesson twice changes nothing.
type plan func(s *replayed, view *topicView) (*change, error)

// topicView reads a Topic's items: as they are on disk or, in a dry run, as
// they would be once recovery had finished an interrupted write. Every
// reader of an entity resolves the lines a merge leaves for it the same way,
// with what the History recorded (see resolveEntity). A view lives for one
// read or one write, during which the files it reads do not change, so it
// parses each file once.
type topicView struct {
	root    *os.Root
	pending map[string]itemContent
	// s is the replayed History; nil for a Topic not created yet.
	s       *replayed
	indexes map[string]map[string][][]byte
}

func newView(root *os.Root, s *replayed) *topicView {
	return &topicView{root: root, s: s}
}

func (v *topicView) read(item string) ([]byte, bool, error) {
	if c, ok := v.pending[item]; ok {
		return c.data, c.exists, nil
	}
	file, key, err := parseItem(item)
	if err != nil {
		return nil, false, err
	}
	codec, _ := codecFor(file)
	if key == "" || codec.index == nil {
		return readItemWith(v.root, item, v.s.itemVersions(item))
	}
	idx, ok := v.indexes[file]
	if !ok {
		data, exists, err := readFile(v.root, file)
		if err != nil {
			return nil, false, err
		}
		if exists {
			if idx, err = codec.index(data); err != nil {
				return nil, false, corruptf("%s is damaged: %v", file, err)
			}
		}
		if v.indexes == nil {
			v.indexes = map[string]map[string][][]byte{}
		}
		v.indexes[file] = idx
	}
	content, exists, _ := resolveEntity(idx[key], v.s.itemVersions(item), codec.historyText)
	return content, exists, nil
}

// Points where a test can interrupt a write, as a crash would. See Core.crash.
const (
	crashBeforeLock  = "before-lock"  // the Topic is open, its lock not yet taken: nothing is written
	crashAfterIntent = "after-intent" // the marker is written, the Event is not
	crashAfterEvent  = "after-event"  // the Event is written, no content is
	crashAfterItem   = "after-item"   // after each item is replaced
	crashBeforeClear = "before-clear" // all content is replaced, the marker remains

	crashRecoveryAfterItem   = "recovery-after-item"   // recovery replaced an item
	crashRecoveryBeforeClear = "recovery-before-clear" // recovery is done, the marker remains
)

// writeTopic records one change in a Topic, so that a crash at any point
// leaves something recovery can finish (ADR-0005):
//
//  1. take the Topic's lock, so the CLI and the MCP server never interleave,
//     and check the Topic was not removed or replaced while waiting for it;
//  2. finish any interrupted write, then replay the History and plan;
//  3. leave an intent marker naming the Event;
//  4. append the Event, with each item's hash before and after;
//  5. replace each item atomically;
//  6. clear the marker.
//
// It returns the Event written, or nil when the plan had nothing to do. A
// dry run returns the Event that would be written, planned against the
// Topic as it would be after recovery, and writes nothing at all.
func (c *Core) writeTopic(ctx context.Context, topicID string, p plan, dryRun bool) (*event, error) {
	home, topic, err := c.openTopicFolder(topicID)
	if err != nil {
		return nil, err
	}
	defer home.Close()
	defer topic.Close()
	return c.writeOpenedTopic(ctx, home, topic, topicID, p, dryRun)
}

// writeOpenedTopic is writeTopic for an operation that opened the Topic's
// folder itself, read it, and worked for a while before writing: a Check
// runs for as long as its commands do. Step 1 then checks the folder the
// operation read, so a Topic removed or replaced during that work receives
// nothing: opening the Topic by its id again would write into whichever
// Topic has the id by then.
func (c *Core) writeOpenedTopic(ctx context.Context, home, topic *os.Root, topicID string, p plan, dryRun bool) (*event, error) {
	if dryRun {
		return c.planDryRun(home, topic, topicID, p)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := c.lockOpenedTopic(ctx, home, topic, topicID)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := c.recoverTopic(home, topic, topicID); err != nil {
		return nil, err
	}
	h, err := readHistory(topic, topicID)
	if err != nil {
		return nil, err
	}
	s := replayHistory(h)
	if err := refuseNewer(topicID, s); err != nil {
		return nil, err
	}
	ch, err := p(s, newView(topic, s))
	if err != nil || ch == nil {
		return nil, err
	}
	wall := c.now()
	ev, contents, err := c.prepareEvent(newView(topic, s), *ch, nextEventTime(wall, s.latest), wall)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := writeIntent(home, intent{Format: FormatVersion, Topic: topicID, Event: ev.ID, Type: ev.Type}); err != nil {
		return nil, err
	}
	if err := c.crashAt(crashAfterIntent); err != nil {
		return nil, err
	}
	if err := c.appendEvent(topic, topicID, ev); err != nil {
		return nil, err
	}
	if err := c.crashAt(crashAfterEvent); err != nil {
		return nil, recordedButUnfinished(topicID, err)
	}
	for i, it := range ev.Items {
		if err := writeItem(topic, it.Item, contents[i]); err != nil {
			return nil, recordedButUnfinished(topicID, err)
		}
		if err := c.crashAt(crashAfterItem); err != nil {
			return nil, recordedButUnfinished(topicID, err)
		}
	}
	if err := c.crashAt(crashBeforeClear); err != nil {
		return nil, recordedButUnfinished(topicID, err)
	}
	if err := clearIntent(home, topicID); err != nil {
		return nil, recordedButUnfinished(topicID, err)
	}
	// The most recent Topic is local convenience state: failing to record
	// it must not turn a successful write into an error.
	_ = c.setRecentTopic(home, topicID)
	return &ev, nil
}

// planDryRun plans a write without writing anything, not even the lock:
// against the Topic as recovery would leave it.
func (c *Core) planDryRun(home, topic *os.Root, topicID string, p plan) (*event, error) {
	s, view, err := c.recoveredView(home, topic, topicID)
	if err != nil {
		return nil, err
	}
	if err := refuseNewer(topicID, s); err != nil {
		return nil, err
	}
	ch, err := p(s, view)
	if err != nil || ch == nil {
		return nil, err
	}
	wall := c.now()
	ev, _, err := c.prepareEvent(view, *ch, nextEventTime(wall, s.latest), wall)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// recoveredView replays a Topic and views its items as they would be once
// recovery had finished an interrupted write, without writing anything.
func (c *Core) recoveredView(home, topic *os.Root, topicID string) (*replayed, *topicView, error) {
	h, err := readHistory(topic, topicID)
	if err != nil {
		return nil, nil, err
	}
	s := replayHistory(h)
	view := newView(topic, s)
	view.pending = map[string]itemContent{}
	// A dry run decides nothing, so it logs nothing.
	steps, err := c.recoverySteps(slog.New(slog.DiscardHandler), home, topic, topicID, h, s)
	if err != nil {
		return nil, nil, err
	}
	for _, st := range steps {
		view.pending[st.item] = st.content
	}
	return s, view, nil
}

// refuseNewer refuses to write a Topic whose History holds Events written by
// a newer version of study, which this one cannot replay correctly.
func refuseNewer(topicID string, s *replayed) error {
	if s.newer == 0 {
		return nil
	}
	return &Error{Code: CodeNewerFormat,
		Message: fmt.Sprintf("the History of %s holds Events written by a newer version of study, "+
			"so this version can read the Topic but not change it: upgrade study", topicID)}
}

// recordedButUnfinished explains a write that failed after its Event was
// recorded: the change is not lost, and the next write finishes it.
func recordedButUnfinished(topicID string, err error) error {
	return &Error{Code: CodeOf(err), Err: unfinished{err},
		Message: fmt.Sprintf("the change is recorded in the History of %s, but it could not be finished (%v); "+
			"the next change to %s finishes it once that is fixed", topicID, err, topicID)}
}

// unfinished marks the cause of a write that failed after its Event was
// recorded.
type unfinished struct{ err error }

func (u unfinished) Error() string { return u.err.Error() }
func (u unfinished) Unwrap() error { return u.err }

// wasRecorded reports whether a failed write recorded its Event anyway, so
// recovery will finish it.
func wasRecorded(err error) bool {
	var u unfinished
	return errors.As(err, &u)
}

// cause is the error behind a write's failure, without the explanation
// recordedButUnfinished added.
func cause(err error) error {
	var u unfinished
	if errors.As(err, &u) {
		return u.err
	}
	return err
}

// itemContent is an item's content after an Event, or its absence.
type itemContent struct {
	data   []byte
	exists bool
}

// prepareEvent builds the Event for ch at time at, written when the wall
// clock read wall: it runs the Event's applier on each item's current
// content and records both versions.
func (c *Core) prepareEvent(view *topicView, ch change, at, wall time.Time) (event, []itemContent, error) {
	kind, ok := eventKinds[ch.Type]
	if !ok {
		return event{}, nil, internalError("recording an Event", fmt.Errorf("unknown Event type %q", ch.Type))
	}
	data, err := json.Marshal(ch.Data)
	if err != nil {
		return event{}, nil, internalError("encoding an Event", err)
	}
	ev := event{Format: FormatVersion, ID: c.newID(), Time: at, Wall: wall.UTC().Truncate(clockTick), Type: ch.Type, Data: data}
	contents := make([]itemContent, 0, len(ch.Items))
	for _, item := range ch.Items {
		current, exists, err := view.read(item)
		if err != nil {
			return event{}, nil, err
		}
		next, keep, err := kind.apply(ev, item, current, exists)
		if err != nil {
			return event{}, nil, err
		}
		ev.Items = append(ev.Items, itemChange{Item: item, Before: contentHash(current, exists), After: contentHash(next, keep)})
		contents = append(contents, itemContent{data: next, exists: keep})
	}
	return ev, contents, nil
}

// recoveryStep is one item recovery rewrites.
type recoveryStep struct {
	item    string
	content itemContent
}

// recoverTopic finishes a write that was interrupted. It first repairs a
// History whose last line lacks its newline, then, if the Topic's intent
// marker names an Event, finishes that Event (see recoverySteps) and clears
// the marker. The caller holds the Topic lock.
func (c *Core) recoverTopic(home, topic *os.Root, topicID string) error {
	if err := c.repairHistoryTail(topic, topicID); err != nil {
		return err
	}
	m, ok, err := readIntent(home, topicID)
	if err != nil || !ok {
		return err
	}
	h, err := readHistory(topic, topicID)
	if err != nil {
		return err
	}
	steps, err := c.recoverySteps(c.log, home, topic, topicID, h, replayHistory(h))
	if err != nil {
		return err
	}
	for _, st := range steps {
		if err := writeItem(topic, st.item, st.content); err != nil {
			return err
		}
		c.log.Info("finished an interrupted write", "topic", topicID, "event", m.Event, "item", st.item)
		if err := c.crashAt(crashRecoveryAfterItem); err != nil {
			return err
		}
	}
	if ev := findEvent(h, m.Event); ev != nil {
		removeTempFiles(topic, ev.Items)
	}
	if err := c.crashAt(crashRecoveryBeforeClear); err != nil {
		return err
	}
	return clearIntent(home, topicID)
}

// recoverySteps decides how to finish the write the intent marker names, if
// any. Because of the lock, at most one Event can be unapplied after a
// crash, and only that Event is inspected. For each item it edits:
//
//   - matching the hash after the Event: that part of the write finished;
//   - matching the hash before, and the Event is still the latest to change
//     the item, or replay holds it so no Event recorded a version: the
//     write never reached it, so apply it;
//   - matching the hash before, but a later Event (synced from another
//     machine) changed the item since: the Event is superseded, so leave it;
//   - missing or unreadable: stop with an error and keep the Event and the
//     marker, so nothing is lost;
//   - anything else: the learner edited it since, so keep the edit and log it.
//
// It writes nothing, so dry runs can use it too; log receives its decisions.
func (c *Core) recoverySteps(log *slog.Logger, home, topic *os.Root, topicID string, h historyLog, s *replayed) ([]recoveryStep, error) {
	m, ok, err := readIntent(home, topicID)
	if err != nil || !ok {
		return nil, err
	}
	ev := findEvent(h, m.Event)
	if ev == nil {
		log.Info("an interrupted write never reached the History, so there is nothing to finish",
			"topic", topicID, "event", m.Event, "type", m.Type)
		return nil, nil
	}
	kind, ok := eventKinds[ev.Type]
	if !ok {
		return nil, corruptf("Topic %s has an interrupted %s write that this version of study cannot finish: upgrade study", topicID, ev.Type)
	}
	var steps []recoveryStep
	for _, it := range ev.Items {
		current, exists, err := readItemWith(topic, it.Item, s.itemVersions(it.Item))
		if CodeOf(err) == CodeCorrupt {
			return nil, corruptf("%s in Topic %s cannot be read (%v), so an interrupted write (Event %s) cannot be finished: "+
				"fix or restore it and try again", it.Item, topicID, err, ev.ID)
		}
		if err != nil {
			return nil, err
		}
		switch hash := contentHash(current, exists); {
		case hash == it.After:
			continue
		case hash == it.Before:
			// A held Event never recorded a version, so there is no latest
			// version to compare with: it is not superseded.
			if latest := s.versions[it.Item]; latest.event != "" && latest.event != ev.ID {
				log.Info("left an interrupted write that a later Event superseded",
					"topic", topicID, "event", ev.ID, "item", it.Item, "later", latest.event)
				continue
			}
			next, keep, err := kind.apply(*ev, it.Item, current, exists)
			if err != nil {
				return nil, err
			}
			if contentHash(next, keep) != it.After {
				// This binary writes the item differently from the one that
				// recorded the Event, after an upgrade say. Nobody edited the
				// item, so the Event's change still applies.
				log.Warn("finished an interrupted write whose content this version of study writes differently",
					"topic", topicID, "event", ev.ID, "item", it.Item)
			}
			steps = append(steps, recoveryStep{item: it.Item, content: itemContent{data: next, exists: keep}})
		case !exists:
			return nil, corruptf("%s in Topic %s is missing, so an interrupted write (Event %s) cannot be finished: "+
				"restore it, for example from the last Checkpoint, and try again", it.Item, topicID, ev.ID)
		default:
			if err := validateItem(it.Item, current); err != nil {
				return nil, corruptf("%s in Topic %s cannot be read (%v), so an interrupted write (Event %s) cannot be finished: "+
					"fix or restore it and try again", it.Item, topicID, err, ev.ID)
			}
			log.Warn("kept a hand edit made after an interrupted write", "topic", topicID, "event", ev.ID, "item", it.Item)
		}
	}
	return steps, nil
}

func findEvent(h historyLog, id string) *event {
	for i := range h.events {
		if h.events[i].ID == id {
			return &h.events[i]
		}
	}
	return nil
}

// removeTempFiles removes temporary files an interrupted write left next to
// the files of items.
func removeTempFiles(topic *os.Root, items []itemChange) {
	done := map[string]bool{}
	for _, it := range items {
		file, _, err := parseItem(it.Item)
		if err != nil {
			continue
		}
		name := filepath.FromSlash(file)
		dir, prefix := filepath.Dir(name), tempPrefix(filepath.Base(name))
		key := dir + "\x00" + prefix
		if done[key] {
			continue
		}
		done[key] = true
		entries, err := fs.ReadDir(topic.FS(), filepath.ToSlash(dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix) {
				_ = topic.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}

func (c *Core) crashAt(point string) error {
	if c.crash == nil {
		return nil
	}
	return c.crash(point)
}

// isTopic reports whether a folder in the Study home is a Topic: it has
// settings or a History. A Topic whose topic.toml is missing is damaged,
// not gone.
func isTopic(home *os.Root, name string) bool {
	for _, file := range []string{topicFile, historyFile} {
		_, err := home.Lstat(filepath.Join(name, file))
		// A folder that cannot be read (such as permission denied) counts as
		// a Topic, so it is reported as unreadable instead of disappearing.
		if err == nil || !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return true
		}
	}
	return false
}

// checkTopicID checks an ID that names an existing Topic.
func checkTopicID(topicID string) error {
	if strings.TrimSpace(topicID) == "" {
		return invalidf("name the Topic: run study status to see their ids")
	}
	return validateTopicID(topicID)
}

func noSuchTopic(topicID string) error {
	return &Error{Code: CodeNotFound, Message: "there is no Topic named " + topicID + ": run study status to see your Topics"}
}

// checkTopicFolder checks that topicID names a real Topic folder in the
// Study home: a folder, not a symbolic link, since locks and intent markers
// are kept by name and a second name for one Topic would bypass them.
func checkTopicFolder(home *os.Root, topicID string) error {
	if err := checkTopicID(topicID); err != nil {
		return err
	}
	info, err := home.Lstat(topicID)
	if errors.Is(err, fs.ErrNotExist) {
		return noSuchTopic(topicID)
	}
	if err != nil {
		return internalError("reading Topic "+topicID, err)
	}
	if !info.IsDir() {
		return corruptf("%s in the Study home is not a folder: Topics are folders, never links", topicID)
	}
	if !isTopic(home, topicID) {
		return noSuchTopic(topicID)
	}
	return nil
}

// openTopicFolder opens the Study home and an existing Topic's folder.
func (c *Core) openTopicFolder(topicID string) (home, topic *os.Root, err error) {
	if err := checkTopicID(topicID); err != nil {
		return nil, nil, err
	}
	home, err = os.OpenRoot(c.home)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, noSuchTopic(topicID)
	}
	if err != nil {
		return nil, nil, internalError("opening the Study home "+c.home, err)
	}
	if err := checkTopicFolder(home, topicID); err != nil {
		home.Close()
		return nil, nil, err
	}
	topic, err = home.OpenRoot(topicID)
	if err != nil {
		home.Close()
		return nil, nil, internalError("opening Topic "+topicID, err)
	}
	return home, topic, nil
}

// intent is the marker a write leaves in the Study home's .lamplight/intents
// while it runs. It is local to the machine and never synced. If it is still
// there when the next write starts, the write was interrupted and recovery
// finishes it.
type intent struct {
	Format int    `json:"format"`
	Topic  string `json:"topic"`
	Event  string `json:"event"`
	Type   string `json:"type"`
}

func intentPath(topicID string) string { return filepath.Join(localDir, "intents", topicID+".json") }

func writeIntent(home *os.Root, m intent) error {
	if err := home.MkdirAll(filepath.Dir(intentPath(m.Topic)), 0o755); err != nil {
		return internalError("creating "+filepath.Dir(intentPath(m.Topic)), err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return internalError("encoding an intent marker", err)
	}
	return writeFileAtomic(home, intentPath(m.Topic), append(data, '\n'))
}

func readIntent(home *os.Root, topicID string) (intent, bool, error) {
	path := intentPath(topicID)
	data, err := home.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return intent{}, false, nil
	}
	if err != nil {
		return intent{}, false, internalError("reading "+path, err)
	}
	var m intent
	if err := json.Unmarshal(data, &m); err != nil || m.Event == "" || m.Format < 1 {
		return intent{}, false, corruptf("the intent marker %s is damaged, so an interrupted write to Topic %s cannot be checked: "+
			"look at the end of the Topic's History, then delete the marker", path, topicID)
	}
	if m.Format > FormatVersion {
		return intent{}, false, newerFormat(path, m.Format)
	}
	return m, true, nil
}

func clearIntent(home *os.Root, topicID string) error {
	if err := home.Remove(intentPath(topicID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return internalError("clearing the intent marker of "+topicID, err)
	}
	syncDir(home, filepath.Dir(intentPath(topicID)))
	return nil
}

// hasIntent reports whether a write to the Topic was interrupted.
func hasIntent(home *os.Root, topicID string) bool {
	_, err := home.Lstat(intentPath(topicID))
	return err == nil
}

// unfinishedItems names the items of the Event an intent marker names: a
// write in progress or interrupted has yet to bring their content up to the
// History, so a difference there is not an edit made outside Lamplight.
func unfinishedItems(home *os.Root, topicID string, s *replayed) map[string]bool {
	m, ok, err := readIntent(home, topicID)
	if err != nil || !ok {
		return nil
	}
	out := map[string]bool{}
	for _, ev := range s.applied {
		if ev.ID == m.Event {
			for _, it := range ev.Items {
				out[it.Item] = true
			}
		}
	}
	return out
}
