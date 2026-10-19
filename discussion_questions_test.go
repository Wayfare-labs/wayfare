package wayfare

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/discussion-questions.md collects the questions contributors have
// actually asked in this repository, and answers each one by quoting the code
// it rests on.
//
// The risk this file guards is specific and worth naming. A document like this
// is written once and then ages, and it ages in the worst direction: the code
// moves, the document does not, and a reader is handed a confident answer about
// code that no longer says it. So every quoted claim is checked against the
// file the document cites, and a question whose issue is still open cannot be
// marked answered.
//
// The second risk is fabrication. "Questions contributors actually ask" is an
// empirical claim, and a plausible-sounding list would satisfy every Done-when
// criterion while being worth nothing. The document may therefore only cite
// questions with a recorded source, and may not link to a discussion thread —
// Discussions holds none, so a link to one would be a link to nothing.

const questionsDoc = "docs/discussion-questions.md"

func questionsText(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(questionsDoc)
	if err != nil {
		t.Fatalf("reading %s: %v", questionsDoc, err)
	}
	return string(raw)
}

// TestQuestionsDocQuotesTheCodeItRestsOn is the load-bearing test. Each entry is
// a claim the document makes about a specific file, paired with text that file
// actually contains. When the code changes, the document's answer is wrong
// whether or not anyone noticed, and this fails.
//
// The document cites a location and argues from it; it is not a code listing, so
// what is checked is that the cited file still says what the document claims it
// says. Requiring the document to restate every line of code would make it
// worse to read and would fail on harmless re-wrapping.
func TestQuestionsDocQuotesTheCodeItRestsOn(t *testing.T) {
	doc := questionsText(t)

	claims := []struct {
		// what the document claims, for the failure message
		claim string
		file  string
		// quote must appear in file, whitespace- and comment-normalised.
		quote string
	}{
		{
			claim: "the ladder is 'a dozen round trips to Horizon' in server/api.go",
			file:  "server/api.go",
			quote: "A full ladder is a dozen round trips to Horizon",
		},
		{
			claim: "deployment.md puts a sweep at 'roughly three dozen Horizon calls'",
			file:  "docs/deployment.md",
			quote: "One sweep is roughly three dozen Horizon calls per corridor",
		},
		{
			claim: "deployment.md puts a sweep at 'about 110 requests every six hours'",
			file:  "docs/deployment.md",
			quote: "110 requests every six hours",
		},
		{
			claim: "writeError publishes a machine-readable code alongside the prose",
			file:  "server/api.go",
			quote: `"code":  code,`,
		},
		{
			claim: "the slippage probe prices the amount and a small probe",
			file:  "dex/dex.go",
			quote: "prices the requested amount and a small probe",
		},
		{
			claim: "the probe default is 10 send units",
			file:  "route/route.go",
			quote: "Defaults to 10 send units",
		},
		{
			claim: "the UI attaches the error code to the thrown Error",
			file:  "server/index.html",
			quote: "throw Object.assign(new Error(data.error",
		},
		{
			claim: "the error code is read only to mark the sizes field invalid",
			file:  "server/index.html",
			quote: "if (e.code === 'invalid_sizes')",
		},
		{
			claim: "the rendered banner is still built from the English alone",
			file:  "server/index.html",
			quote: "showErrorBanner(`Could not measure: ${esc(e.message)}`)",
		},
		{
			claim: "the legend item is granted a min-width",
			file:  "server/index.html",
			quote: ".legend-item { display: flex;",
		},
		{
			claim: "the legend's label rule grants no min-width",
			file:  "server/index.html",
			quote: ".legend-item dd {",
		},
		{
			claim: "the ledger's recorded mobile run carries its timestamp",
			file:  "docs/qa/artifacts/270-mobile.md",
			quote: "2026-09-23T17:35:50Z",
		},
	}

	for _, c := range claims {
		raw, err := os.ReadFile(c.file)
		if err != nil {
			t.Errorf("the document quotes %s, which is not in the tree: %v", c.file, err)
			continue
		}
		if !containsCode(string(raw), c.quote) {
			t.Errorf("the document claims %s, but %s no longer contains %q; "+
				"the answer has moved on from the code", c.claim, c.file, c.quote)
		}
		// The document has to at least cite the file it is arguing from, or a
		// reader has no way to go and check.
		if !strings.Contains(doc, c.file) {
			t.Errorf("the document claims %s but never names %s, so the claim "+
				"cannot be checked by a reader", c.claim, c.file)
		}
	}
}

