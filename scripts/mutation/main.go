//go:build ignore

// Command mutation applies the hand-written mutations catalogued in
// docs/qa/artifacts/667-mutation-catalogue.md to the pricing path, one at a
// time, runs the test suite against each, and reports which tests kill it.
//
// Boundary tests exist for verdictFor, classify, the strict-send refusals and
// the conservative-mid selection, but a test that executes a line is not a
// test that would notice the line changing (#667). This measures the second
// thing directly: delete a refusal or flip a band boundary, and see whether
// anything fails.
//
// Usage:
//
//	go run scripts/mutation/main.go                 # every mutation, ./...
//	go run scripts/mutation/main.go -run 'M0[1-5]'  # a subset, by id
//	go run scripts/mutation/main.go -json out.json  # also write raw results
//
// Each mutation is an exact text substitution that must match its file
// exactly once, so a refactor that moves the code makes the catalogue fail
// loudly ("anchor not found") instead of silently mutating nothing. The file
// is restored after every run, including on interrupt, and the tool refuses
// to start if a target file has uncommitted changes, so a crash can never
// leave a mutant behind that is indistinguishable from an edit.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

type mutation struct {
	ID     string
	Area   string
	File   string
	What   string // what the mutation simulates
	Old    string
	New    string
	Expect string // "killed" unless the catalogue documents an equivalent mutant
}

