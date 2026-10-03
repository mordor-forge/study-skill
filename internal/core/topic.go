package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"golang.org/x/text/unicode/norm"

	"github.com/mordor-forge/lamplight/v2/internal/checkpoint"
)

const (
	topicFile     = "topic.toml"
	gitattributes = ".gitattributes"
	gitignore     = ".gitignore"
	maxTitleRunes = 200
	maxGoalRunes  = 500
)

// Event types recorded in a Topic's History.
const (
	eventTopicCreated = "topic.created"
	eventTopicUpdated = "topic.updated"
)

// Topic is one subject the learner is studying, with its own folder and git
// repository inside the Study home.
type Topic struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Goal    string    `json:"goal,omitempty"`
	Path    string    `json:"path"`
	Created time.Time `json:"created"`
	// KnowledgeBase is where the Topic's Sources are searched for Evidence;
	// absent until one is chosen.
	KnowledgeBase *KnowledgeBase `json:"knowledge_base,omitempty"`
	// Flags are what replaying the History found that needs the learner's
	// attention.
	Flags []Flag `json:"flags,omitempty"`
	// Resume is where the learner stopped, once the Topic has a Syllabus
	// or a Session: show its Next step first.
	Resume *ResumePoint `json:"resume,omitempty"`
	// Imported is where the Topic was imported from, for a Topic imported
	// from a v1 workspace, and whether it has been adopted yet.
	Imported *TopicImported `json:"imported,omitempty"`
	// LessonsWithoutEvidence are the Lessons started or done that cite no
	// Evidence, once the Topic has Sources or a NotebookLM Knowledge base.
	// They are marked, never blocked.
	LessonsWithoutEvidence []string `json:"lessons_without_evidence,omitempty"`
	// Cards says whether Cards are ready to review, never how many; absent
	// for a Topic without Cards.
	Cards *CardsReady `json:"cards,omitempty"`
	// LearnerAdditions is the path of the Topic's additions to the Learner
	// profile, when they exist.
	LearnerAdditions string `json:"learner_additions,omitempty"`
	// AssessmentDue is the Milestone whose end-of-Milestone Assessment is
	// the next thing to do, once all its Lessons are done or skipped.
	AssessmentDue *MilestoneRef `json:"assessment_due,omitempty"`

	// The plan; see plan.go. State is active, paused or finished.
	State string `json:"state"`
	// Deadline is the Goal's deadline, YYYY-MM-DD.
	Deadline string       `json:"deadline,omitempty"`
	Pace     []PacePeriod `json:"pace,omitempty"`
	// NewCardsPerDay is the daily cap on new Cards decided.
	NewCardsPerDay int `json:"new_cards_per_day"`
	// Forecast says when each Milestone ends at the Pace; none while the
	// Topic is paused or finished, or before it has a Syllabus.
	Forecast *Forecast `json:"forecast,omitempty"`
	// Tasks are the open Tasks relevant now.
	Tasks []Task `json:"tasks,omitempty"`
	// SettingsProblems says what in topic.toml or tasks.jsonl could not be
	// read, such as a hand-edited Pace or a line that is not a Task, each
	// naming its file; the rest of the Topic stands.
	SettingsProblems []string `json:"settings_problems,omitempty"`
	// Level is how advanced the teaching is, and where that came from: the
	// last Assessment, or the learner's choice since. Absent until set.
	Level *LevelInfo `json:"level,omitempty"`
	// Approach is how the Topic's Lessons relate to each other: concepts,
	// project or challenges. Absent until chosen.
	Approach string `json:"approach,omitempty"`
}

// TopicSpec describes a Topic to create.
type TopicSpec struct {
	// Title names the Topic, for example "Linear algebra". Required.
	Title string
	// ID is the Topic's folder name. Derived from Title when empty.
	ID string
	// Goal is what the learner wants to be able to do at the end. Optional.
	Goal string
	// DryRun validates the request and returns the Topic that would be
	// created, without writing anything.
	DryRun bool
}