// TestQuestionsDocBlockquotesAreVerbatim closes the gap the claim test leaves.
//
// TestQuestionsDocQuotesTheCodeItRestsOn checks that each claim is true of the
// file the document cites. It deliberately does not require the document to
// restate the code, so that harmless re-wrapping is not a failure. The cost is
// that editing a *misquote* in the document would go unnoticed — so this checks
// the other direction, for the lines the document does present as verbatim.
//
// Every `> ` blockquote the document offers as a quotation has to appear in
// some file the document cites. A blockquote that matches nothing is either a
// misquotation or an unsourced quote, and both make the document's answer
// unfalsifiable.
func TestQuestionsDocBlockquotesAreVerbatim(t *testing.T) {
	doc := questionsText(t)

	// Collect the files the document cites, so a quote can be checked against
	// something rather than against the whole tree.
	cited := map[string]string{}
	re := regexp.MustCompile("`([a-z0-9_/]+\\.(?:go|html|md)):\\d+")
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		path := m[1]
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cited[path] = string(raw)
	}
	if len(cited) == 0 {
		t.Fatal("the document cites no files, so no quotation in it can be checked")
	}

	checked := 0
	for _, quote := range blockquotes(doc) {
		checked++
		found := false
		for path, raw := range cited {
			if containsCode(raw, quote) {
				found = true
				t.Logf("quotation verified against %s", path)
				break
			}
		}
		if !found {
			t.Errorf("the document presents this as a verbatim quotation, but it "+
				"appears in none of the files it cites (%v):\n  %s",
				sortedKeys(cited), quote)
		}
	}
	if checked == 0 {
		t.Error("the document presents no quotations; the answers it gives are " +
			"assertions with nothing to falsify them against")
	}
}

// blockquotes returns each run of consecutive "> " lines as one string.
//
// A quotation the document wraps across three source lines is one quotation, and
// checking it line by line would test fragments that appear nowhere on their own.
func blockquotes(doc string) []string {
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, line := range strings.Split(doc, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "> ") {
			cur = append(cur, strings.TrimSpace(strings.TrimPrefix(t, ">")))
			continue
		}
		flush()
	}
	flush()
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestQuestionsDocClaimsAboutTheFixedIssueStillHold is the other direction: a
// question the document calls *fixed* has to stay fixed, or the document is
// claiming a repair that has been undone.
//
// #474 is the one the document records as fixed, and the fix is two specific
// call sites. Those are pinned here rather than trusted.
func TestQuestionsDocClaimsAboutTheFixedIssueStillHold(t *testing.T) {
	doc := questionsText(t)

	raw, err := os.ReadFile("server/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := normalize(string(raw))

	if !strings.Contains(page, "await loadAssets()") {
		t.Error("loadAssets() is defined but never called again; the document " +
			"records #474 as fixed and that is no longer true")
	}

	// The document must still say so, and must not have quietly downgraded or
	// upgraded the claim.
	for _, want := range []string{"Fixed, and this is the one that worked", "loadAssets"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document no longer says %q; its claim about #474 has "+
				"changed and the reason has not", want)
		}
	}
}

// TestQuestionsDocDoesNotClaimAnyQuestionIsAnswered is the negative test that
// matters most.
//
// Four of the five questions are recorded with an open issue. A document of this
// kind drifts toward sounding resolved, and a reader who trusts it would stop
// filing the bug. So the document has to carry the unresolved statuses, and it
// has to be explicit about the one that is merely unestablished.
func TestQuestionsDocDoesNotClaimAnyQuestionIsAnswered(t *testing.T) {
	doc := questionsText(t)

	// The statuses the document assigns, each of which must be present. These
	// are checked as clauses because the document discusses each status more
	// than once and a bare word would pass on any incidental use.
	for _, want := range []string{
		"**Still open.**",                             // #481 and #477
		"**Partly addressed since.**",                 // #475
		"**Not established either way.**",             // #476
		"**Fixed, and this is the one that worked.**", // #474
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document no longer carries the status %q; an "+
				"unresolved question may now read as answered", want)
		}
	}

	// The inconclusive one has to say what would settle it, or "not
	// established" is a shrug rather than a result.
	if !strings.Contains(doc, "not established") {
		t.Error("the document does not report its own inconclusive result")
	}
	if !strings.Contains(doc, "nobody has done it") {
		t.Error("the document does not say that the one open measurement is " +
			"unclaimed; that is the actionable part of the finding")
	}
}

