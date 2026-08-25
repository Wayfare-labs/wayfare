package runstore

import (
	"context"
	"testing"
	"time"
)

func TestDetectTransitions_NoChange(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-NGNC"

	// Write two consecutive runs with the same integrity state.
	for i := 0; i < 2; i++ {
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  "DIRECT",
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 0 {
		t.Errorf("expected no transitions, got %d", len(transitions))
	}
}

func TestDetectTransitions_IntegrityStateChange(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-NGNC"

	// Write two consecutive runs with different integrity states.
	for i, integrity := range []string{"DIRECT", "DERIVATIVE"} {
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  integrity,
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if integrity == "DERIVATIVE" {
			rec.DependsOn = []string{"NGNC"}
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	tr := transitions[0]
	if tr.PreviousIntegrity != "DIRECT" {
		t.Errorf("PreviousIntegrity = %s, want DIRECT", tr.PreviousIntegrity)
	}
	if tr.CurrentIntegrity != "DERIVATIVE" {
		t.Errorf("CurrentIntegrity = %s, want DERIVATIVE", tr.CurrentIntegrity)
	}
	if tr.TransitionType != TransitionIntegrityStateChange {
		t.Errorf("TransitionType = %v, want TransitionIntegrityStateChange", tr.TransitionType)
	}
	if tr.PreviousReferenceSource != "exchangerate-api" {
		t.Errorf("PreviousReferenceSource = %s, want exchangerate-api", tr.PreviousReferenceSource)
	}
	if tr.CurrentReferenceSource != "exchangerate-api" {
		t.Errorf("CurrentReferenceSource = %s, want exchangerate-api", tr.CurrentReferenceSource)
	}
}

func TestDetectTransitions_DependonsChanged(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-GHSC"

	// Write two consecutive runs with the same DERIVATIVE integrity
	// but different DependsOn sets.
	for i, deps := range [][]string{
		{"NGNC"},
		{"NGNC", "KESC"},
	} {
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  "DERIVATIVE",
			DependsOn:  deps,
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	tr := transitions[0]
	if tr.TransitionType != TransitionDependsOnChanged {
		t.Errorf("TransitionType = %v, want TransitionDependsOnChanged", tr.TransitionType)
	}
	if len(tr.PreviousDependsOn) != 1 || tr.PreviousDependsOn[0] != "NGNC" {
		t.Errorf("PreviousDependsOn = %v, want [NGNC]", tr.PreviousDependsOn)
	}
	if len(tr.CurrentDependsOn) != 2 {
		t.Errorf("CurrentDependsOn = %v, want [NGNC KESC]", tr.CurrentDependsOn)
	}
}

func TestDetectTransitions_UnknownExcluded(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-KESC"

	// Write three runs: DIRECT → UNKNOWN → DERIVATIVE
	// Only the UNKNOWN → DERIVATIVE transition should be excluded (not a real change).
	integrities := []string{"DIRECT", "UNKNOWN", "DERIVATIVE"}
	for i, integrity := range integrities {
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  integrity,
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if integrity == "DERIVATIVE" {
			rec.DependsOn = []string{"KESC"}
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}

	// DIRECT → UNKNOWN should be filtered (prev is UNKNOWN... no, prev is DIRECT).
	// Actually: DIRECT → UNKNOWN is filtered (curr is UNKNOWN), UNKNOWN → DERIVATIVE
	// is filtered (prev is UNKNOWN). So no transitions should be detected.
	if len(transitions) != 0 {
		t.Errorf("expected 0 transitions (UNKNOWN filtered), got %d", len(transitions))
		for _, tr := range transitions {
			t.Logf("  transition: %s → %s", tr.PreviousIntegrity, tr.CurrentIntegrity)
		}
	}
}

func TestDetectTransitions_NoMarketToPriceable(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-NGNC"

	// Write two runs: NO-MARKET → DIRECT
	for i, integrity := range []string{"NO-MARKET", "DIRECT"} {
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  integrity,
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	tr := transitions[0]
	if tr.PreviousIntegrity != "NO-MARKET" {
		t.Errorf("PreviousIntegrity = %s, want NO-MARKET", tr.PreviousIntegrity)
	}
	if tr.CurrentIntegrity != "DIRECT" {
		t.Errorf("CurrentIntegrity = %s, want DIRECT", tr.CurrentIntegrity)
	}
}

func TestDetectTransitions_CompleteHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-NGNC"

	// Write 105 runs: 102 DIRECT, then DERIVATIVE.
	// The old Recent(100) limit would miss the DIRECT→DERIVATIVE transition
	// at position 102, but All() loads the complete history.
	for i := 0; i < 105; i++ {
		integrity := "DIRECT"
		if i >= 102 {
			integrity = "DERIVATIVE"
		}
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 1, 1+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  integrity,
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	transitions, err := DetectTransitions(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition (at position 102→103), got %d", len(transitions))
	}

	tr := transitions[0]
	if tr.PreviousIntegrity != "DIRECT" {
		t.Errorf("PreviousIntegrity = %s, want DIRECT", tr.PreviousIntegrity)
	}
	if tr.CurrentIntegrity != "DERIVATIVE" {
		t.Errorf("CurrentIntegrity = %s, want DERIVATIVE", tr.CurrentIntegrity)
	}
}

func TestDetectLatestTransition(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	corridor := "USDC-NGNC"

	// Write four runs: DIRECT, DIRECT, DERIVATIVE, DERIVATIVE
	for i := 0; i < 4; i++ {
		integrity := "DIRECT"
		if i >= 2 {
			integrity = "DERIVATIVE"
		}
		rec := &Record{
			Version:    Version,
			Seq:        int64(i + 1),
			RecordedAt: time.Date(2026, 8, 20+i, 12, 0, 0, 0, time.UTC),
			Corridor:   corridor,
			Integrity:  integrity,
			DependsOn:  []string{},
			Reference: Reference{
				Mid:    "1500",
				Source: "exchangerate-api",
			},
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// DetectLatestTransition should only compare the two most recent runs.
	tr, err := DetectLatestTransition(ctx, store, corridor)
	if err != nil {
		t.Fatal(err)
	}

	// The two most recent runs are both DERIVATIVE, so no transition.
	if tr != nil {
		t.Errorf("expected no transition between latest runs, got: %s → %s",
			tr.PreviousIntegrity, tr.CurrentIntegrity)
	}
}

func TestDepsetsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"equal", []string{"KESC", "NGNC"}, []string{"KESC", "NGNC"}, true},
		{"same elements different order", []string{"NGNC", "KESC"}, []string{"KESC", "NGNC"}, true},
		{"different length", []string{"KESC"}, []string{"KESC", "NGNC"}, false},
		{"different values", []string{"KESC"}, []string{"NGNC"}, false},
		{"dedup one unique vs repeated", []string{"NGNC", "NGNC"}, []string{"NGNC"}, true},
		{"dedup with extra unique", []string{"NGNC", "NGNC", "KESC"}, []string{"NGNC", "KESC"}, true},
		{"dedup different unique counts", []string{"NGNC", "NGNC"}, []string{"KESC", "KESC"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := depsetsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("depsetsEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestTransitionType_String(t *testing.T) {
	tests := []struct {
		typ  TransitionType
		want string
	}{
		{TransitionIntegrityStateChange, "INTEGRITY_STATE_CHANGE"},
		{TransitionDependsOnChanged, "DEPENDS_ON_CHANGED"},
		{TransitionType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("TransitionType(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}
