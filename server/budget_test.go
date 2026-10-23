package server

import (
	"testing"
	"time"
)

func TestBudgetScalesWithRungsUpToCeiling(t *testing.T) {
	s := &Server{Timeout: 90 * time.Second}
	if a, b := s.budgetFor(3), s.budgetFor(6); a >= b {
		t.Errorf("budget did not grow with rungs: 3 rungs %s, 6 rungs %s", a, b)
	}
	if got := s.budgetFor(12); got >= 90*time.Second {
		t.Errorf("default ladder budget = %s, want it inside the 90s ceiling", got)
	}
	if got := s.budgetFor(maxSizes); got != 90*time.Second {
		t.Errorf("budget for %d rungs = %s, want the 90s ceiling", maxSizes, got)
	}
	small := &Server{Timeout: time.Second}
	if got := small.budgetFor(12); got != time.Second {
		t.Errorf("budget under a 1s ceiling = %s, want 1s", got)
	}
}
