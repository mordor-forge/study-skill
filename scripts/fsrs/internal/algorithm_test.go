package internal

import (
	"math"
	"testing"
	"time"
)

func newTestFSRS() *FSRS {
	return NewFSRS()
}

func TestInitialStability(t *testing.T) {
	f := newTestFSRS()

	tests := []struct {
		rating Rating
		want   float64
	}{
		{Again, 0.212},
		{Hard, 1.2931},
		{Good, 2.3065},
		{Easy, 8.2956},
	}
	for _, tt := range tests {
		got := f.InitialStability(tt.rating)
		if math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("InitialStability(%s) = %f, want %f", tt.rating, got, tt.want)
		}
	}
}

func TestInitialDifficulty(t *testing.T) {
	f := newTestFSRS()

	// D0(rating) = w4 - exp(w5 * (rating - 1)) + 1
	// w4 = 6.4133, w5 = 0.8334

	// Again (1): 6.4133 - exp(0) + 1 = 6.4133
	// Easy (4): 6.4133 - exp(0.8334*3) + 1 = 6.4133 - exp(2.5002) + 1
	// exp(2.5002) ~ 12.183 => 6.4133 - 12.183 + 1 = -4.77 => clamped to 1.0

	dAgain := f.InitialDifficulty(Again)
	if math.Abs(dAgain-6.4133) > 0.01 {
		t.Errorf("D0(Again) = %f, want ~6.4133", dAgain)
	}

	dEasy := f.InitialDifficulty(Easy)
	if dEasy != minDifficulty {
		t.Errorf("D0(Easy) = %f, want %f (clamped)", dEasy, minDifficulty)
	}

	// Difficulty should decrease with higher ratings.
	dHard := f.InitialDifficulty(Hard)
	dGood := f.InitialDifficulty(Good)
	if dAgain <= dHard || dHard <= dGood || dGood <= dEasy {
		t.Errorf("Difficulty should decrease with higher ratings: Again=%f Hard=%f Good=%f Easy=%f",
			dAgain, dHard, dGood, dEasy)
	}
}

func TestDifficultyClamp(t *testing.T) {
	f := newTestFSRS()

	// D0(Easy) should be clamped to 1.0.
	d := f.InitialDifficulty(Easy)
	if d < minDifficulty || d > maxDifficulty {
		t.Errorf("Difficulty %f out of [%f, %f]", d, minDifficulty, maxDifficulty)
	}

	// UpdateDifficulty should also stay in range.
	// Start at max difficulty, rate Easy many times.
	d = maxDifficulty
	for i := 0; i < 100; i++ {
		d = f.UpdateDifficulty(d, Easy)
	}
	if d < minDifficulty || d > maxDifficulty {
		t.Errorf("After 100 Easy reviews, difficulty %f out of [%f, %f]", d, minDifficulty, maxDifficulty)
	}

	// Start at min difficulty, rate Again many times.
	d = minDifficulty
	for i := 0; i < 100; i++ {
		d = f.UpdateDifficulty(d, Again)
	}
	if d < minDifficulty || d > maxDifficulty {
		t.Errorf("After 100 Again reviews, difficulty %f out of [%f, %f]", d, minDifficulty, maxDifficulty)
	}
}

func TestRetrievabilityDecay(t *testing.T) {
	f := newTestFSRS()
	stability := 5.0

	// At t=0, R should be 1.0.
	r0 := f.Retrievability(0, stability)
	if math.Abs(r0-1.0) > 1e-6 {
		t.Errorf("R(0, 5) = %f, want 1.0", r0)
	}

	// R should decrease over time.
	r1 := f.Retrievability(1, stability)
	r5 := f.Retrievability(5, stability)
	r30 := f.Retrievability(30, stability)

	if r1 >= r0 {
		t.Errorf("R should decrease: R(0)=%f R(1)=%f", r0, r1)
	}
	if r5 >= r1 {
		t.Errorf("R should decrease: R(1)=%f R(5)=%f", r1, r5)
	}
	if r30 >= r5 {
		t.Errorf("R should decrease: R(5)=%f R(30)=%f", r5, r30)
	}

	// Higher stability should mean slower decay.
	rLowS := f.Retrievability(10, 2.0)
	rHighS := f.Retrievability(10, 20.0)
	if rHighS <= rLowS {
		t.Errorf("Higher stability should mean slower decay: R(10,2)=%f R(10,20)=%f", rLowS, rHighS)
	}

	// R should be in [0, 1].
	if r0 < 0 || r0 > 1 || r30 < 0 || r30 > 1 {
		t.Errorf("R out of [0,1]: R(0)=%f R(30)=%f", r0, r30)
	}
}