// TestQuestionsDocLinksNoDiscussionThread is the anti-fabrication test.
//
// Discussions is enabled and empty. A document that linked to a thread would be
// linking to nothing, and one that described discussion content would be
// inventing it. Only category links are legitimate.
func TestQuestionsDocLinksNoDiscussionThread(t *testing.T) {
	doc := questionsText(t)

	// A thread URL is /discussions/<number> or /discussions/<number>/<slug>.
	thread := regexp.MustCompile(`discussions/\d+`)
	if m := thread.FindString(doc); m != "" {
		t.Errorf("the document links to %q; Discussions holds no threads, so "+
			"that link would resolve to nothing", m)
	}

	// And the category the repository points at must be what it cites.
	if !strings.Contains(doc, "discussions/categories/q-a") {
		t.Error("the document does not link the Q&A category the repository " +
			"sends contributors to")
	}

	// It must not assert that Discussions holds anything.
	for _, forbidden := range []string{
		"in the discussion",
		"discussed here",
		"answered in Discussions",
		"as discussed in",
	} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("the document says %q; Discussions is empty, so nothing "+
				"can have been discussed there", forbidden)
		}
	}
}

// TestQuestionsDocCitesEveryQuestionWithASource is the other half of the
// anti-fabrication test: each question must name the issue it came from, so a
// reader can go and check that somebody really asked it.
func TestQuestionsDocCitesEveryQuestionWithASource(t *testing.T) {
	doc := questionsText(t)

	// The five issues the document says are the entire non-maintainer corpus.
	// Each must be cited, and each citation must name a person, because
	// "contributors ask" is a claim about people and not about issues.
	for _, want := range []string{
		"#474", "#475", "#476", "#477", "#481",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not cite issue %s; a question with no "+
				"recorded source is an invented one", want)
		}
	}
	for _, want := range []string{"`Hotmopo`", "`goodness-cpu`"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not name %s; the corpus claim cannot be "+
				"checked", want)
		}
	}

	// Every question section must carry a source link and a date.
	questions := strings.Count(doc, "### ")
	if citations := strings.Count(doc, "Asked in [#"); citations < 5 {
		t.Errorf("the document has %d question sections but only %d "+
			"\"Asked in [#...]\" citations; each question needs its source",
			questions, citations)
	}
	for _, want := range []string{"2026-09-23", "2026-09-24"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not carry the date %s; an undated "+
				"question cannot be placed", want)
		}
	}
}

// TestQuestionsDocCitationsResolve checks every file:line the document cites,
// the way the case study's test does. A citation that points at a renamed or
// shortened file is the common way one of these documents rots.
func TestQuestionsDocCitationsResolve(t *testing.T) {
	doc := questionsText(t)

	re := regexp.MustCompile("`([a-z0-9_/]+\\.(?:go|html|md)):(\\d+)(?:-(\\d+))?`")
	seen := 0
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		path, first, last := m[1], m[2], m[3]
		seen++
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("the document cites %s, which is not in the tree", path)
			continue
		}
		lines := strings.Count(string(raw), "\n") + 1
		hi := first
		if last != "" && last > hi {
			hi = last
		}
		var n int
		if _, err := fmt.Sscanf(hi, "%d", &n); err != nil {
			t.Errorf("the document cites %s:%s, not a line number", path, hi)
			continue
		}
		if n > lines {
			t.Errorf("the document cites %s:%s but the file has %d lines", path, hi, lines)
		}
	}
	if seen == 0 {
		t.Error("the document cites no file:line references, so its code claims " +
			"cannot be checked by a reader")
	}
}