// TopicChanges describes changes to a Topic's settings. Nil fields stay as
// they are.
type TopicChanges struct {
	Title *string
	// Goal replaces the goal; an empty goal removes it.
	Goal *string
	// KnowledgeBase chooses the Topic's Knowledge base.
	KnowledgeBase *KnowledgeBase
	// Deadline sets the Goal's deadline, YYYY-MM-DD; empty removes it.
	Deadline *string
	// Pace replaces the Pace periods; an empty list removes the Pace.
	Pace *[]PacePeriod
	// NewCardsPerDay sets the daily cap on new Cards decided.
	NewCardsPerDay *int
	// State pauses or finishes the Topic, or makes it active again.
	State *string
	// Level is the learner's choice of Level; it holds until the next
	// Assessment sets one.
	Level *string
	// Approach is concepts, project or challenges.
	Approach *string
	// AddTasks adds Tasks; RemoveTasks removes Tasks by id.
	AddTasks    []TaskSpec
	RemoveTasks []string
	// DryRun validates the request and returns the Topic as it would be,
	// without writing anything.
	DryRun bool
}

// TopicUpdate is the result of UpdateTopic.
type TopicUpdate struct {
	Topic Topic `json:"topic"`
	// Changed is false when the Topic already had the requested values, so
	// nothing was recorded.
	Changed bool `json:"changed"`
	// AddedTasks are the Tasks asked for, with their ids; a Task with the
	// title of one already there is that Task. In a dry run, a new Task
	// has no id yet.
	AddedTasks []Task `json:"added_tasks,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

// topicSettings is the content of topic.toml.
type topicSettings struct {
	Format int    `toml:"format"`
	Title  string `toml:"title"`
	Goal   string `toml:"goal,omitempty"`

	// extra holds settings this version of study does not know, such as
	// ones a newer version added within the same format, or the learner's
	// own. They are written back unchanged.
	extra map[string]any
}

// topicCreatedData is the payload of a topic.created Event.
type topicCreatedData struct {
	Title string `json:"title"`
	Goal  string `json:"goal,omitempty"`
}

// topicUpdatedData is the payload of a topic.updated Event: the fields that
// changed, with their new values.
type topicUpdatedData struct {
	Title *string `json:"title,omitempty"`
	Goal  *string `json:"goal,omitempty"`
}

var topicIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// CreateTopic creates a Topic folder in the Study home with its settings, its
// History and its git repository, and makes it the most recent Topic.
//
// The folder is assembled under .lamplight/tmp and renamed into place only
// once it is complete, so an interrupted creation never leaves a half-made
// Topic behind, and needs neither the Topic lock nor an intent marker.
func (c *Core) CreateTopic(ctx context.Context, spec TopicSpec) (Topic, error) {
	title, err := cleanText("title", spec.Title, maxTitleRunes)
	if err != nil {
		return Topic{}, err
	}
	if title == "" {
		return Topic{}, invalidf("a Topic needs a title")
	}
	goal, err := cleanText("goal", spec.Goal, maxGoalRunes)
	if err != nil {
		return Topic{}, err
	}
	id := spec.ID
	if id == "" {
		if id = slugify(title); id == "" {
			return Topic{}, invalidf("cannot derive a folder name from %q: pass an id such as \"linear-algebra\"", title)
		}
	}
	if err := validateTopicID(id); err != nil {
		return Topic{}, err
	}
	wall := c.now()
	topic := Topic{ID: id, Title: title, Goal: goal, Path: filepath.Join(c.home, id),
		Created: nextEventTime(wall, time.Time{}), State: TopicActive, NewCardsPerDay: NewCardsPerDay}
	if _, err := os.Lstat(topic.Path); err == nil {
		return Topic{}, alreadyExists(id)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Topic{}, internalError("checking "+topic.Path, err)
	}
	if spec.DryRun {
		return topic, nil
	}

	home, err := c.openHome()
	if err != nil {
		return Topic{}, err
	}
	defer home.Close()
	scratch := filepath.Join(localDir, "tmp")
	if err := home.MkdirAll(scratch, 0o755); err != nil {
		return Topic{}, internalError("creating "+scratch, err)
	}
	staging := filepath.Join(scratch, id+"."+randomID())
	if err := home.Mkdir(staging, 0o755); err != nil {
		return Topic{}, internalError("creating "+staging, err)
	}
	if err := c.initTopic(ctx, home, staging, topic, wall); err != nil {
		_ = home.RemoveAll(staging)
		return Topic{}, err
	}
	if err := home.Rename(staging, id); err != nil {
		_ = home.RemoveAll(staging)
		if _, statErr := home.Lstat(id); statErr == nil {
			return Topic{}, alreadyExists(id)
		}
		return Topic{}, internalError("moving the new Topic into place", err)
	}
	// The most recent Topic is local convenience state: failing to record it
	// must not turn a successful creation into an error.
	if err := c.setRecentTopic(home, id); err != nil {
		c.log.Warn("could not record the most recent Topic", "topic", id, "err", err)
	}
	c.log.Info("topic created", "topic", id, "path", topic.Path)
	return topic, nil
}

// initTopic writes a new Topic into dir: the topic.created Event, then the
// content it creates, then the git repository.
func (c *Core) initTopic(ctx context.Context, home *os.Root, dir string, topic Topic, wall time.Time) error {
	root, err := home.OpenRoot(dir)
	if err != nil {
		return internalError("opening "+dir, err)
	}
	defer root.Close()

	ev, contents, err := c.prepareEvent(&topicView{root: root}, change{
		Type:  eventTopicCreated,
		Data:  topicCreatedData{Title: topic.Title, Goal: topic.Goal},
		Items: []string{topicFile, gitattributes},
	}, topic.Created, wall)
	if err != nil {
		return err
	}
	if err := c.appendEvent(root, topic.ID, ev); err != nil {
		return err
	}
	for i, it := range ev.Items {
		if err := writeItem(root, it.Item, contents[i]); err != nil {
			return err
		}
	}
	// The .gitignore is the learner's to edit, so it is written once here
	// rather than recorded as an item of the Event.
	if err := writeFileAtomic(root, gitignore, []byte(checkpoint.DefaultGitignore())); err != nil {
		return err
	}
	return gitInit(ctx, filepath.Join(c.home, dir))
}

// topicStep is one change UpdateTopic records with its own Event.
type topicStep struct {
	// what names the change for errors, such as "Pace". describe, when
	// set, names what the step's Event actually changed.
	what     string
	plan     plan
	describe func() string
}

// UpdateTopic changes a Topic's settings: title, goal, Knowledge base, the
// Goal's deadline, the Pace, the daily cap on new Cards, Tasks and the
// Topic's state. Asking for the values it already has changes nothing and
// records no Event.
//
// Each kind of change is recorded by its own Event type, so one update can
// record several Events. Everything is validated first, so a later write
// can only fail for reasons such as a full disk; the error then says which
// changes were recorded.
func (c *Core) UpdateTopic(ctx context.Context, id string, changes TopicChanges) (TopicUpdate, error) {
	if err := checkTopicID(id); err != nil {
		return TopicUpdate{}, err
	}
	var want topicUpdatedData
	if changes.Title != nil {
		title, err := cleanText("title", *changes.Title, maxTitleRunes)
		if err != nil {
			return TopicUpdate{}, err
		}
		if title == "" {
			return TopicUpdate{}, invalidf("a Topic needs a title")
		}
		want.Title = &title
	}
	if changes.Goal != nil {
		goal, err := cleanText("goal", *changes.Goal, maxGoalRunes)
		if err != nil {
			return TopicUpdate{}, err
		}
		want.Goal = &goal
	}
	var kb *KnowledgeBase
	if changes.KnowledgeBase != nil {
		checked, err := checkKnowledgeBase(*changes.KnowledgeBase)
		if err != nil {
			return TopicUpdate{}, err
		}
		kb = &checked
	}
	var steps []topicStep
	if want.Title != nil || want.Goal != nil {
		var applied topicUpdatedData
		steps = append(steps, topicStep{what: "title and goal", plan: planTitleAndGoal(id, want, &applied),
			describe: func() string {
				switch {
				case applied.Goal == nil:
					return "title"
				case applied.Title == nil:
					return "goal"
				}
				return "title and goal"
			}})
	}
	if kb != nil {
		steps = append(steps, topicStep{what: "Knowledge base", plan: planKnowledgeBase(id, *kb)})
	}
	if changes.Deadline != nil {
		deadline, err := checkDate("deadline", *changes.Deadline)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "deadline", plan: planDeadline(id, deadline)})
	}
	if changes.Pace != nil {
		pace, err := checkPace(*changes.Pace)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "Pace", plan: planPace(id, pace)})
	}
	if changes.NewCardsPerDay != nil {
		n := *changes.NewCardsPerDay
		if n < 0 || n > maxNewCardsPerDay {
			return TopicUpdate{}, invalidf("the daily cap on new Cards must be from 0 to %d, not %d", maxNewCardsPerDay, n)
		}
		steps = append(steps, topicStep{what: "daily cap on new Cards", plan: planNewCardsPerDay(id, n)})
	}
	var added []Task
	if len(changes.AddTasks) > 0 {
		specs, err := checkTaskSpecs(changes.AddTasks)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "Tasks", plan: c.planTasksAdded(id, specs, &added)})
	}
	if len(changes.RemoveTasks) > 0 {
		for _, t := range changes.RemoveTasks {
			if err := checkTaskID(t); err != nil {
				return TopicUpdate{}, err
			}
		}
		steps = append(steps, topicStep{what: "Tasks removed", plan: planTasksRemoved(id, changes.RemoveTasks)})
	}
	if changes.Level != nil {
		level, err := checkLevel(*changes.Level)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "Level", plan: planLevel(id, level)})
	}
	if changes.Approach != nil {
		approach, err := checkApproach(*changes.Approach)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "Approach", plan: planApproach(id, approach)})
	}
	if changes.State != nil {
		state, err := checkTopicState(*changes.State)
		if err != nil {
			return TopicUpdate{}, err
		}
		steps = append(steps, topicStep{what: "state", plan: planTopicState(state)})
	}
	if len(steps) == 0 {
		return TopicUpdate{}, invalidf("nothing to change: give a title, goal, Knowledge base, deadline, Pace, " +
			"daily cap on new Cards, Tasks, Level, Approach or state")
	}

	var events []*event
	var recorded []string
	for _, step := range steps {
		ev, err := c.writeTopic(ctx, id, step.plan, changes.DryRun)
		if err != nil {
			if len(recorded) == 0 {
				return TopicUpdate{}, err
			}
			what := joinAnd(recorded)
			verb := "was"
			if strings.Contains(what, " and ") {
				verb = "were"
			}
			msg := fmt.Sprintf("the %s of %s %s changed, but not its %s: %v", what, id, verb, step.what, err)
			if wasRecorded(err) {
				msg = fmt.Sprintf("the %s of %s %s changed, and its %s was recorded but not finished: the next change "+
					"to %s finishes it (%v)", what, id, verb, step.what, id, cause(err))
			}
			return TopicUpdate{}, &Error{Code: CodeOf(err), Err: err, Message: msg}
		}
		if ev != nil {
			events = append(events, ev)
			what := step.what
			if step.describe != nil {
				what = step.describe()
			}
			recorded = append(recorded, what)
		}
	}
	var topic Topic
	var err error
	if changes.DryRun {
		topic, err = c.previewTopic(id, events)
		// A dry run invents no ids: the real run picks its own.
		for _, ev := range events {
			if ev.Type != eventTaskAdded {
				continue
			}
			for _, it := range ev.Items {
				fresh := strings.TrimPrefix(it.Item, tasksFile+"#")
				for i := range added {
					if added[i].ID == fresh {
						added[i].ID = ""
					}
				}
				for i := range topic.Tasks {
					if topic.Tasks[i].ID == fresh {
						topic.Tasks[i].ID = ""
					}
				}
			}
		}
	} else {
		topic, err = c.readTopic(id)
	}
	if err != nil {
		return TopicUpdate{}, err
	}
	return TopicUpdate{Topic: topic, Changed: len(events) > 0, AddedTasks: added, DryRun: changes.DryRun}, nil
}

// planTitleAndGoal plans changing the title or the goal; applied receives
// what the Event changes.
func planTitleAndGoal(id string, want topicUpdatedData, applied *topicUpdatedData) plan {
	return func(_ *replayed, view *topicView) (*change, error) {
		data, exists, err := view.read(topicFile)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, corruptf("%s of %s is missing", topicFile, id)
		}
		current, err := parseTopicSettings(data, filepath.Join(id, topicFile))
		if err != nil {
			return nil, err
		}
		var diff topicUpdatedData
		if want.Title != nil && *want.Title != current.Title {
			diff.Title = want.Title
		}
		if want.Goal != nil && *want.Goal != current.Goal {
			diff.Goal = want.Goal
		}
		if diff.Title == nil && diff.Goal == nil {
			return nil, nil
		}
		*applied = diff
		return &change{Type: eventTopicUpdated, Data: diff, Items: []string{topicFile}}, nil
	}
}

// previewTopic is the Topic as it would be after events, which a dry run
// planned but did not write: their changes to topic.toml are applied in
// memory, as recovery would apply them, and so is a change of state.
func (c *Core) previewTopic(id string, events []*event) (Topic, error) {
	topic, err := c.readTopic(id)
	if err != nil {
		return Topic{}, err
	}
	home, root, err := c.openTopicFolder(id)
	if err != nil {
		return Topic{}, err
	}
	defer home.Close()
	defer root.Close()
	s, view, err := c.recoveredView(home, root, id)
	if err != nil {
		return Topic{}, err
	}
	data, exists, err := view.read(topicFile)
	if err != nil {
		return Topic{}, err
	}
	for _, ev := range events {
		for _, it := range ev.Items {
			if it.Item == topicFile {
				if data, exists, err = eventKinds[ev.Type].apply(*ev, topicFile, data, exists); err != nil {
					return Topic{}, err
				}
				continue
			}
			current, had, err := view.read(it.Item)
			if err != nil {
				return Topic{}, err
			}
			next, keep, err := eventKinds[ev.Type].apply(*ev, it.Item, current, had)
			if err != nil {
				return Topic{}, err
			}
			view.pending[it.Item] = itemContent{data: next, exists: keep}
		}
		switch ev.Type {
		case eventTopicStateSet:
			if err := replayTopicStateSet(s, *ev); err != nil {
				return Topic{}, err
			}
		case eventLevelSet:
			if err := replayLevelSet(s, *ev); err != nil {
				return Topic{}, err
			}
		}
	}
	if !exists {
		return Topic{}, corruptf("%s of %s is missing", topicFile, id)
	}
	settings, err := parseTopicSettings(data, filepath.Join(id, topicFile))
	if err != nil {
		return Topic{}, err
	}
	topic.Title, topic.Goal, topic.KnowledgeBase = settings.Title, settings.Goal, knowledgeBaseOf(settings)
	c.addPlan(&topic, s, settings, view)
	addLevel(&topic, s, settings, data)
	addApproach(&topic, settings)
	return topic, nil
}

func applyTopicCreated(ev event, item string, _ []byte, _ bool) ([]byte, bool, error) {
	var d topicCreatedData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return nil, false, corruptf("Event %s has an unreadable payload: %v", ev.ID, err)
	}
	switch item {
	case topicFile:
		data, err := encodeTopicSettings(topicSettings{Title: d.Title, Goal: d.Goal})
		return data, err == nil, err
	case gitattributes:
		return []byte(strings.Join(unionFiles, " merge=union\n") + " merge=union\n"), true, nil
	}
	return nil, false, corruptf("Event %s (%s) cannot edit %s", ev.ID, ev.Type, item)
}

// unionFiles hold one record per line, so a union merge keeps both
// machines' lines; replay and status flag what conflicts.
var unionFiles = []string{historyFile, cardsFile, sourcesFile, tasksFile}

func applyTopicUpdated(ev event, item string, current []byte, exists bool) ([]byte, bool, error) {
	if item != topicFile {
		return nil, false, corruptf("Event %s (%s) cannot edit %s", ev.ID, ev.Type, item)
	}
	if !exists {
		return nil, false, corruptf("%s is missing", topicFile)
	}
	var d topicUpdatedData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		return nil, false, corruptf("Event %s has an unreadable payload: %v", ev.ID, err)
	}
	settings, err := parseTopicSettings(current, topicFile)
	if err != nil {
		return nil, false, err
	}
	if d.Title != nil {
		settings.Title = *d.Title
	}
	if d.Goal != nil {
		settings.Goal = *d.Goal
	}
	data, err := encodeTopicSettings(settings)
	return data, err == nil, err
}

func replayTopicCreated(s *replayed, ev event) error {
	if s.created.IsZero() {
		s.created = ev.Time
	}
	return nil
}

func replayTopicUpdated(s *replayed, _ event) error {
	if s.created.IsZero() {
		return fmt.Errorf("%w: the Topic's creation", errUnknownItem)
	}
	return nil
}

// parseTopicSettings reads topic.toml; where names it in errors.
func parseTopicSettings(data []byte, where string) (topicSettings, error) {
	var settings topicSettings
	if hasConflictMarkers(data) {
		return settings, corruptf("%s holds a git merge conflict: resolve the merge conflict in %s by keeping one side "+
			"of each block between <<<<<<< and >>>>>>>, and deleting the marker lines", where, topicFile)
	}
	md, err := toml.Decode(string(data), &settings)
	if err != nil {
		return settings, corruptf("%s is not valid TOML: %v", where, err)
	}
	if settings.Format > FormatVersion {
		return settings, newerFormat(where, settings.Format)
	}
	if settings.Format < 1 {
		return settings, corruptf("%s has no format number: add format = %d", where, FormatVersion)
	}
	if len(md.Undecoded()) > 0 {
		if _, err := toml.Decode(string(data), &settings.extra); err != nil {
			return settings, corruptf("%s is not valid TOML: %v", where, err)
		}
		for _, known := range []string{"format", "title", "goal"} {
			delete(settings.extra, known)
		}
	}
	return settings, nil
}

// hasConflictMarkers reports whether a file holds the markers git leaves
// around a merge conflict.
func hasConflictMarkers(data []byte) bool {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("<<<<<<< ")) || bytes.HasPrefix(line, []byte(">>>>>>> ")) ||
			bytes.Equal(bytes.TrimRight(line, "\r"), []byte("=======")) {
			return true
		}
	}
	return false
}

// encodeTopicSettings writes topic.toml in the current format: the settings
// Lamplight knows, then any others, sorted, so the same settings always
// give the same bytes.
func encodeTopicSettings(settings topicSettings) ([]byte, error) {
	settings.Format = FormatVersion
	var buf bytes.Buffer
	buf.WriteString("# Topic settings. Lamplight rewrites this file; comments are not kept.\n")
	if err := toml.NewEncoder(&buf).Encode(settings); err != nil {
		return nil, internalError("encoding "+topicFile, err)
	}
	if len(settings.extra) > 0 {
		// The known settings are plain keys, so the others, tables
		// included, can follow them.
		if err := toml.NewEncoder(&buf).Encode(settings.extra); err != nil {
			return nil, internalError("encoding "+topicFile, err)
		}
	}
	return buf.Bytes(), nil
}

// gitInit makes dir a git repository. Commits are made later by Checkpoints,
// which never run programs named by repository configuration.
func gitInit(ctx context.Context, dir string) error {
	cmd := gitCommand(ctx, dir, "init", "--quiet", "--initial-branch=main")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return &Error{Code: CodeInternal, Message: "git is required: install git and try again", Err: err}
		}
		return internalError("initialising git: "+strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// readTopic reads one Topic from the Study home.
func (c *Core) readTopic(id string) (Topic, error) {
	home, topic, err := c.openTopicFolder(id)
	if err != nil {
		return Topic{}, err
	}
	topic.Close()
	defer home.Close()
	return c.loadTopic(home, id)
}

// loadTopic reads one Topic from the Study home and replays its History.
// Content is authoritative for text, so the title and goal come from
// topic.toml; the History gives the creation time and the flags. It writes
// nothing: an interrupted write is flagged, and the next write finishes it.
func (c *Core) loadTopic(home *os.Root, id string) (Topic, error) {
	root, err := home.OpenRoot(id)
	if err != nil {
		return Topic{}, internalError("opening Topic "+id, err)
	}
	defer root.Close()
	data, exists, err := readItem(root, topicFile)
	if err != nil {
		return Topic{}, err
	}
	if !exists {
		return Topic{}, corruptf("%s of %s is missing", topicFile, id)
	}
	settings, err := parseTopicSettings(data, filepath.Join(c.home, id, topicFile))
	if err != nil {
		return Topic{}, err
	}
	topic := Topic{ID: id, Title: settings.Title, Goal: settings.Goal, Path: filepath.Join(c.home, id),
		KnowledgeBase: knowledgeBaseOf(settings)}
	h, err := readHistory(root, id)
	if err != nil {
		return Topic{}, err
	}
	s := replayHistory(h)
	topic.Created = s.created
	topic.Flags = c.topicFlags(root, s)
	if r := s.study.resume(); !r.empty() {
		describeBreakPoint(root, &r)
		topic.Resume = &r
	}
	topic.LessonsWithoutEvidence = s.lessonsWithoutEvidence(s.citingLessons(topic.KnowledgeBase))
	c.addPlan(&topic, s, settings, newView(root, s))
	addLevel(&topic, s, settings, data)
	addApproach(&topic, settings)
	addImported(&topic, s)
	c.addTopicGuidance(root, s, &topic)
	if unfinished := unfinishedItems(home, id, s); len(unfinished) > 0 {
		kept := topic.Flags[:0]
		for _, f := range topic.Flags {
			if f.Kind != FlagEditedOutside || !unfinished[f.Item] {
				kept = append(kept, f)
			}
		}
		topic.Flags = kept
	}
	// A marker while the lock is held is a write in progress, not an
	// interrupted one.
	if wasInterrupted(home, id) {
		topic.Flags = append(topic.Flags, newFlag(FlagInterruptedWrite, "", nil, "",
			"a write to this Topic was interrupted; the next change to it finishes the write"))
	}
	return topic, nil
}

func validateTopicID(id string) error {
	if len(id) > 64 || !topicIDPattern.MatchString(id) {
		return invalidf("%q is not a valid Topic id: use lowercase letters, digits and single hyphens, up to 64 characters", id)
	}
	return nil
}

// cleanText trims s and rejects invalid UTF-8 and control characters, which
// an agent could use to inject terminal escape sequences into human output.
func cleanText(field, s string, maxRunes int) (string, error) {
	if !utf8.ValidString(s) {
		return "", invalidf("the %s is not valid UTF-8 text", field)
	}
	s = strings.TrimSpace(s)
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", invalidf("the %s contains a control character", field)
		}
		if isBidiControl(r) {
			return "", invalidf("the %s contains a bidirectional control character (U+%04X), which can make text "+
				"display differently from what it says", field, r)
		}
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return "", invalidf("the %s is longer than %d characters", field, maxRunes)
	}
	return s, nil
}

// isBidiControl reports whether r is a bidirectional embedding, override or
// isolate control (U+202A–U+202E, U+2066–U+2069), which can reorder how text
// is displayed in a terminal.
func isBidiControl(r rune) bool {
	return r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069
}

// slugify turns a title into a Topic id: "Lineare Algebra für Anfänger"
// becomes "lineare-algebra-fur-anfanger". Accents are dropped; other letters
// outside ASCII separate words.
func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(title)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// A combining accent: drop it and keep the base letter.
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	return slug
}

// writeFileAtomic replaces name inside root without exposing a half-written
// file: it writes a uniquely named temporary file, syncs it, renames it over
// name, and syncs the folder so the rename survives a power cut. Concurrent
// writers never share a temporary file. Temporary files are hidden and
// named so that the Topic's .gitignore keeps a crash's leftovers out of
// Checkpoints, and recovery removes them.
func writeFileAtomic(root *os.Root, name string, data []byte) error {
	tmp := filepath.Join(filepath.Dir(name), tempPrefix(filepath.Base(name))+randomID())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return internalError("writing "+name, err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = syncFile(f)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = root.Remove(tmp)
		return internalError("writing "+name, werr)
	}
	if err := root.Rename(tmp, name); err != nil {
		_ = root.Remove(tmp)
		return internalError("replacing "+name, err)
	}
	syncDir(root, filepath.Dir(name))
	return nil
}

// tempPrefix is how temporary files for base begin: ".<base>.lamplight-tmp-".
// checkpoint.DefaultGitignore ignores the pattern.
func tempPrefix(base string) string { return "." + base + ".lamplight-tmp-" }

// syncDir flushes a folder's entries to disk. It is best effort: some file
// systems cannot sync a folder, and the data itself is already synced.
func syncDir(root *os.Root, dir string) {
	d, err := root.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func alreadyExists(id string) error {
	return &Error{Code: CodeAlreadyExists, Message: "a Topic named " + id + " already exists"}
}