func TestRetrievabilityZeroStability(t *testing.T) {
	f := newTestFSRS()
	r := f.Retrievability(5, 0)
	if r != 0 {
		t.Errorf("R(5, 0) = %f, want 0", r)
	}
}

func TestNextInterval(t *testing.T) {
	f := newTestFSRS()

	// Minimum interval is 1 day.
	interval := f.NextInterval(0.01)
	if interval < 1 {
		t.Errorf("NextInterval(0.01) = %d, want >= 1", interval)
	}

	// Higher stability should give longer interval.
	i1 := f.NextInterval(1.0)
	i5 := f.NextInterval(5.0)
	i20 := f.NextInterval(20.0)
	if i5 <= i1 {
		t.Errorf("Higher stability should give longer interval: I(1)=%d I(5)=%d", i1, i5)
	}
	if i20 <= i5 {
		t.Errorf("Higher stability should give longer interval: I(5)=%d I(20)=%d", i5, i20)
	}

	// Maximum interval is 36500 days.
	iMax := f.NextInterval(1e9)
	if iMax > maxInterval {
		t.Errorf("NextInterval(1e9) = %d, want <= %d", iMax, maxInterval)
	}
}

func TestStabilityAfterSuccessEasyVsHard(t *testing.T) {
	f := newTestFSRS()

	d := 5.0
	s := 3.0
	r := 0.8

	sHard := f.StabilityAfterSuccess(d, s, r, Hard)
	sGood := f.StabilityAfterSuccess(d, s, r, Good)
	sEasy := f.StabilityAfterSuccess(d, s, r, Easy)

	if sHard >= sGood {
		t.Errorf("Hard should give less stability: Hard=%f Good=%f", sHard, sGood)
	}
	if sGood >= sEasy {
		t.Errorf("Good should give less stability than Easy: Good=%f Easy=%f", sGood, sEasy)
	}

	// All should be >= original stability (success increases stability).
	if sHard < s {
		t.Errorf("Success stability should increase: original=%f Hard=%f", s, sHard)
	}
}

func TestStabilityAfterFailure(t *testing.T) {
	f := newTestFSRS()

	d := 5.0
	s := 10.0
	r := 0.3

	sFail := f.StabilityAfterFailure(d, s, r)

	// Stability should never increase on failure.
	if sFail > s {
		t.Errorf("Stability should not increase on failure: original=%f failed=%f", s, sFail)
	}

	// Stability should never go below minStability.
	if sFail < minStability {
		t.Errorf("Stability below minimum: %f < %f", sFail, minStability)
	}
}

func TestStabilityNeverBelowMinimum(t *testing.T) {
	f := newTestFSRS()

	// Extreme case: very high difficulty, very low stability, very low retrievability.
	sFail := f.StabilityAfterFailure(10.0, 0.1, 0.01)
	if sFail < minStability {
		t.Errorf("Stability below minimum: %f < %f", sFail, minStability)
	}

	sSuccess := f.StabilityAfterSuccess(10.0, 0.1, 0.01, Hard)
	if sSuccess < minStability {
		t.Errorf("Stability below minimum: %f < %f", sSuccess, minStability)
	}
}

