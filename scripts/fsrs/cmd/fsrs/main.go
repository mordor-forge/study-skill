package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gdetrane/study-skill/scripts/fsrs/internal"
)

const defaultStorePath = ".fsrs/cards.json"

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: fsrs <schedule|review|add|status> [args...]")
	}

	storePath := os.Getenv("FSRS_STORE")
	if storePath == "" {
		storePath = defaultStorePath
	}

	store := internal.NewStore(storePath)
	if err := store.Load(); err != nil {
		fatalf("loading store: %v", err)
	}

	switch os.Args[1] {
	case "schedule":
		cmdSchedule(store)
	case "review":
		cmdReview(store)
	case "add":
		cmdAdd(store)
	case "status":
		cmdStatus(store)
	default:
		fatalf("unknown command: %s", os.Args[1])
	}
}

// cmdSchedule outputs due cards as JSON with a due_count field.
func cmdSchedule(store *internal.Store) {
	now := time.Now().UTC()
	due := store.DueCards(now)
	if due == nil {
		due = []*internal.Card{}
	}

	out := struct {
		DueCount int              `json:"due_count"`
		Cards    []*internal.Card `json:"cards"`
	}{len(due), due}
	outputJSON(out)
}

// cmdReview records a review and outputs the updated card.
func cmdReview(store *internal.Store) {
	if len(os.Args) < 4 {
		fatalf("usage: fsrs review <id> <rating>")
	}
	id := os.Args[2]
	ratingStr := os.Args[3]

	ratingInt, err := strconv.Atoi(ratingStr)
	if err != nil {
		fatalf("invalid rating %q: must be 1-4", ratingStr)
	}
	rating := internal.Rating(ratingInt)
	if !rating.Valid() {
		fatalf("invalid rating %d: must be 1 (Again), 2 (Hard), 3 (Good), or 4 (Easy)", ratingInt)
	}

	card := store.Get(id)
	if card == nil {
		fatalf("card %q not found", id)
	}

	fsrs := internal.NewFSRS()
	now := time.Now().UTC()
	fsrs.Review(card, rating, now)

	if err := store.Save(); err != nil {
		fatalf("saving store: %v", err)
	}

	outputJSON(card)
}

// cmdAdd adds a new card for a lesson.
func cmdAdd(store *internal.Store) {
	if len(os.Args) < 5 {
		fatalf("usage: fsrs add <id> <topic> <lesson_num>")
	}
	id := os.Args[2]
	topic := os.Args[3]
	lessonNum, err := strconv.Atoi(os.Args[4])
	if err != nil {
		fatalf("invalid lesson number %q: %v", os.Args[4], err)
	}

	card := internal.NewCard(id, topic, lessonNum)
	if err := store.Add(&card); err != nil {
		fatalf("adding card: %v", err)
	}

	if err := store.Save(); err != nil {
		fatalf("saving store: %v", err)
	}

	outputJSON(card)
}

// cmdStatus outputs an overview of all cards.
func cmdStatus(store *internal.Store) {
	now := time.Now().UTC()
	all := store.AllCards()
	due := store.DueCards(now)

	type statusOutput struct {
		Total    int              `json:"total"`
		Due      int              `json:"due"`
		New      int              `json:"new"`
		Learning int              `json:"learning"`
		Review   int              `json:"review"`
		Cards    []*internal.Card `json:"cards"`
	}

	var newCount, learningCount, reviewCount int
	for _, c := range all {
		switch c.State {
		case internal.New:
			newCount++
		case internal.Learning:
			learningCount++
		case internal.Review:
			reviewCount++
		}
	}

	if all == nil {
		all = []*internal.Card{}
	}

	out := statusOutput{
		Total:    len(all),
		Due:      len(due),
		New:      newCount,
		Learning: learningCount,
		Review:   reviewCount,
		Cards:    all,
	}
	outputJSON(out)
}

// outputJSON marshals v as indented JSON to stdout.
func outputJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatalf("marshaling JSON: %v", err)
	}
	fmt.Println(string(data))
}

// fatalf prints an error to stderr and exits with code 1.
func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fsrs: "+format+"\n", args...)
	os.Exit(1)
}
