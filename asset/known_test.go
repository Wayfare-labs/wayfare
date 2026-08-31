package asset

import (
	"reflect"
	"testing"
)

// TestLookup covers case-insensitive resolution of a verified code and that
// an unrecognised code is reported as absent rather than guessed at.
func TestLookup(t *testing.T) {
	cases := []struct {
		code string
		want Asset
		ok   bool
	}{
		{"USDC", USDC(), true},
		{"usdc", USDC(), true},
		{" NgNc ", NGNC(), true},
		{"NOTREAL", Asset{}, false},
		{"", Asset{}, false},
	}
	for _, c := range cases {
		got, ok := Lookup(c.code)
		if ok != c.ok {
			t.Errorf("Lookup(%q) ok = %v, want %v", c.code, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("Lookup(%q) = %+v, want %+v", c.code, got, c.want)
		}
	}
}

// TestKnownCodes pins that the list is sorted, since callers render it
// directly (e.g. in CLI help output) and an unsorted map iteration order
// would make that output nondeterministic.
func TestKnownCodes(t *testing.T) {
	got := KnownCodes()
	want := []string{"EURMTL", "GHSC", "KESC", "NGNC", "NGNT", "PYUSD", "USDC", "USDZ", "ZARZ"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KnownCodes() = %v, want %v", got, want)
	}
}

// TestFiatPeg covers a registered token, a correct code from the wrong
// issuer, and native XLM — the three cases the doc comment on FiatPeg
// promises not to guess on.
func TestFiatPeg(t *testing.T) {
	if peg, ok := FiatPeg(NGNC()); !ok || peg != "NGN" {
		t.Errorf("FiatPeg(NGNC()) = (%q, %v), want (\"NGN\", true)", peg, ok)
	}
	if peg, ok := FiatPeg(GHSC()); !ok || peg != "GHS" {
		t.Errorf("FiatPeg(GHSC()) = (%q, %v), want (\"GHS\", true)", peg, ok)
	}
	if peg, ok := FiatPeg(KESC()); !ok || peg != "KES" {
		t.Errorf("FiatPeg(KESC()) = (%q, %v), want (\"KES\", true)", peg, ok)
	}

	// The expanded registry: every entry verified 2026-08-26 from the
	// issuer's own stellar.toml.
	for _, c := range []struct {
		a   Asset
		peg string
	}{
		{NGNT(), "NGN"},
		{USDZ(), "USD"},
		{ZARZ(), "ZAR"},
		{EURMTL(), "EUR"},
		{PYUSD(), "USD"},
	} {
		if peg, ok := FiatPeg(c.a); !ok || peg != c.peg {
			t.Errorf("FiatPeg(%s) = (%q, %v), want (%q, true)", c.a, peg, ok, c.peg)
		}
	}

	impostor := Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5")
	if _, ok := FiatPeg(impostor); ok {
		t.Error("FiatPeg must return false for the right code from the wrong issuer")
	}

	if _, ok := FiatPeg(Native()); ok {
		t.Error("FiatPeg must return false for native XLM, a bridge asset")
	}

	if _, ok := FiatPeg(USDC()); ok {
		t.Error("FiatPeg must return false for a verified token with no registered peg")
	}
}

// TestClassifyHop pins the three-way hop classification that route.classify
// relies on: fiat-pegged tokens are dependencies, native XLM and registered
// non-fiat tokens are bridges, and everything else is unknown — including an
// unregistered token whose code matches a registered fiat token.
func TestClassifyHop(t *testing.T) {
	cases := []struct {
		name string
		a    Asset
		want HopKind
	}{
		{"NGNC is a fiat dependency", NGNC(), HopFiat},
		{"the expanded registry is fiat", USDZ(), HopFiat},
		{"PYUSD is fiat", PYUSD(), HopFiat},
		{"native XLM is a bridge", Native(), HopBridge},
		{"USDC is a registered non-fiat bridge", USDC(), HopBridge},
		{"an unregistered token is unknown", Stellar("BLND", "GBLNDISS1234567890123456789012345678901234567890123456789"), HopUnknown},
		{"an impostor with a registered code is unknown", Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5"), HopUnknown},
		{"off-chain fiat is unknown as a hop", NGN(), HopUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyHop(tc.a); got != tc.want {
				t.Errorf("ClassifyHop(%s) = %v, want %v", tc.a, got, tc.want)
			}
		})
	}
}

// TestClassifyHopString keeps the human forms stable for readers.
func TestClassifyHopString(t *testing.T) {
	cases := map[HopKind]string{
		HopFiat:    "fiat",
		HopBridge:  "bridge",
		HopUnknown: "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("HopKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

// TestIsFiatToken exercises the same cases through the boolean-only helper,
// since callers on the hot classification path use this form directly.
func TestIsFiatToken(t *testing.T) {
	for _, a := range []Asset{NGNC(), GHSC(), KESC()} {
		if !IsFiatToken(a) {
			t.Errorf("IsFiatToken(%s) = false, want true", a)
		}
	}

	impostor := Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5")
	if IsFiatToken(impostor) {
		t.Error("IsFiatToken must return false for an unregistered issuer")
	}
	if IsFiatToken(Native()) {
		t.Error("IsFiatToken must return false for XLM, a bridge asset")
	}
}

// TestRegistryCompleteness ensures that every registered entry is valid,
// and that every corridor destination token has a non-empty fiat peg, SEP-1
// status, verification date, source URL, and home domain.
func TestRegistryCompleteness(t *testing.T) {
	entries := Registry()
	if len(entries) == 0 {
		t.Fatal("Registry() returned no entries")
	}

	for _, e := range entries {
		if err := ValidateEntry(e); err != nil {
			t.Errorf("ValidateEntry(%+v) failed: %v", e, err)
		}

		a, ok := Lookup(e.Code)
		if !ok {
			t.Errorf("Lookup(%q) returned not found", e.Code)
		}
		if a.Issuer != e.Issuer {
			t.Errorf("Lookup(%q).Issuer = %q, want %q", e.Code, a.Issuer, e.Issuer)
		}

		if e.Code != "USDC" {
			if e.Peg == "" {
				t.Errorf("corridor entry %q must have a non-empty fiat peg", e.Code)
			}
			if e.Status == "" {
				t.Errorf("corridor entry %q must have a non-empty SEP-1 status", e.Code)
			}
			if e.VerificationDate == "" {
				t.Errorf("corridor entry %q must have a non-empty verification date", e.Code)
			}
			if e.SourceURL == "" {
				t.Errorf("corridor entry %q must have a non-empty source URL", e.Code)
			}
			if e.HomeDomain == "" {
				t.Errorf("corridor entry %q must have a non-empty home domain", e.Code)
			}

			peg, ok := FiatPeg(a)
			if !ok || peg != e.Peg {
				t.Errorf("FiatPeg(%s) = (%q, %v), want (%q, true)", a, peg, ok, e.Peg)
			}

			domain, ok := HomeDomain(a)
			if !ok || domain != e.HomeDomain {
				t.Errorf("HomeDomain(%s) = (%q, %v), want (%q, true)", a, domain, ok, e.HomeDomain)
			}
		}
	}
}

// TestHalfRegisteredEntryFails tests that ValidateEntry fails loudly when
// any required field is missing from a registration entry, preventing
// silent misclassification of corridor assets.
func TestHalfRegisteredEntryFails(t *testing.T) {
	validCorridor := Entry{
		Code:             "TESTC",
		Issuer:           LinkIOIssuer,
		Peg:              "TST",
		Status:           "live",
		VerificationDate: "2026-08-08",
		SourceURL:        "https://example.com/.well-known/stellar.toml",
		HomeDomain:       "example.com",
	}

	cases := []struct {
		name      string
		mutate    func(e Entry) Entry
		wantError string
	}{
		{
			name: "missing code",
			mutate: func(e Entry) Entry {
				e.Code = ""
				return e
			},
			wantError: "asset code is required",
		},
		{
			name: "missing issuer",
			mutate: func(e Entry) Entry {
				e.Issuer = ""
				return e
			},
			wantError: "issuer is required",
		},
		{
			name: "missing peg on corridor token",
			mutate: func(e Entry) Entry {
				e.Peg = ""
				return e
			},
			wantError: "fiat peg is required",
		},
		{
			name: "missing status on corridor token",
			mutate: func(e Entry) Entry {
				e.Status = ""
				return e
			},
			wantError: "SEP-1 status is required",
		},
		{
			name: "missing verification date on corridor token",
			mutate: func(e Entry) Entry {
				e.VerificationDate = ""
				return e
			},
			wantError: "verification date is required",
		},
		{
			name: "missing source URL on corridor token",
			mutate: func(e Entry) Entry {
				e.SourceURL = ""
				return e
			},
			wantError: "source URL is required",
		},
		{
			name: "missing home domain on corridor token",
			mutate: func(e Entry) Entry {
				e.HomeDomain = ""
				return e
			},
			wantError: "home domain is required",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := c.mutate(validCorridor)
			err := ValidateEntry(entry)
			if err == nil {
				t.Fatalf("ValidateEntry(%+v) succeeded, want error containing %q", entry, c.wantError)
			}
		})
	}
}

// TestLookupEntry verifies looking up full registration metadata by Asset and by Code.
func TestLookupEntry(t *testing.T) {
	entry, ok := LookupEntry(NGNC())
	if !ok {
		t.Fatal("LookupEntry(NGNC()) returned false")
	}
	if entry.Code != "NGNC" || entry.Peg != "NGN" || entry.Status != "live" || entry.HomeDomain != "ngnc.online" {
		t.Errorf("LookupEntry(NGNC()) = %+v, unexpected fields", entry)
	}

	entryByCode, ok := LookupEntryByCode("ghsc")
	if !ok {
		t.Fatal("LookupEntryByCode(\"ghsc\") returned false")
	}
	if entryByCode.Code != "GHSC" || entryByCode.Peg != "GHS" || entryByCode.Status != "pending" {
		t.Errorf("LookupEntryByCode(\"ghsc\") = %+v, unexpected fields", entryByCode)
	}

	usdcEntry, ok := LookupEntry(USDC())
	if !ok {
		t.Fatal("LookupEntry(USDC()) returned false")
	}
	if usdcEntry.Code != "USDC" || usdcEntry.Peg != "" || usdcEntry.Status != "unverified" {
		t.Errorf("LookupEntry(USDC()) = %+v, unexpected fields", usdcEntry)
	}

	if _, ok := LookupEntryByCode("UNKNOWN"); ok {
		t.Error("LookupEntryByCode(\"UNKNOWN\") must return false")
	}
	if _, ok := LookupEntry(Native()); ok {
		t.Error("LookupEntry(Native()) must return false")
	}
	if _, ok := LookupEntry(Fiat("NGN")); ok {
		t.Error("LookupEntry(Fiat(\"NGN\")) must return false")
	}
}
