package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store manages JSON file-based card persistence.
type Store struct {
	Path  string
	Cards map[string]*Card
}

// NewStore creates a new Store pointing to the given file path.
func NewStore(path string) *Store {
	return &Store{
		Path:  path,
		Cards: make(map[string]*Card),
	}
}

// Load reads cards from the JSON file. If the file does not exist,
// the store starts empty (no error).
func (s *Store) Load() error {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.Cards = make(map[string]*Card)
			return nil
		}
		return fmt.Errorf("reading store file: %w", err)
	}

	// Support both bare array format [card1, card2]
	// and wrapped object format {"cards": [card1, card2]}
	var cards []*Card
	if err := json.Unmarshal(data, &cards); err != nil {
		// Try wrapped format
		var wrapped struct {
			Cards []*Card `json:"cards"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return fmt.Errorf("parsing store file: %w (also tried wrapped: %w)", err, err2)
		}
		cards = wrapped.Cards
	}

	s.Cards = make(map[string]*Card, len(cards))
	for _, c := range cards {
		s.Cards[c.ID] = c
	}
	return nil
}

// Save writes all cards to the JSON file, creating parent directories
// as needed.
func (s *Store) Save() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating store directory: %w", err)
	}

	cards := make([]*Card, 0, len(s.Cards))
	for _, c := range s.Cards {
		cards = append(cards, c)
	}

	data, err := json.MarshalIndent(cards, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cards: %w", err)
	}

	if err := os.WriteFile(s.Path, data, 0o644); err != nil {
		return fmt.Errorf("writing store file: %w", err)
	}
	return nil
}

// Get returns a card by ID, or nil if not found.
func (s *Store) Get(id string) *Card {
	return s.Cards[id]
}

// Add adds a new card to the store. Returns error if ID already exists.
func (s *Store) Add(card *Card) error {
	if _, exists := s.Cards[card.ID]; exists {
		return fmt.Errorf("card %q already exists", card.ID)
	}
	s.Cards[card.ID] = card
	return nil
}

// DueCards returns all cards that are due at or before the given time.
func (s *Store) DueCards(at time.Time) []*Card {
	var due []*Card
	for _, c := range s.Cards {
		if c.IsDue(at) {
			due = append(due, c)
		}
	}
	return due
}

// AllCards returns all cards in the store.
func (s *Store) AllCards() []*Card {
	cards := make([]*Card, 0, len(s.Cards))
	for _, c := range s.Cards {
		cards = append(cards, c)
	}
	return cards
}
