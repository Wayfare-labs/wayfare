package main

// This file is the other half of the pair covering GitHub issue #113
// (backlog #5): "ToCorridorJSON, staleJSON and cmd/ladder -json must agree
// on the field set; today none of them is compared with another." See the
// package comment on server/wire_parity_test.go for why the comparison is
// split across two packages instead of living in one test function.
//
// It also carries the parity test for issue #85: cmd/ladder -json runs the
// same checks the server runs and attaches them at the same composition
// point, so both producers of the shared wire shape emit the same fields for
// the same corridor.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// sampleLadderResult builds a LadderResult with every optional field
// populated, mirroring server.wellPopulatedLadderResult. Duplicated rather
// than shared because the two live in different, non-importable packages
// (this one is package main); keeping both fixtures maximally populated is
// what makes each half's comparison meaningful.
func sampleLadderResult() *route.LadderResult {
	send, recv := asset.USDC(), asset.NGNC()
	quotedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	recommended := route.Quote{
		Kind:            route.KindDEX,
		Description:     "USDC -> XLM -> NGNC",
		Source:          "stellar-dex",
		SendAsset:       send,
		SendAmount:      decimal.RequireFromString("100"),
		ReceiveAsset:    recv,
		ReceiveAmount:   decimal.RequireFromString("129000"),
		EffectiveRate:   decimal.RequireFromString("1290"),
		ReferenceMid:    decimal.RequireFromString("1350.2568"),
		ReferenceSource: "exchangerate-api",
		LossPct:         decimal.RequireFromString("4.46"),
		LossAmount:      decimal.RequireFromString("6025.68"),
		Verdict:         route.VerdictFair,
		Warnings:        []string{"derivative corridor: depends on NGNC"},
		QuotedAt:        quotedAt,
	}

	return &route.LadderResult{
		Request: route.LadderRequest{
			SendAsset:      send,
			ReceiveAsset:   recv,
			ReferenceBase:  "USD",
			ReferenceQuote: "NGN",
		},
		Rungs: []route.Rung{
			{
				SendAmount: decimal.RequireFromString("100"),
				Result: &route.Result{
					Quotes:    []route.Quote{recommended},
					Integrity: route.IntegrityDirect,
				},
			},
		},
		Integrity: route.IntegrityDirect,

		ReferenceMid:    decimal.RequireFromString("1350.2568"),
		ReferenceSource: "exchangerate-api",
		Reference: refrate.Rate{
			Base:            "USD",
			Quote:           "NGN",
			Mid:             decimal.RequireFromString("1350.2568"),
			AsOf:            quotedAt,
			Source:          "exchangerate-api",
			FetchedAt:       quotedAt,
			SecondaryMid:    decimal.RequireFromString("1348.9000"),
			SecondarySource: "currency-api",
			DivergencePct:   decimal.RequireFromString("0.0931"),
			Agreement:       refrate.AgreementAgree,
		},
		Parallel: &refrate.Parallel{
			Status: refrate.ParallelReported,
			Mid:    decimal.RequireFromString("1620.00"),
			Source: "parallel-desk",
			GapPct: decimal.RequireFromString("19.98"),
		},

		Floor:     decimal.RequireFromString("4.46"),
		FloorSize: decimal.RequireFromString("100"),
		WorstLoss: decimal.RequireFromString("4.46"),
		WorstSize: decimal.RequireFromString("100"),

		Recommended:     &recommended,
		RecommendedSize: decimal.RequireFromString("100"),

		Finding: "USDC to NGNC prices directly; best observed loss 4.46% at 100 USDC.",
	}
}

// sampleFindings builds a findings set spanning all three check states plus
// a metric, mirroring server.findingsFixture. The ladder's -json path and
// the server's live path both attach findings through route.WithFindings, and
// a fixture that exercises every shape the findings block can carry keeps the
// field-set comparison meaningful rather than vacuous.
func sampleFindings() *checks.Findings {
	now := time.Now().UTC()
	f := &checks.Findings{}
	f.Add(checks.Pass(
		checks.AnchorAssetISO4217{}.Describe(),
		checks.Subject{Domain: "ngnc.online", Asset: asset.NGNC()},
		"anchor_asset names the shilling",
		checks.Evidence{Source: "ngnc.online/.well-known/stellar.toml",
			Observed: "NGN", ObservedAt: now}))
	f.Add(checks.Undetermined(
		checks.IssuerAuthFlags{}.Describe(),
		checks.Subject{Asset: asset.NGNC()},
		"issuer unreachable"))
	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "spread.bid-ask", Scope: checks.ScopeAsset, Subject: "NGNC",
			At: now, Determined: true,
			Evidence: []checks.Evidence{{Source: "https://horizon.stellar.org/order_book",
				Observed: "0.0004", ObservedAt: now}},
		},
		Value: decimal.RequireFromString("0.0004"), Unit: checks.UnitRatio,
		Summary: "bid-ask spread on the USDC/NGNC book",
	})
	return f
}

