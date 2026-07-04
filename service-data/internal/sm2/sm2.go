// Package sm2 implements the SuperMemo-2 spaced-repetition algorithm.
// It is pure (no I/O) so it can be unit-tested in isolation.
package sm2

// MinEase is the floor for the ease factor, per the classic SM-2 spec.
const MinEase = 1.30

// State is the mutable memory state of a single card.
type State struct {
	EaseFactor  float64 // EF, >= MinEase
	Interval    int     // days until next review
	Repetitions int     // consecutive correct answers
}

// Next applies one review graded with quality q (0..5) and returns the
// updated state. q < 3 counts as a lapse: repetitions reset and the card is
// scheduled for tomorrow. The ease factor is always adjusted and clamped.
func Next(s State, q int) State {
	if q < 0 {
		q = 0
	}
	if q > 5 {
		q = 5
	}

	next := s
	if q < 3 {
		next.Repetitions = 0
		next.Interval = 1
	} else {
		switch next.Repetitions {
		case 0:
			next.Interval = 1
		case 1:
			next.Interval = 6
		default:
			next.Interval = int(round(float64(next.Interval) * next.EaseFactor))
		}
		next.Repetitions++
	}

	qf := float64(q)
	next.EaseFactor = next.EaseFactor + (0.1 - (5-qf)*(0.08+(5-qf)*0.02))
	if next.EaseFactor < MinEase {
		next.EaseFactor = MinEase
	}
	return next
}

func round(f float64) float64 {
	if f < 0 {
		return float64(int(f - 0.5))
	}
	return float64(int(f + 0.5))
}
