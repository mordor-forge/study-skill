package internal

import (
	"testing"
	"time"
)

func TestRatingString(t *testing.T) {
	tests := []struct {
		r    Rating
		want string
	}{
		{Again, "Again"},
		{Hard, "Hard"},
		{Good, "Good"},
		{Easy, "Easy"},
		{Rating(0), "Unknown"},
		{Rating(5), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("Rating(%d).String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}

func TestRatingValid(t *testing.T) {
	tests := []struct {
		r    Rating
		want bool
	}{
		{Again, true},
		{Hard, true},
		{Good, true},
		{Easy, true},
		{Rating(0), false},
		{Rating(5), false},
		{Rating(-1), false},
	}
	for _, tt := range tests {
		if got := tt.r.Valid(); got != tt.want {
			t.Errorf("Rating(%d).Valid() = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		s    State
		want string
	}{
		{New, "New"},
		{Learning, "Learning"},
		{Review, "Review"},
		{State(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestNewCard(t *testing.T) {
	before := time.Now().UTC()
	c := NewCard("test-1", "Go Concurrency", 3)
	after := time.Now().UTC()

	if c.ID != "test-1" {
		t.Errorf("ID = %q, want %q", c.ID, "test-1")
	}
	if c.Topic != "Go Concurrency" {
		t.Errorf("Topic = %q, want %q", c.Topic, "Go Concurrency")
	}
	if c.LessonNum != 3 {
		t.Errorf("LessonNum = %d, want %d", c.LessonNum, 3)
	}
	if c.State != New {
		t.Errorf("State = %d, want New(%d)", c.State, New)
	}
	if c.Difficulty != 0 {
		t.Errorf("Difficulty = %f, want 0", c.Difficulty)
	}
	if c.Stability != 0 {
		t.Errorf("Stability = %f, want 0", c.Stability)
	}
	if c.Reviews != 0 {
		t.Errorf("Reviews = %d, want 0", c.Reviews)
	}
	if c.Lapses != 0 {
		t.Errorf("Lapses = %d, want 0", c.Lapses)
	}
	if c.Due.Before(before.Truncate(time.Second)) || c.Due.After(after.Truncate(time.Second).Add(time.Second)) {
		t.Errorf("Due = %v, want between %v and %v", c.Due, before, after)
	}
}

func TestIsDue(t *testing.T) {
	now := time.Now().UTC()
	c := Card{
		Due: now,
	}

	// Card due at now should be due at now
	if !c.IsDue(now) {
		t.Error("Card due at now should be due at now")
	}

	// Card due at now should be due in the future
	if !c.IsDue(now.Add(time.Hour)) {
		t.Error("Card due at now should be due an hour later")
	}

	// Card due at now should NOT be due in the past
	if c.IsDue(now.Add(-time.Hour)) {
		t.Error("Card due at now should not be due an hour ago")
	}
}