// The catalogue. IDs are stable: the artifact's baseline table refers to them.
var mutations = []mutation{
	// verdictFor — route/route.go
	{"M01", "verdictFor", "route/route.go", "Good band excludes its 3% boundary",
		"case lossPct.LessThanOrEqual(ThresholdGood):", "case lossPct.LessThan(ThresholdGood):", "killed"},
	{"M02", "verdictFor", "route/route.go", "Fair band excludes its 8% boundary",
		"case lossPct.LessThanOrEqual(ThresholdFair):", "case lossPct.LessThan(ThresholdFair):", "killed"},
	{"M03", "verdictFor", "route/route.go", "Poor band excludes its 20% boundary",
		"case lossPct.LessThanOrEqual(ThresholdPoor):", "case lossPct.LessThan(ThresholdPoor):", "killed"},
	{"M04", "verdictFor", "route/route.go", "Unusable band deleted: everything past Poor grades Poor",
		"\tdefault:\n\t\treturn VerdictUnusable\n\t}\n}", "\tdefault:\n\t\treturn VerdictPoor\n\t}\n}", "killed"},
	{"M05", "verdictFor", "route/route.go", "Good and Fair swapped",
		"case lossPct.LessThanOrEqual(ThresholdGood):\n\t\treturn VerdictGood",
		"case lossPct.LessThanOrEqual(ThresholdGood):\n\t\treturn VerdictFair", "killed"},
	{"M06", "verdictFor", "route/route.go", "Negative-loss clamp deleted: a better-than-mid rate reports a negative cost",
		"if q.LossPct.IsNegative() {\n\t\tq.LossPct = decimal.Zero\n\t}", "_ = 0", "killed"},

	// classify — route/route.go
	{"M07", "classify", "route/route.go", "No paths classified DIRECT instead of NO-MARKET",
		"if len(paths) == 0 {\n\t\treturn IntegrityNoMarket, nil, nil", "if len(paths) == 0 {\n\t\treturn IntegrityDirect, nil, nil", "killed"},
	{"M08", "classify", "route/route.go", "Destination-as-own-hop skip deleted",
		"if h.Code == dest.Code && h.Issuer == dest.Issuer {\n\t\t\t\tcontinue\n\t\t\t}", "_ = dest", "killed"},
	{"M09", "classify", "route/route.go", "Off by one: a path with one fiat hop counts as independent",
		"if fiatHops == 0 {\n\t\t\tindependent = true", "if fiatHops <= 1 {\n\t\t\tindependent = true", "killed"},
	{"M10", "classify", "route/route.go", "A fiat hop is counted but not recorded as a dependency",
		"fiatHops++\n\t\t\t\tseen[h.Code+\":\"+h.Issuer] = h", "fiatHops++", "killed"},
	{"M11", "classify", "route/route.go", "An unregistered hop is treated as a fiat dependency",
		"unknown[h.Code+\":\"+h.Issuer] = h", "unknown[h.Code+\":\"+h.Issuer] = h\n\t\t\t\tfiatHops++", "killed"},
	{"M12", "classify", "route/route.go", "Any independent path no longer makes the corridor DIRECT",
		"if fiatHops == 0 {\n\t\t\tindependent = true\n\t\t}", "_ = independent", "killed"},

	// strict-send refusals — dex/dex.go
	{"M13", "strict-send refusal", "dex/dex.go", "Null path record refusal deleted",
		"if strings.TrimSpace(string(raw)) == \"null\" {", "if false && strings.TrimSpace(string(raw)) == \"null\" {", "killed"},
	{"M14", "strict-send refusal", "dex/dex.go", "Zero computed amount accepted (only negative refused)",
		"if !computed.IsPositive() {", "if computed.IsNegative() {", "killed"},
	{"M15", "strict-send refusal", "dex/dex.go", "Decimal-places refusal off by one: 8 places accepted",
		"if computed.Exponent() < stellarMaxScale {", "if computed.Exponent() < stellarMaxScale-1 {", "killed"},
	{"M16", "strict-send refusal", "dex/dex.go", "Destination exponent guard deleted",
		"if absExponent(dstAmt) > maxDecimalExponent {", "if false && absExponent(dstAmt) > maxDecimalExponent {", "killed"},
	{"M17", "strict-send refusal", "dex/dex.go", "Unrecognised hop asset_type silently dropped instead of refused",
		"return nil, fmt.Errorf(\"dex: path hop has unrecognised asset_type %q\", h.AssetType)", "continue", "killed"},
	{"M18", "strict-send refusal", "dex/dex.go", "Strict-send validates source_amount instead of the computed destination_amount",
		"if direction == \"strict-send\" {", "if direction == \"strict-receive\" {", "killed"},

	// conservative-mid selection — refrate/cross.go
	{"M19", "conservative mid", "refrate/cross.go", "Disagreeing providers scored against the smaller (flattering) mid",
		"if secondary.Mid.GreaterThan(primary.Mid) {\n\t\t\tout.Mid, out.Source, out.AsOf = secondary.Mid",
		"if secondary.Mid.LessThan(primary.Mid) {\n\t\t\tout.Mid, out.Source, out.AsOf = secondary.Mid", "killed"},
	{"M20", "conservative mid", "refrate/cross.go", "Disagree band includes its 2% boundary",
		"case out.DivergencePct.GreaterThan(DivergenceAgree):", "case out.DivergencePct.GreaterThanOrEqual(DivergenceAgree):", "killed"},
	{"M21", "conservative mid", "refrate/cross.go", "Malfunction band includes its boundary",
		"case out.DivergencePct.GreaterThan(DivergenceMalfunction):", "case out.DivergencePct.GreaterThanOrEqual(DivergenceMalfunction):", "killed"},
	{"M22", "conservative mid", "refrate/cross.go", "Divergence measured relative to the larger mid",
		"out.DivergencePct = hi.Sub(lo).Div(lo)", "out.DivergencePct = hi.Sub(lo).Div(hi)", "killed"},
	{"M23", "conservative mid", "refrate/cross.go", "Stale pair scored against the older mid",
		"if secondary.AsOf.After(primary.AsOf) {", "if secondary.AsOf.Before(primary.AsOf) {", "killed"},
	{"M24", "conservative mid", "refrate/cross.go", "Zero-mid guard checks only the primary",
		"if primary.Mid.IsZero() || secondary.Mid.IsZero() {", "if primary.Mid.IsZero() {", "killed"},
}

type result struct {
	mutation
	Outcome string   `json:"outcome"` // killed, survived, invalid (did not compile), error
	Killers []string `json:"killers"` // package.Test names that failed
	Seconds float64  `json:"seconds"`
	Detail  string   `json:"detail,omitempty"`
}

