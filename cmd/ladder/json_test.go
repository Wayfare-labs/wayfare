package main

// This file is the other half of the pair covering GitHub issue #113
// (backlog #5): "ToCorridorJSON, staleJSON and cmd/ladder -json must agree
// on the field set; today none of them is compared with another." See the
// package comment on server/wire_parity_test.go for why the comparison is
// split across two packages instead of living in one test function.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
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

// TestLadderJSONMatchesToCorridorJSON pins that -json mode emits exactly
// route.ToCorridorJSON's own encoding of the ladder result — the same bytes,
// not merely the same shape.
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

	var got bytes.Buffer
	if err := encodeCorridorJSON(&got, result, pair); err != nil {
		t.Fatalf("encodeCorridorJSON: %v", err)
	}

	want, err := json.MarshalIndent(route.ToCorridorJSON(result, pair), "", "  ")
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
// independently rules that out.
func TestLadderJSONFieldSetIsNotHandRolled(t *testing.T) {
	result := sampleLadderResult()
	pair := "USD/NGN"

	var buf bytes.Buffer
	if err := encodeCorridorJSON(&buf, result, pair); err != nil {
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
