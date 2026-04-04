package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cards.json")

	// Create and save.
	store := NewStore(path)
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	card := &Card{
		ID:          "test-1",
		Topic:       "Go Concurrency",
		LessonNum:   3,
		State:       Review,
		Difficulty:  5.5,
		Stability:   12.3,
		Due:         now.Add(5 * 24 * time.Hour),
		LastReview:  now,
		Reviews:     4,
		Lapses:      1,
		ElapsedDays: 3,
	}
	if err := store.Add(card); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into a new store.
	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	loaded := store2.Get("test-1")
	if loaded == nil {
		t.Fatal("Card not found after load")
	}

	// Verify all fields.
	if loaded.ID != card.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, card.ID)
	}
	if loaded.Topic != card.Topic {
		t.Errorf("Topic = %q, want %q", loaded.Topic, card.Topic)
	}
	if loaded.LessonNum != card.LessonNum {
		t.Errorf("LessonNum = %d, want %d", loaded.LessonNum, card.LessonNum)
	}
	if loaded.State != card.State {
		t.Errorf("State = %d, want %d", loaded.State, card.State)
	}
	if loaded.Difficulty != card.Difficulty {
		t.Errorf("Difficulty = %f, want %f", loaded.Difficulty, card.Difficulty)
	}
	if loaded.Stability != card.Stability {
		t.Errorf("Stability = %f, want %f", loaded.Stability, card.Stability)
	}
	if !loaded.Due.Equal(card.Due) {
		t.Errorf("Due = %v, want %v", loaded.Due, card.Due)
	}
	if !loaded.LastReview.Equal(card.LastReview) {
		t.Errorf("LastReview = %v, want %v", loaded.LastReview, card.LastReview)
	}
	if loaded.Reviews != card.Reviews {
		t.Errorf("Reviews = %d, want %d", loaded.Reviews, card.Reviews)
	}
	if loaded.Lapses != card.Lapses {
		t.Errorf("Lapses = %d, want %d", loaded.Lapses, card.Lapses)
	}
	if loaded.ElapsedDays != card.ElapsedDays {
		t.Errorf("ElapsedDays = %d, want %d", loaded.ElapsedDays, card.ElapsedDays)
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := store.Load(); err != nil {
		t.Fatalf("Load of nonexistent file should succeed: %v", err)
	}
	if len(store.Cards) != 0 {
		t.Errorf("Cards should be empty, got %d", len(store.Cards))
	}
}

func TestDuplicateAdd(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cards.json"))
	card := &Card{ID: "dup-1", Topic: "Topic"}
	if err := store.Add(card); err != nil {
		t.Fatalf("First Add failed: %v", err)
	}
	if err := store.Add(card); err == nil {
		t.Error("Second Add should fail for duplicate ID")
	}
}

func TestDueCardsFiltering(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cards.json"))
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	// Card due in the past.
	pastCard := &Card{ID: "past", Due: now.Add(-24 * time.Hour)}
	// Card due now.
	nowCard := &Card{ID: "now", Due: now}
	// Card due in the future.
	futureCard := &Card{ID: "future", Due: now.Add(24 * time.Hour)}

	store.Add(pastCard)
	store.Add(nowCard)
	store.Add(futureCard)

	due := store.DueCards(now)
	if len(due) != 2 {
		t.Errorf("DueCards(now) returned %d cards, want 2", len(due))
	}

	// Verify the right cards are due.
	ids := make(map[string]bool)
	for _, c := range due {
		ids[c.ID] = true
	}
	if !ids["past"] || !ids["now"] {
		t.Errorf("Expected past and now to be due, got: %v", ids)
	}
	if ids["future"] {
		t.Error("Future card should not be due")
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "cards.json")

	store := NewStore(path)
	store.Add(&Card{ID: "test-1", Topic: "Topic"})

	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("File not created: %v", err)
	}
}

func TestAllCards(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cards.json"))
	store.Add(&Card{ID: "a", Topic: "A"})
	store.Add(&Card{ID: "b", Topic: "B"})
	store.Add(&Card{ID: "c", Topic: "C"})

	all := store.AllCards()
	if len(all) != 3 {
		t.Errorf("AllCards() returned %d, want 3", len(all))
	}
}

func TestLoadWrappedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cards.json")

	// Write the wrapped format that an LLM or SKILL.md might create
	wrapped := []byte(`{"cards":[{"id":"test-1","topic":"Go Basics","lesson_num":1,"state":0,"difficulty":0.3,"stability":0.4,"due":"2026-04-04T10:00:00Z","last_review":"2026-04-04T10:00:00Z","reviews":0,"lapses":0,"elapsed_days":0}]}`)
	if err := os.WriteFile(path, wrapped, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load of wrapped format should succeed: %v", err)
	}

	if len(store.Cards) != 1 {
		t.Fatalf("Expected 1 card, got %d", len(store.Cards))
	}

	card := store.Get("test-1")
	if card == nil {
		t.Fatal("Card 'test-1' not found")
	}
	if card.Topic != "Go Basics" {
		t.Errorf("Topic = %q, want %q", card.Topic, "Go Basics")
	}
}

func TestLoadEmptyWrappedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cards.json")

	// The most common case: LLM creates {"cards":[]}
	if err := os.WriteFile(path, []byte(`{"cards":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load of empty wrapped format should succeed: %v", err)
	}

	if len(store.Cards) != 0 {
		t.Errorf("Expected 0 cards, got %d", len(store.Cards))
	}
}