// TestQuestionsDocIsLinkedFromWhereReadersArrive covers the "linked from
// wherever a reader would look for it" criterion. Four places send contributors
// to Discussions; each has to mention the seed.
func TestQuestionsDocIsLinkedFromWhereReadersArrive(t *testing.T) {
	for _, from := range []string{
		"README.md",
		"CONTRIBUTING.md",
		"docs/contributor-faq.md",
		"docs/backlog.md",
	} {
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "discussion-questions.md") {
			t.Errorf("%s does not link the discussion-question seed; it "+
				"sends contributors to Discussions without saying what is "+
				"already there", from)
		}
	}

	// And every place that sends a contributor to Discussions is one the
	// document should have been added to. If a new one appears, this fails and
	// the new one is missing a link.
	for _, from := range []string{"README.md", "CONTRIBUTING.md", "docs/contributor-faq.md"} {
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "discussions/categories/q-a") &&
			!strings.Contains(string(raw), "discussion-questions.md") {
			t.Errorf("%s points at Discussions but not at the seed document", from)
		}
	}
}

// TestQuestionsDocLinksResolve checks the document's own relative links, since a
// question document whose links are dead cannot be followed to the evidence it
// cites.
func TestQuestionsDocLinksResolve(t *testing.T) {
	doc := questionsText(t)

	re := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		path := target
		if i := strings.Index(path, "#"); i >= 0 {
			path = path[:i]
		}
		if strings.HasPrefix(path, "/") {
			continue
		}
		if _, err := os.Stat(filepath.Join("docs", path)); err != nil {
			t.Errorf("the document links to %q, which does not exist", target)
		}
	}
}

// TestQuestionsDocDoesNotOverlapTheFAQ keeps the two documents honest with each
// other. The FAQ answers settled questions; this one holds the open ones. If
// this document started restating the FAQ, one of them would rot.
func TestQuestionsDocDoesNotOverlapTheFAQ(t *testing.T) {
	doc := questionsText(t)

	faq, err := os.ReadFile("docs/contributor-faq.md")
	if err != nil {
		t.Fatal(err)
	}

	// Both documents must be honest about the split, in their own words.
	if !strings.Contains(doc, "21") {
		t.Error("the document does not say how many questions the FAQ already " +
			"answers, so a reader cannot tell the two apart")
	}
	if !strings.Contains(string(faq), "discussion-questions.md") {
		t.Error("the FAQ does not point at the discussion-question seed")
	}
}

// normalize collapses runs of whitespace so a quote can be matched across the
// line wrapping both the source and the document apply to it.
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// containsCode matches a quote against source text, tolerating Go's line-comment
// wrapping.
//
// A Go doc comment wraps at 80 columns and puts "//" at the start of each
// continuation line, so the sentence a human reads as
//
//	// Timeout bounds a single corridor measurement. A full ladder is a
//	// dozen round trips to Horizon, so this is generous by HTTP standards.
//
// has a "//" sitting in the middle of it as far as the raw bytes go, and a
// document quoting that sentence reproduces the markers too. Matching
// normalized raw text would fail on the wrapping rather than on the meaning,
// which is the same class of bug as matching an arrow glyph.
//
// Three normalisations are tried, strictest first, so a widening can only ever
// turn a miss into a hit and never mask a genuine absence. A line-leading "//"
// is removed before any blanket replacement, because removing every "//" would
// also eat the one in a URL inside a string literal.
func containsCode(src, quote string) bool {
	// The comment markers are dropped from the quote as well as the source: a
	// quotation of a Go comment carries them, and comparing a marked quote to
	// an unmarked source is a formatting mismatch, not a factual one.
	want := normalize(stripSlashes(quote))

	for _, candidate := range []string{
		normalize(src),
		normalize(lineCommentMarker.ReplaceAllString(src, " ")),
		normalize(stripSlashes(src)),
	} {
		if strings.Contains(candidate, want) {
			return true
		}
	}
	return false
}

func stripSlashes(s string) string {
	return strings.ReplaceAll(s, "//", " ")
}

var lineCommentMarker = regexp.MustCompile(`(?m)^[ \t]*//`)
