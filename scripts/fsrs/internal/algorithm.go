package internal

import (
	"math"
	"time"
)

// DefaultParameters contains the FSRS-6 default parameter set (w0-w20).
var DefaultParameters = Parameters{
	W: [21]float64{
		0.212, 1.2931, 2.3065, 8.2956, // w0-w3: initial stability per rating
		6.4133, 0.8334, 3.0194, 0.001, // w4-w7: difficulty params
		1.8722, 0.1666, 0.796, 1.4835, // w8-w11: stability update params
		0.0614, 0.2629, 1.6483, 0.6014, // w12-w15: stability update params
		1.8729, 0.5425, 0.0912, // w16-w18: easy bonus, same-day params
		0.0658, 0.1542, // w19-w20: unused, decay exponent
	},
	DesiredRetention: 0.9,
}

const (
	minDifficulty = 1.0
	maxDifficulty = 10.0
	minStability  = 0.1
	maxInterval   = 36500 // 100 years in days
)

// Parameters holds the 21 FSRS-6 parameters and desired retention.
type Parameters struct {
	W                [21]float64 `json:"w"`
	DesiredRetention float64     `json:"desired_retention"`
}

// FSRS implements the FSRS-6 scheduling algorithm.
type FSRS struct {
	Params Parameters
}

// NewFSRS creates a new FSRS scheduler with default parameters.
func NewFSRS() *FSRS {
	return &FSRS{Params: DefaultParameters}
}

// InitialStability returns S0 for a given rating.
// S0(rating) = w[rating-1]
func (f *FSRS) InitialStability(rating Rating) float64 {
	return math.Max(f.Params.W[int(rating)-1], minStability)
}

// InitialDifficulty returns D0 for a given rating.
// D0(rating) = w4 - exp(w5 * (rating - 1)) + 1, clamped [1, 10]
func (f *FSRS) InitialDifficulty(rating Rating) float64 {
	d := f.Params.W[4] - math.Exp(f.Params.W[5]*(float64(rating)-1)) + 1
	return clamp(d, minDifficulty, maxDifficulty)
}

// UpdateDifficulty computes new difficulty after a review.
//
//	deltaD = -w6 * (rating - 3)
//	D' = D + deltaD * (10 - D) / 9        (linear damping)
//	D'' = w7 * D0(4) + (1 - w7) * D'      (mean reversion)
//	clamp(D'', 1, 10)
func (f *FSRS) UpdateDifficulty(d float64, rating Rating) float64 {
	deltaD := -f.Params.W[6] * (float64(rating) - 3)
	dPrime := d + deltaD*(10-d)/9
	d0Easy := f.InitialDifficulty(Easy) // D0(4)
	dDoublePrime := f.Params.W[7]*d0Easy + (1-f.Params.W[7])*dPrime
	return clamp(dDoublePrime, minDifficulty, maxDifficulty)
}

// Retrievability computes the probability of recall after t days with stability S.
// R(t, S) = (1 + factor * t / S)^(-w20)
// where factor = 0.9^(-1/w20) - 1
func (f *FSRS) Retrievability(elapsedDays int, stability float64) float64 {
	if stability <= 0 {
		return 0
	}
	t := float64(elapsedDays)
	w20 := f.Params.W[20]
	factor := math.Pow(0.9, -1.0/w20) - 1
	return math.Pow(1+factor*t/stability, -w20)
}

// NextInterval computes the next review interval in days.
// interval = (S / factor) * (desired_retention^(-1/w20) - 1)
// clamped to [1, 36500]
func (f *FSRS) NextInterval(stability float64) int {
	w20 := f.Params.W[20]
	factor := math.Pow(0.9, -1.0/w20) - 1
	interval := (stability / factor) * (math.Pow(f.Params.DesiredRetention, -1.0/w20) - 1)
	rounded := int(math.Round(interval))
	if rounded < 1 {
		rounded = 1
	}
	if rounded > maxInterval {
		rounded = maxInterval
	}
	return rounded
}

// StabilityAfterSuccess computes new stability after a successful review (rating >= 2).
// S' = S * (exp(w8) * (11 - D) * S^(-w9) * (exp(w10*(1-R)) - 1) * hard_penalty * easy_bonus + 1)
func (f *FSRS) StabilityAfterSuccess(d, s float64, r float64, rating Rating) float64 {
	w := f.Params.W
	hardPenalty := 1.0
	if rating == Hard {
		hardPenalty = w[15]
	}
	easyBonus := 1.0
	if rating == Easy {
		easyBonus = w[16]
	}

	inner := math.Exp(w[8]) * (11 - d) * math.Pow(s, -w[9]) *
		(math.Exp(w[10]*(1-r)) - 1) * hardPenalty * easyBonus
	newS := s * (inner + 1)
	return math.Max(newS, minStability)
}

// StabilityAfterFailure computes new stability after a failed review (rating == 1).
// S_fail = w11 * D^(-w12) * (S^w13 * (exp(w14*(1-R)) - 1))
// S' = min(S_fail, S)  -- stability can't increase on failure
func (f *FSRS) StabilityAfterFailure(d, s float64, r float64) float64 {
	w := f.Params.W
	sFail := w[11] * math.Pow(d, -w[12]) * (math.Pow(s, w[13]) * (math.Exp(w[14]*(1-r)) - 1))
	// Stability can never increase on failure.
	newS := math.Min(sFail, s)
	return math.Max(newS, minStability)
}

// Review processes a card review and returns the updated card.
// This is the main entry point for scheduling. It modifies the card in place
// and also returns it for convenience.
func (f *FSRS) Review(card *Card, rating Rating, now time.Time) *Card {
	now = now.UTC().Truncate(time.Second)

	// Calculate elapsed days since last review.
	elapsedDays := 0
	if !card.LastReview.IsZero() {
		elapsed := now.Sub(card.LastReview)
		elapsedDays = int(elapsed.Hours() / 24)
		if elapsedDays < 0 {
			elapsedDays = 0
		}
	}
	card.ElapsedDays = elapsedDays

	if card.State == New {
		// First review: initialize stability and difficulty.
		card.Stability = f.InitialStability(rating)
		card.Difficulty = f.InitialDifficulty(rating)
		card.State = Learning

		if rating == Again {
			card.Lapses++
		}
	} else {
		// Subsequent reviews: update stability and difficulty.
		r := f.Retrievability(elapsedDays, card.Stability)

		// Update difficulty.
		card.Difficulty = f.UpdateDifficulty(card.Difficulty, rating)

		// Update stability.
		if rating == Again {
			card.Stability = f.StabilityAfterFailure(card.Difficulty, card.Stability, r)
			card.Lapses++
			card.State = Learning
		} else {
			card.Stability = f.StabilityAfterSuccess(card.Difficulty, card.Stability, r, rating)
			card.State = Review
		}
	}

	card.Reviews++
	card.LastReview = now

	// Compute next due date.
	interval := f.NextInterval(card.Stability)
	card.Due = now.Add(time.Duration(interval) * 24 * time.Hour)

	return card
}

// clamp restricts v to [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
