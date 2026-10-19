// Package asset models the assets moving through a remittance corridor.
//
// Two very different things are both "assets" here: Stellar-issued tokens
// (USDC, NGNC) and off-chain fiat (NGN in a bank account). SEP-38 gives us a
// single string form that covers both, and we lean on it rather than inventing
// our own.
package asset

import (
	"fmt"
	"strings"
)

// Kind distinguishes an on-chain token from off-chain fiat. The difference
// matters for routing: a Stellar asset can be moved by a path payment, fiat
// can only be reached by handing off to an anchor.
type Kind int

const (
	KindStellar Kind = iota
	KindFiat
)

// Asset is a corridor endpoint or intermediate hop.
//
// The zero value is not useful; build these with Stellar, Native, or Fiat.
type Asset struct {
	Kind   Kind
	Code   string // "USDC", "NGNC", "XLM", or an ISO-4217 code like "NGN"
	Issuer string // Stellar account ID; empty for native XLM and for fiat
}

// Stellar builds an issued Stellar asset.
func Stellar(code, issuer string) Asset {
	return Asset{Kind: KindStellar, Code: code, Issuer: issuer}
}

// Native is XLM, the bridge asset most corridors route through.
func Native() Asset {
	return Asset{Kind: KindStellar, Code: "XLM"}
}

// Fiat builds an off-chain currency from its ISO-4217 code.
func Fiat(code string) Asset {
	return Asset{Kind: KindFiat, Code: strings.ToUpper(code)}
}

// IsNative reports whether a is XLM.
func (a Asset) IsNative() bool {
	return a.Kind == KindStellar && a.Code == "XLM" && a.Issuer == ""
}

// Identifiable reports whether a carries enough identity to render a
// meaningful SEP38 wire form.
//
// A fiat asset needs its code; native needs nothing further; an issued
// Stellar asset needs both code and issuer — an issuer-less Stellar asset
// is not really identified at all, since anyone can issue a token under any
// code. A caller that renders SEP38() unconditionally, without checking
// this first, risks publishing "stellar:CODE:" for an asset whose issuer
// was never known — a wire form that looks complete but states an identity
// nothing verified.
func (a Asset) Identifiable() bool {
	switch {
	case a.Kind == KindFiat:
		return a.Code != ""
	case a.IsNative():
		return true
	case a.Kind == KindStellar:
		return a.Code != "" && a.Issuer != ""
	default:
		// An unrecognised Kind is not Stellar just because it fell through
		// the cases above. SEP38()'s own default branch would still render
		// one as though it were, so this must refuse it explicitly rather
		// than let that happen silently.
		return false
	}
}

// SEP38 renders the asset in the identification format required by SEP-38,
// which is also what SEP-31 and SEP-6 use for asset fields.
//
// Stellar assets are "stellar:CODE:ISSUER", native is the documented special
// case "stellar:native", and fiat is "iso4217:CODE".
func (a Asset) SEP38() string {
	switch {
	case a.Kind == KindFiat:
		return "iso4217:" + a.Code
	case a.IsNative():
		return "stellar:native"
	default:
		return "stellar:" + a.Code + ":" + a.Issuer
	}
}

// String is the short human form used in CLI output.
func (a Asset) String() string {
	if a.Kind == KindFiat {
		return a.Code
	}
	if a.IsNative() {
		return "XLM"
	}
	if len(a.Issuer) >= 4 {
		return fmt.Sprintf("%s(%s…)", a.Code, a.Issuer[:4])
	}
	return a.Code
}

// Equal compares assets by identity. Two Stellar assets with the same code but
// different issuers are different assets — a distinction that matters, since
// anyone can issue a token called "USDC".
func (a Asset) Equal(b Asset) bool {
	return a.Kind == b.Kind && a.Code == b.Code && a.Issuer == b.Issuer
}

// ParseSEP38 reads the SEP-38 asset identification format.
func ParseSEP38(s string) (Asset, error) {
	parts := strings.Split(s, ":")
	switch {
	case len(parts) == 2 && parts[0] == "iso4217":
		return Fiat(parts[1]), nil
	case len(parts) == 2 && parts[0] == "stellar" && parts[1] == "native":
		return Native(), nil
	case len(parts) == 3 && parts[0] == "stellar":
		return Stellar(parts[1], parts[2]), nil
	}
	return Asset{}, fmt.Errorf("asset: cannot parse %q as a SEP-38 asset", s)
}

// Horizon query parameters -------------------------------------------------

// HorizonType returns the asset_type value Horizon expects.
func (a Asset) HorizonType() string {
	switch {
	case a.IsNative():
		return "native"
	case len(a.Code) <= 4:
		return "credit_alphanum4"
	default:
		return "credit_alphanum12"
	}
}

// HorizonParams returns the query parameters identifying this asset to
// Horizon, prefixed for the given role (e.g. "selling", "buying", "source").
func (a Asset) HorizonParams(prefix string) map[string]string {
	p := map[string]string{prefix + "_asset_type": a.HorizonType()}
	if !a.IsNative() {
		p[prefix+"_asset_code"] = a.Code
		p[prefix+"_asset_issuer"] = a.Issuer
	}
	return p
}
