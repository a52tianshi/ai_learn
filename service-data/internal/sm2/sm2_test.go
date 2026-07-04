package sm2

import "testing"

func TestFirstCorrectAnswers(t *testing.T) {
	s := State{EaseFactor: 2.5, Interval: 0, Repetitions: 0}

	s = Next(s, 5) // 1st correct -> interval 1
	if s.Interval != 1 || s.Repetitions != 1 {
		t.Fatalf("after 1st: interval=%d reps=%d, want 1/1", s.Interval, s.Repetitions)
	}

	s = Next(s, 5) // 2nd correct -> interval 6
	if s.Interval != 6 || s.Repetitions != 2 {
		t.Fatalf("after 2nd: interval=%d reps=%d, want 6/2", s.Interval, s.Repetitions)
	}

	s = Next(s, 4) // 3rd correct -> interval = round(6 * EF)
	if s.Interval < 13 || s.Interval > 17 {
		t.Fatalf("after 3rd: interval=%d, want ~15", s.Interval)
	}
}

func TestLapseResets(t *testing.T) {
	s := State{EaseFactor: 2.5, Interval: 30, Repetitions: 4}
	s = Next(s, 1) // wrong
	if s.Repetitions != 0 {
		t.Fatalf("reps=%d, want 0 after lapse", s.Repetitions)
	}
	if s.Interval != 1 {
		t.Fatalf("interval=%d, want 1 after lapse", s.Interval)
	}
}

func TestEaseClampedToFloor(t *testing.T) {
	s := State{EaseFactor: 1.3, Interval: 1, Repetitions: 1}
	for i := 0; i < 10; i++ {
		s = Next(s, 0) // repeated total blackouts
	}
	if s.EaseFactor < MinEase {
		t.Fatalf("ease=%f fell below floor %f", s.EaseFactor, MinEase)
	}
}

func TestQualityClamped(t *testing.T) {
	// out-of-range quality must not panic and behaves as clamped
	s := Next(State{EaseFactor: 2.5}, 99)
	if s.Repetitions != 1 {
		t.Fatalf("reps=%d, want 1 (q clamped to 5)", s.Repetitions)
	}
}
