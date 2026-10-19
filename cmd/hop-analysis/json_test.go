package main

import (
	"encoding/json"
	"testing"
)

// TestReportJSONShape pins the keys -json emits, which docs/native-xlm-routing.md
// documents for consumers piping the output into jq.
func TestReportJSONShape(t *testing.T) {
	report, err := Analyse("../../testdata/snapshots")
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"corridors", "note"} {
		if _, ok := top[k]; !ok {
			t.Errorf("report is missing top-level key %q", k)
		}
	}

	var corridors []map[string]json.RawMessage
	if err := json.Unmarshal(top["corridors"], &corridors); err != nil || len(corridors) == 0 {
		t.Fatalf("corridors: %v (n=%d)", err, len(corridors))
	}
	for _, k := range []string{"snapshot", "send", "receive", "sizes_measured", "sizes_parsed",
		"sizes_with_any_path", "sizes_best_uses_xlm", "sizes_with_non_xlm_alt", "summary", "sizes"} {
		if _, ok := corridors[0][k]; !ok {
			t.Errorf("corridor is missing key %q", k)
		}
	}
}
