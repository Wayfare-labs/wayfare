package server

import (
	"encoding/json"
	"testing"

	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// TestThresholdsEndpointTiesWireValuesToConstants asserts that GET /api/thresholds
// serves the exact verdict thresholds and divergence bands defined in route
// and refrate package code constants (issue #626).
func TestThresholdsEndpointTiesWireValuesToConstants(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	raw := jsonBody(t, srv.URL+"/api/thresholds")
	var got ThresholdsJSON
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshaling /api/thresholds body: %v", err)
	}

	// Tie verdict thresholds to route package constants
	if got.VerdictThresholds.Good != route.ThresholdGood.String() {
		t.Errorf("VerdictThresholds.Good = %q, want %q", got.VerdictThresholds.Good, route.ThresholdGood.String())
	}
	if got.VerdictThresholds.Fair != route.ThresholdFair.String() {
		t.Errorf("VerdictThresholds.Fair = %q, want %q", got.VerdictThresholds.Fair, route.ThresholdFair.String())
	}
	if got.VerdictThresholds.Poor != route.ThresholdPoor.String() {
		t.Errorf("VerdictThresholds.Poor = %q, want %q", got.VerdictThresholds.Poor, route.ThresholdPoor.String())
	}

	// Tie divergence thresholds to refrate package constants
	if got.DivergenceThresholds.Agree != refrate.DivergenceAgree.String() {
		t.Errorf("DivergenceThresholds.Agree = %q, want %q", got.DivergenceThresholds.Agree, refrate.DivergenceAgree.String())
	}
	if got.DivergenceThresholds.Malfunction != refrate.DivergenceMalfunction.String() {
		t.Errorf("DivergenceThresholds.Malfunction = %q, want %q", got.DivergenceThresholds.Malfunction, refrate.DivergenceMalfunction.String())
	}
}

// TestThresholdsJSONContentType pins that /api/thresholds returns application/json.
func TestThresholdsJSONContentType(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	jsonBody(t, srv.URL+"/api/thresholds")
}
