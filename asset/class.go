package asset

// Class is a hop-composition taxonomy used by pathfinding analyses.
//
// The class is not an integrity claim, and it is not a preference: nothing
// in the engine promotes or penalises a class. It exists because a written
// finding about hop composition (issue #101 — native XLM routing) needs
// structured labels, not a string match on asset codes: "USDC" from an
// unrecognised issuer is not the settlement asset, "XLM" is a class not a
// name, and a fiat-pegged token is defined by its verified peg rather than
// by an alphabetic pattern.
type Class int

const (
	// ClassUnknown covers a Stellar asset the registry does not recognise,
	// or a zero-valued Asset. Reported honestly rather than folded into
	// ClassOther, so a hop analysis says how many hops it could not label.
	ClassUnknown Class = iota

	// ClassNative is XLM. Every path that traverses XLM traverses one
	// canonical asset — there is no other native — so recognition is
	// unambiguous.
	ClassNative

	// ClassSettlement is the asset senders start from — USDC from Circle's
	// verified issuer. Distinguished from ClassStellarToken because it is
	// never a hop in this project's own corridors (paths originate at it),
	// and mixing it into the same bucket would understate how many paths
	// route through a fiat-pegged intermediate.
	ClassSettlement

	// ClassFiatToken is a Stellar token verified against its issuer's own
	// stellar.toml as tracking a fiat currency — NGNC, GHSC, KESC.
	// Recognised via FiatPeg so an unverified impostor is not miscounted.
	ClassFiatToken

	// ClassStellarToken is a Stellar token that is neither native, the
	// settlement asset, nor a verified fiat-pegged token — an intermediate
	// bridge asset like BLND, or a token this project has no verified
	// registry entry for.
	ClassStellarToken

	// ClassFiat is off-chain fiat — NGN, GHS, KES. Cannot appear on a
	// pathfinding hop (paths are on-chain), but the type covers it so a
	// caller passing arbitrary assets is not silently dropped into
	// ClassUnknown.
	ClassFiat
)

func (c Class) String() string {
	switch c {
	case ClassNative:
		return "native"
	case ClassSettlement:
		return "settlement"
	case ClassFiatToken:
		return "fiat-token"
	case ClassStellarToken:
		return "stellar-token"
	case ClassFiat:
		return "fiat"
	default:
		return "unknown"
	}
}

// Classify labels an asset for hop-composition analysis.
//
// A Stellar asset is native, then settlement (USDC on the verified issuer),
// then a verified fiat-pegged token, then any other Stellar-issued token.
// A fiat asset is ClassFiat. A zero-valued or unrecognisable asset is
// ClassUnknown — a real state, not a fallback.
func Classify(a Asset) Class {
	switch {
	case a.Kind == KindFiat && a.Code != "":
		return ClassFiat
	case a.Kind == KindStellar && a.IsNative():
		return ClassNative
	case a.Kind == KindStellar && a.Code == "USDC" && a.Issuer == USDCIssuer:
		return ClassSettlement
	case a.Kind == KindStellar && IsFiatToken(a):
		return ClassFiatToken
	case a.Kind == KindStellar && a.Code != "":
		return ClassStellarToken
	default:
		return ClassUnknown
	}
}
