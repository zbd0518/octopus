package poolscheduledtest

import (
	"testing"
	"time"
)

// TestParseCronExpr_AcceptedShapes covers every shape the narrowed grammar
// accepts (execution decision for B4-#11): */n minute steps, * minute,
// fixed minute, * hour and fixed hour lists; D/M/DOW must be "*".
func TestParseCronExpr_AcceptedShapes(t *testing.T) {
	accepted := []string{
		"*/30 * * * *",
		"*/1 * * * *",
		"*/59 * * * *",
		"* * * * *",
		"0 * * * *",
		"59 * * * *",
		"15 * * * *",
		"0 9,21 * * *",
		"30 0,6,12,18 * * *",
		"45 23 * * *",
	}
	for _, expr := range accepted {
		if _, err := ParseCronExpr(expr); err != nil {
			t.Fatalf("ParseCronExpr(%q) = %v, want nil", expr, err)
		}
		if err := ValidateCronExpr(expr); err != nil {
			t.Fatalf("ValidateCronExpr(%q) = %v, want nil", expr, err)
		}
	}
}

// TestParseCronExpr_RejectedShapes locks in that unsupported Vixie features
// are rejected rather than silently misinterpreted.
func TestParseCronExpr_RejectedShapes(t *testing.T) {
	rejected := []string{
		"",              // empty
		"* * * *",       // 4 fields
		"* * * * * *",   // 6 fields
		"*/0 * * * *",   // step 0
		"*/60 * * * *",  // step > 59
		"*/-5 * * * *",  // negative step
		"60 * * * *",    // minute > 59
		"-1 * * * *",    // negative minute
		"1-30 * * * *",  // ranges unsupported
		"1,31 * * * *",  // minute lists unsupported (single fixed value only)
		"*/5 24 * * *",  // hour > 23
		"*/5 1-3 * * *", // hour ranges unsupported
		"*/5 * 1 * *",   // day-of-month unsupported
		"*/5 * * 2 *",   // month unsupported
		"*/5 * * * 1",   // day-of-week unsupported
		"*/5 */2 * * *", // hour steps unsupported
		"@daily",        // macros unsupported
	}
	for _, expr := range rejected {
		if _, err := ParseCronExpr(expr); err == nil {
			t.Fatalf("ParseCronExpr(%q) = nil error, want failure", expr)
		}
	}
}

func cronMustParse(t *testing.T, expr string) *Schedule {
	t.Helper()
	s, err := ParseCronExpr(expr)
	if err != nil {
		t.Fatalf("ParseCronExpr(%q): %v", expr, err)
	}
	return s
}

// TestNext_StepMinutes verifies */n steps only match minute 0 and multiples
// of n within each hour (Vixie semantics: the step never spills across hours).
func TestNext_StepMinutes(t *testing.T) {
	s := cronMustParse(t, "*/15 * * * *")
	base := time.Date(2026, 10, 4, 10, 0, 30, 0, time.UTC) // mid-minute

	want := time.Date(2026, 10, 4, 10, 15, 0, 0, time.UTC)
	if got := s.Next(base); !got.Equal(want) {
		t.Fatalf("Next(10:00:30) = %v, want %v", got, want)
	}
	// Exactly on a boundary: the run AT the instant is not returned again.
	onBoundary := time.Date(2026, 10, 4, 10, 15, 0, 0, time.UTC)
	want = time.Date(2026, 10, 4, 10, 30, 0, 0, time.UTC)
	if got := s.Next(onBoundary); !got.Equal(want) {
		t.Fatalf("Next(10:15:00) = %v, want %v", got, want)
	}
	// Step must reset each hour: minute 45 of hour 10 → next is 11:00, not 10:60.
	late := time.Date(2026, 10, 4, 10, 45, 1, 0, time.UTC)
	want = time.Date(2026, 10, 4, 11, 0, 0, 0, time.UTC)
	if got := s.Next(late); !got.Equal(want) {
		t.Fatalf("Next(10:45:01) = %v, want %v", got, want)
	}
}

// TestNext_FixedMinuteAndHourList verifies "0 9,21 * * *" semantics.
func TestNext_FixedMinuteAndHourList(t *testing.T) {
	s := cronMustParse(t, "0 9,21 * * *")
	base := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	want := time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)
	if got := s.Next(base); !got.Equal(want) {
		t.Fatalf("Next(10:00) = %v, want %v", got, want)
	}
	// After the last hour of the day the next match rolls over to tomorrow.
	late := time.Date(2026, 10, 4, 21, 0, 5, 0, time.UTC)
	want = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if got := s.Next(late); !got.Equal(want) {
		t.Fatalf("Next(21:00:05) = %v, want %v", got, want)
	}
}

// TestNext_AllMinutesEveryHour verifies the "*" minute field fires on the
// very next minute boundary.
func TestNext_AllMinutesEveryHour(t *testing.T) {
	s := cronMustParse(t, "* * * * *")
	base := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	want := time.Date(2026, 10, 4, 10, 1, 0, 0, time.UTC)
	if got := s.Next(base); !got.Equal(want) {
		t.Fatalf("Next(10:00:00) = %v, want %v", got, want)
	}
}

// TestNext_SingleFixedMinute verifies minute-only expressions run hourly.
func TestNext_SingleFixedMinute(t *testing.T) {
	s := cronMustParse(t, "45 * * * *")
	base := time.Date(2026, 10, 4, 10, 45, 0, 0, time.UTC)
	want := time.Date(2026, 10, 4, 11, 45, 0, 0, time.UTC)
	if got := s.Next(base); !got.Equal(want) {
		t.Fatalf("Next(10:45:00) = %v, want %v", got, want)
	}
}

// TestNext_MonotonicSequence walks a chain of Next() calls and asserts strict
// monotonicity — the property the runner's schedule-advance relies on.
func TestNext_MonotonicSequence(t *testing.T) {
	s := cronMustParse(t, "*/30 * * * *")
	cur := time.Date(2026, 10, 4, 23, 40, 0, 0, time.UTC)
	prev := cur
	for i := 0; i < 24; i++ {
		next := s.Next(cur)
		if next.IsZero() {
			t.Fatalf("Next returned zero at %v", cur)
		}
		if !next.After(prev) {
			t.Fatalf("non-monotonic sequence: %v -> %v", prev, next)
		}
		prev = next
		cur = next
	}
}
