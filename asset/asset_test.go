package asset

import "testing"

// TestSEP38 covers the three identification shapes SEP-38 defines: an issued
// Stellar asset, the documented native special case, and off-chain fiat.
func TestSEP38(t *testing.T) {
	cases := []struct {
		name string
		a    Asset
		want string
	}{
		{"issued", Stellar("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"), "stellar:USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
		{"native", Native(), "stellar:native"},
		{"fiat", Fiat("ngn"), "iso4217:NGN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.SEP38(); got != c.want {
				t.Errorf("SEP38() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestParseSEP38 checks the inverse of SEP38, including that malformed input
// is an error rather than a zero-value asset silently accepted downstream.
func TestParseSEP38(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		want    Asset
		wantErr bool
	}{
		{"issued", "stellar:USDC:GA5Z", Stellar("USDC", "GA5Z"), false},
		{"native", "stellar:native", Native(), false},
		{"fiat", "iso4217:NGN", Fiat("NGN"), false},
		{"garbage", "not-an-asset", Asset{}, true},
		{"wrong segment count", "stellar:USDC", Asset{}, true},
		{"unknown scheme", "foo:USDC:GA5Z", Asset{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSEP38(c.s)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseSEP38(%q) = %v, want an error", c.s, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSEP38(%q): %v", c.s, err)
			}
			if !got.Equal(c.want) {
				t.Errorf("ParseSEP38(%q) = %+v, want %+v", c.s, got, c.want)
			}
		})
	}
}

// TestSEP38RoundTrip pins SEP38 and ParseSEP38 as true inverses for every
// asset shape, since the pair is only safe to use for wire encoding if it is.
func TestSEP38RoundTrip(t *testing.T) {
	for _, a := range []Asset{
		Stellar("NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"),
		Native(),
		Fiat("GHS"),
	} {
		got, err := ParseSEP38(a.SEP38())
		if err != nil {
			t.Fatalf("ParseSEP38(%s.SEP38()): %v", a, err)
		}
		if !got.Equal(a) {
			t.Errorf("round trip: got %+v, want %+v", got, a)
		}
	}
}

// TestHorizonParams covers an issued asset, native XLM (which omits code and
// issuer entirely), and that the prefix argument is actually applied.
func TestHorizonParams(t *testing.T) {
	issued := Stellar("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")

	got := issued.HorizonParams("source")
	want := map[string]string{
		"source_asset_type":   "credit_alphanum4",
		"source_asset_code":   "USDC",
		"source_asset_issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	}
	if len(got) != len(want) {
		t.Fatalf("HorizonParams(issued) = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("HorizonParams(issued)[%q] = %q, want %q", k, got[k], v)
		}
	}

	native := Native().HorizonParams("selling")
	if native["selling_asset_type"] != "native" {
		t.Errorf("HorizonParams(native)[\"selling_asset_type\"] = %q, want \"native\"", native["selling_asset_type"])
	}
	if _, ok := native["selling_asset_code"]; ok {
		t.Error("HorizonParams(native) must not set an asset_code key")
	}
	if _, ok := native["selling_asset_issuer"]; ok {
		t.Error("HorizonParams(native) must not set an asset_issuer key")
	}

	// A 12-char code must select credit_alphanum12, not alphanum4.
	long := Stellar("VERYLONGCODE", "GA5Z").HorizonParams("buying")
	if long["buying_asset_type"] != "credit_alphanum12" {
		t.Errorf("HorizonParams(long code)[\"buying_asset_type\"] = %q, want \"credit_alphanum12\"", long["buying_asset_type"])
	}
}

// TestEqual pins that Stellar assets are compared by full identity —
// code and issuer together — since two tokens can share a code and mean
// entirely different things.
func TestEqual(t *testing.T) {
	a := Stellar("USDC", "GA5Z")
	sameIssuer := Stellar("USDC", "GA5Z")
	differentIssuer := Stellar("USDC", "GBBB")

	if !a.Equal(sameIssuer) {
		t.Error("identical code and issuer must be Equal")
	}
	if a.Equal(differentIssuer) {
		t.Error("same code, different issuer must not be Equal")
	}
	if a.Equal(Fiat("USDC")) {
		t.Error("a Stellar asset must not equal a fiat asset of the same code")
	}
}

// TestIsNative covers the native special case and its near-misses: an issued
// asset happening to be coded "XLM", and fiat.
func TestIsNative(t *testing.T) {
	if !Native().IsNative() {
		t.Error("Native() must report IsNative")
	}
	if Stellar("XLM", "GA5Z").IsNative() {
		t.Error("an issued asset coded XLM with a non-empty issuer must not be native")
	}
	if Fiat("XLM").IsNative() {
		t.Error("fiat coded XLM must not be native")
	}
}

// TestIdentifiable pins which assets carry enough identity to render a
// meaningful SEP38 wire form. An issued Stellar asset with no issuer is the
// case that matters most: SEP38() would still return a string for it
// ("stellar:CODE:"), and that string looks complete without actually naming
// an issuer — exactly the kind of guessed identity a caller must not publish.
func TestIdentifiable(t *testing.T) {
	cases := []struct {
		name string
		a    Asset
		want bool
	}{
		{"issued Stellar asset", USDC(), true},
		{"native XLM", Native(), true},
		{"fiat", Fiat("NGN"), true},
		{"issued Stellar asset with no issuer", Stellar("USDC", ""), false},
		{"zero-value asset", Asset{}, false},
		{"fiat with no code", Asset{Kind: KindFiat}, false},
		// An unrecognised Kind must not fall through to the Stellar case:
		// SEP38()'s own default branch renders one as "stellar:CODE:ISSUER"
		// regardless of Kind, so Identifiable has to refuse it explicitly
		// rather than let a future Kind value be identified as Stellar by
		// accident.
		{"unknown Kind", Asset{Kind: Kind(99), Code: "USDC", Issuer: "GISSUER"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Identifiable(); got != tc.want {
				t.Errorf("Identifiable() = %v, want %v for %+v", got, tc.want, tc.a)
			}
		})
	}
}
