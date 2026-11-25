package server

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The UI's money handling (#699) is JavaScript inside server/index.html, so the
// regression this guards is a `parseFloat` reappearing on a money field: it is
// exactly the shortcut a later edit reaches for. The ordering itself is checked
// by running the shipped helpers under node; where node is absent the ordering
// test skips rather than passes quietly.

func indexHTML(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	return string(b)
}

func TestNoBinaryFloatingPointOnMoneyFields(t *testing.T) {
	page := indexHTML(t)

	// A mention in prose is fine; a call is not.
	call := regexp.MustCompile(`parseFloat\s*\(`)
	if loc := call.FindStringIndex(page); loc != nil {
		line := strings.Count(page[:loc[0]], "\n") + 1
		t.Fatalf("index.html:%d calls parseFloat; money and percentage figures are decimal strings", line)
	}
}

// decimalHelpers extracts the shipped helper source so the test runs the code
// the page runs, not a copy of it.
func decimalHelpers(t *testing.T) string {
	t.Helper()
	page := indexHTML(t)
	start := strings.Index(page, "// --- decimal strings, never binary floating point")
	if start < 0 {
		t.Fatal("decimal helpers not found in index.html")
	}
	end := strings.Index(page[start:], "// formatPct rounds a loss percentage")
	if end < 0 {
		t.Fatal("formatPct not found after the helpers")
	}
	return page[start : start+end]
}

func TestDecimalHelpersOrderAdversarialValues(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the ordering of the shipped helpers is unchecked here")
	}

	// Pairs whose order a double gets wrong or makes equal when it should not.
	pairs := [][2]string{
		{"0.1", "0.2"},
		{"1.5", "1.50"},
		{"-1", "0"},
		{"-0", "0"},
		{"9.99999999999999999999", "10"},
		{"0.30000000000000004", "0.3"},
		{"100", "99.999999999999999999"},
		{"-2.5", "-2.05"},
		{"00012.5", "12.5"},
	}

	input, err := json.Marshal(pairs)
	if err != nil {
		t.Fatalf("marshal pairs: %v", err)
	}

	// expect[i] is the sign compareDecimalStrings must return for pair i.
	want := []int{-1, 0, -1, 0, -1, 1, 1, -1, 0}

	driver := decimalHelpers(t) + `
const pairs = JSON.parse(require('fs').readFileSync(0, 'utf8'));
const out = pairs.map(([a, b]) => Math.sign(compareDecimalStrings(a, b)));
const rounds = ['27.155', '27.154', '0.005', '9.999', '99.995', '-0.004', '7'].map(roundDecimal2);
require('fs').writeFileSync(1, JSON.stringify({ out, rounds }));
`
	cmd := exec.Command(node, "-e", driver)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run helpers under node: %v\n%s", err, stderr.String())
	}

	var got struct {
		Out    []int    `json:"out"`
		Rounds []string `json:"rounds"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode node output %q: %v", stdout.String(), err)
	}

	for i, pair := range pairs {
		if got.Out[i] != want[i] {
			t.Errorf("compareDecimalStrings(%q, %q) = %d, want %d", pair[0], pair[1], got.Out[i], want[i])
		}
	}

	wantRounds := []string{"27.16", "27.15", "0.01", "10.00", "100.00", "0.00", "7.00"}
	for i, want := range wantRounds {
		if got.Rounds[i] != want {
			t.Errorf("roundDecimal2 case %d = %q, want %q", i, got.Rounds[i], want)
		}
	}
}