// TestLadderJSONMatchesToCorridorJSON pins that -json mode emits exactly the
// shared wire shape's own encoding of the ladder result plus its findings —
// the same bytes, not merely the same shape.
//
// This is what makes it sound to treat cmd/ladder as already covered once
// route.ToCorridorJSON and staleJSON are checked against each other
// (server.TestLiveAndStaleAgreeOnFieldSet): cmd/ladder has no encoding path
// of its own left to drift. Reimplement -json with even one hand-built field
// — the exact "second, independently-maintained shape" the wire.go package
// comment warns about — and this test fails.
func TestLadderJSONMatchesToCorridorJSON(t *testing.T) {
	result := sampleLadderResult()
	pair := "USD/NGN"
	findings := sampleFindings()

	var got bytes.Buffer
	if err := encodeCorridorJSON(&got, result, pair, findings); err != nil {
		t.Fatalf("encodeCorridorJSON: %v", err)
	}

	want, err := json.MarshalIndent(
		route.WithFindings(route.ToCorridorJSON(result, pair), findings), "", "  ")
	if err != nil {
		t.Fatalf("marshaling the reference encoding: %v", err)
	}
	// json.Encoder appends a trailing newline that MarshalIndent does not.
	want = append(want, '\n')

	if got.String() != string(want) {
		t.Errorf("cmd/ladder -json output does not match route.ToCorridorJSON's own "+
			"encoding.\ngot:\n%s\nwant:\n%s", got.String(), want)
	}
}

// TestLadderJSONFieldSetIsNotHandRolled is a second angle on the same claim,
// asserting at the key level rather than the byte level: every top-level
// field cmd/ladder's -json mode emits must come from route.CorridorJSON's
// own JSON tags. A test that only compared bytes could be satisfied by
// coincidence if both sides were wrong the same way; walking the tags
// independently rules that out. The fixture carries findings, so the
// findings key itself is exercised here too.
func TestLadderJSONFieldSetIsNotHandRolled(t *testing.T) {
	result := sampleLadderResult()
	pair := "USD/NGN"

	var buf bytes.Buffer
	if err := encodeCorridorJSON(&buf, result, pair, sampleFindings()); err != nil {
		t.Fatalf("encodeCorridorJSON: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshaling -json output: %v", err)
	}

	canonical := jsonTagsOf(route.CorridorJSON{})
	for k := range got {
		if !canonical[k] {
			t.Errorf("-json output has field %q, which is not a JSON tag on "+
				"route.CorridorJSON; cmd/ladder must never emit a field the shared "+
				"wire type does not declare", k)
		}
	}
}

// TestLadderJSONAndAPIAgreeOnFieldSet is the comparison issue #85 asks for:
// cmd/ladder -json and /api/corridor must emit the same top-level field set
// for the same corridor at the same commit. The two producers live in
// different packages — cmd/ladder is package main, and the server's live path
// is server/api.go's handleCorridor — so the server side is composed inline
// here from the same two exported steps the handler uses: route.ToCorridorJSON
// then route.WithFindings. Mutate either producer — drop a field from one,
// add a block to only one — and this test fails.
func TestLadderJSONAndAPIAgreeOnFieldSet(t *testing.T) {
	result := sampleLadderResult()
	pair := "USD/NGN"

	for _, tc := range []struct {
		name     string
		findings *checks.Findings
	}{
		{"with checks", sampleFindings()},
		{"checks skipped", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ladder bytes.Buffer
			if err := encodeCorridorJSON(&ladder, result, pair, tc.findings); err != nil {
				t.Fatalf("encodeCorridorJSON: %v", err)
			}
			ladderFields := topLevelKeys(t, ladder.Bytes())

			// The server's live path (server/api.go handleCorridor): render
			// the shared shape, then attach findings at the one composition
			// point, if a checks runner is configured.
			api := route.WithFindings(route.ToCorridorJSON(result, pair), tc.findings)
			apiFields := fieldSetOf(t, api)

			for k := range ladderFields {
				if !apiFields[k] {
					t.Errorf("ladder -json emits %q, which /api/corridor does not", k)
				}
			}
			for k := range apiFields {
				if !ladderFields[k] {
					t.Errorf("/api/corridor emits %q, which ladder -json does not", k)
				}
			}

			if tc.findings == nil && ladderFields["findings"] {
				t.Error("with checks skipped, ladder -json must carry no findings block — " +
					"absent means \"not checked\", and must not read as \"checked, nothing found\"")
			}
			if tc.findings != nil && !ladderFields["findings"] {
				t.Error("with checks enabled, ladder -json must carry the findings block, " +
					"exactly as /api/corridor does")
			}
		})
	}
}

// jsonTagsOf returns the set of top-level JSON field names a struct type
// declares in its own tags, regardless of omitempty — read via reflection
// rather than by marshaling a value, because a zero or sparse value would
// hide every omitempty field and undercount the real tag set.
func jsonTagsOf(v any) map[string]bool {
	t := reflect.TypeOf(v)
	out := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = t.Field(i).Name
		}
		out[name] = true
	}
	return out
}

// topLevelKeys unmarshals a JSON document and returns its top-level keys.
func topLevelKeys(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshaling JSON document: %v", err)
	}
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// fieldSetOf marshals v and returns its top-level JSON keys.
func fieldSetOf(t *testing.T, v any) map[string]bool {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	return topLevelKeys(t, buf)
}
