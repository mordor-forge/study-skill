package internal

import "time"

// Rating represents a review quality rating.
type Rating int

const (
	Again Rating = 1 // Forgot
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

// String returns the human-readable name for the rating.
func (r Rating) String() string {
	switch r {
	case Again:
		return "Again"
	case Hard:
		return "Hard"
	case Good:
		return "Good"
	case Easy:
		return "Easy"
	default:
		return "Unknown"
	}
}

// Valid returns true if the rating is in [1, 4].
func (r Rating) Valid() bool {
	return r >= Again && r <= Easy
}

// State represents the learning state of a card.
type State int

const (
	New      State = 0
	Learning State = 1
	Review   State = 2
)

// String returns the human-readable name for the state.
func (s State) String() string {
	switch s {
	case New:
		return "New"
	case Learning:
		return "Learning"
	case Review:
		return "Review"
	default:
		return "Unknown"
	}
}

// Card represents a single flashcard with its scheduling state.
type Card struct {
	ID          string    `json:"id"`
	Topic       string    `json:"topic"`
	LessonNum   int       `json:"lesson_num"`
	State       State     `json:"state"`
	Difficulty  float64   `json:"difficulty"`
	Stability   float64   `json:"stability"`
	Due         time.Time `json:"due"`
	LastReview  time.Time `json:"last_review"`
	Reviews     int       `json:"reviews"`
	Lapses      int       `json:"lapses"`
	ElapsedDays int       `json:"elapsed_days"`
}

// NewCard creates a new card with sensible defaults.
// The card is in New state and due immediately.
func NewCard(id, topic string, lessonNum int) Card {
	now := time.Now().UTC().Truncate(time.Second)
	return Card{
		ID:         id,
		Topic:      topic,
		LessonNum:  lessonNum,
		State:      New,
		Difficulty: 0,
		Stability:  0,
		Due:        now,
		LastReview: time.Time{},
		Reviews:    0,
		Lapses:     0,
	}
}

// IsDue returns true if the card is due for review at or before the given time.
func (c *Card) IsDue(at time.Time) bool {
	return !c.Due.After(at)
}