func main() {
	run := flag.String("run", "", "regexp selecting mutation ids")
	pkgs := flag.String("pkgs", "./...", "packages to test against each mutant")
	jsonOut := flag.String("json", "", "write raw results to this file")
	flag.Parse()

	var sel *regexp.Regexp
	if *run != "" {
		sel = regexp.MustCompile(*run)
	}

	// Refuse to mutate files with local edits: the restore writes back the
	// bytes read here, and those must be the committed ones.
	files := map[string]bool{}
	for _, m := range mutations {
		files[m.File] = true
	}
	for f := range files {
		if err := exec.Command("git", "diff", "--quiet", "HEAD", "--", f).Run(); err != nil {
			fatalf("%s has uncommitted changes; commit or stash them first so a mutant can never be mistaken for an edit", f)
		}
	}

	var restore func()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		if restore != nil {
			restore()
		}
		os.Exit(130)
	}()

	var results []result
	for _, m := range mutations {
		if sel != nil && !sel.MatchString(m.ID) {
			continue
		}
		orig, err := os.ReadFile(m.File)
		if err != nil {
			fatalf("%s: %v", m.ID, err)
		}
		if n := bytes.Count(orig, []byte(m.Old)); n != 1 {
			fatalf("%s: anchor found %d times in %s, want exactly 1; the code moved and the catalogue must be updated", m.ID, n, m.File)
		}
		restore = func() { _ = os.WriteFile(m.File, orig, 0o644) }
		mutated := bytes.Replace(orig, []byte(m.Old), []byte(m.New), 1)
		if err := os.WriteFile(m.File, mutated, 0o644); err != nil {
			restore()
			fatalf("%s: %v", m.ID, err)
		}

		fmt.Fprintf(os.Stderr, "%s %-22s ", m.ID, m.Area)
		r := runSuite(m, *pkgs)
		restore()
		restore = nil
		fmt.Fprintf(os.Stderr, "%-8s %5.1fs  %d killing test(s)\n", r.Outcome, r.Seconds, len(r.Killers))
		results = append(results, r)
	}

	printTable(results)
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			fatalf("writing %s: %v", *jsonOut, err)
		}
	}
	for _, r := range results {
		if r.Outcome != r.Expect {
			os.Exit(1)
		}
	}
}

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

func runSuite(m mutation, pkgs string) result {
	start := time.Now()
	cmd := exec.Command("go", "test", "-count=1", "-json", pkgs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, _ := cmd.Output()

	r := result{mutation: m}
	killers := map[string]bool{}
	buildFailed := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ev testEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Action == "fail" && ev.Test != "" {
			// Report the top-level test; subtests are named under it.
			top, _, _ := strings.Cut(ev.Test, "/")
			pkg := ev.Package[strings.LastIndex(ev.Package, "/")+1:]
			killers[pkg+"."+top] = true
		}
		if ev.Action == "build-fail" || (ev.Action == "output" && strings.Contains(ev.Output, "[build failed]")) {
			buildFailed = true
		}
	}
	if strings.Contains(stderr.String(), "build failed") || strings.Contains(stderr.String(), "# github.com/") {
		buildFailed = true
	}
	for k := range killers {
		r.Killers = append(r.Killers, k)
	}
	sort.Strings(r.Killers)
	r.Seconds = time.Since(start).Seconds()

	switch {
	case buildFailed:
		r.Outcome = "invalid"
		r.Detail = firstLines(stderr.String(), 5)
	case len(r.Killers) > 0:
		r.Outcome = "killed"
	default:
		r.Outcome = "survived"
	}
	return r
}

func printTable(results []result) {
	fmt.Println("| ID | Area | Mutation | Outcome | Killed by |")
	fmt.Println("|:---|:---|:---|:---|:---|")
	for _, r := range results {
		killers := "—"
		if len(r.Killers) > 0 {
			killers = "`" + strings.Join(r.Killers, "`, `") + "`"
		}
		fmt.Printf("| %s | %s | %s | %s | %s |\n", r.ID, r.Area, r.What, strings.ToUpper(r.Outcome), killers)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mutation: "+format+"\n", args...)
	os.Exit(2)
}