func TestNewCardReview(t *testing.T) {
	f := newTestFSRS()
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	// Review a new card with Good rating.
	card := NewCard("test-1", "Go Basics", 1)
	card.Due = now // ensure it's due now
	f.Review(&card, Good, now)

	// State should transition from New to Learning.
	if card.State != Learning {
		t.Errorf("State = %s, want Learning", card.State)
	}

	// Reviews should increment.
	if card.Reviews != 1 {
		t.Errorf("Reviews = %d, want 1", card.Reviews)
	}

	// Stability should be initialized.
	if card.Stability <= 0 {
		t.Errorf("Stability = %f, want > 0", card.Stability)
	}

	// Difficulty should be initialized.
	if card.Difficulty <= 0 {
		t.Errorf("Difficulty = %f, want > 0", card.Difficulty)
	}

	// Due date should be in the future.
	if !card.Due.After(now) {
		t.Errorf("Due = %v, want after %v", card.Due, now)
	}

	// LastReview should be set.
	if card.LastReview != now {
		t.Errorf("LastReview = %v, want %v", card.LastReview, now)
	}

	// Lapses should be 0 for Good.
	if card.Lapses != 0 {
		t.Errorf("Lapses = %d, want 0", card.Lapses)
	}
}

func TestAgainIncreasesLapses(t *testing.T) {
	f := newTestFSRS()
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	// First review: Again on a new card.
	card := NewCard("test-1", "Hard Topic", 1)
	f.Review(&card, Again, now)

	if card.Lapses != 1 {
		t.Errorf("Lapses after first Again = %d, want 1", card.Lapses)
	}

	// Second review: Again on a learning card.
	f.Review(&card, Again, now.Add(24*time.Hour))

	if card.Lapses != 2 {
		t.Errorf("Lapses after second Again = %d, want 2", card.Lapses)
	}

	// Third review: Good should not increase lapses.
	lapsesBefore := card.Lapses
	f.Review(&card, Good, now.Add(48*time.Hour))

	if card.Lapses != lapsesBefore {
		t.Errorf("Lapses after Good = %d, want %d (unchanged)", card.Lapses, lapsesBefore)
	}
}

func TestEasyGivesLongerIntervalThanHard(t *testing.T) {
	f := newTestFSRS()
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	// Create two identical cards, review one as Hard and one as Easy.
	cardHard := NewCard("hard-1", "Topic", 1)
	cardEasy := NewCard("easy-1", "Topic", 1)

	// First review with Good to get them into a comparable state.
	f.Review(&cardHard, Good, now)
	f.Review(&cardEasy, Good, now)

	// Second review at same elapsed time.
	reviewTime := now.Add(2 * 24 * time.Hour)
	f.Review(&cardHard, Hard, reviewTime)
	f.Review(&cardEasy, Easy, reviewTime)

	daysHard := int(cardHard.Due.Sub(reviewTime).Hours() / 24)
	daysEasy := int(cardEasy.Due.Sub(reviewTime).Hours() / 24)

	if daysEasy <= daysHard {
		t.Errorf("Easy interval (%d days) should be longer than Hard interval (%d days)",
			daysEasy, daysHard)
	}
}

func TestReviewStateTransitions(t *testing.T) {
	f := newTestFSRS()
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

	card := NewCard("test-1", "Topic", 1)
	if card.State != New {
		t.Fatalf("Initial state = %s, want New", card.State)
	}

	// New -> Learning (first review).
	f.Review(&card, Good, now)
	if card.State != Learning {
		t.Errorf("After first review: state = %s, want Learning", card.State)
	}

	// Learning -> Review (subsequent success).
	f.Review(&card, Good, now.Add(2*24*time.Hour))
	if card.State != Review {
		t.Errorf("After second Good review: state = %s, want Review", card.State)
	}

	// Review -> Learning (failure).
	f.Review(&card, Again, now.Add(5*24*time.Hour))
	if card.State != Learning {
		t.Errorf("After Again review: state = %s, want Learning", card.State)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi, want float64
	}{
		{5, 1, 10, 5},
		{0, 1, 10, 1},
		{15, 1, 10, 10},
		{1, 1, 10, 1},
		{10, 1, 10, 10},
	}
	for _, tt := range tests {
		got := clamp(tt.v, tt.lo, tt.hi)
		if got != tt.want {
			t.Errorf("clamp(%f, %f, %f) = %f, want %f", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}
