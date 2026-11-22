package refrate

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Kind says what a reference mid is: an observation or a definition.
//
// Every provider in this package returns an observation — a figure some
// upstream published about a market at a moment. A same-peg corridor (USDC
// to PYUSD, both tracking USD) has no market rate to observe: USD/USD is 1 by
// definition, and a feed that "answers" 1.0 is echoing its own base currency,
// not measuring anything. Presenting that echo as "via exchangerate-api"
// would make an assumption look like a measurement, which the project's
// layer rule forbids. Kind keeps the two apart on every rate. See
// docs/adr-same-peg-corridors.md.
type Kind int

const (
	// KindObserved is a mid read from a provider. It is the zero value, so
	// every rate built before Kind existed keeps its meaning.
	KindObserved Kind = iota

	// KindDefinitional is a mid that holds by definition — the identity
	// rate between a currency and itself. No provider was consulted.
	KindDefinitional
)

// String renders the kind for the wire: "observed" or "definitional".
func (k Kind) String() string {
	if k == KindDefinitional {
		return "definitional"
	}
	return "observed"
}

// DefinitionalSource is the Source carried by an identity rate. It names no
// provider because none was asked; a stored record carrying it can be
// recognised as definitional without any further field.
const DefinitionalSource = "definitional"

// SamePeg reports whether a reference pair benchmarks a currency against
// itself, i.e. the corridor is a stablecoin swap rather than an FX corridor.
func SamePeg(base, quote string) bool {
	b := strings.ToUpper(strings.TrimSpace(base))
	return b != "" && b == strings.ToUpper(strings.TrimSpace(quote))
}

// Identity returns the definitional reference for a same-peg pair: a mid of
// exactly 1, sourced to no provider.
//
// AsOf and FetchedAt are left zero on purpose. A definition has no moment
// and was never fetched; stamping it with the current time would give it the
// look of a fresh observation, which is the very confusion this type exists
// to prevent. The wire omits both fields when they are zero.
//
// It returns an error for a pair that is not same-peg, so a caller cannot use
// it to manufacture a constant for a real FX pair.
func Identity(base, quote string) (Rate, error) {
	if !SamePeg(base, quote) {
		return Rate{}, fmt.Errorf(
			"refrate: identity reference applies only to a currency against itself, got %s/%s",
			base, quote)
	}
	code := strings.ToUpper(strings.TrimSpace(base))
	return Rate{
		Base:   code,
		Quote:  code,
		Mid:    decimal.NewFromInt(1),
		Source: DefinitionalSource,
		Kind:   KindDefinitional,
		Note: fmt.Sprintf(
			"Same-peg corridor: both assets track %s, so the reference is %s/%s = 1 by definition, "+
				"not an observed market rate, and no provider was consulted. Loss here is deviation "+
				"from par — what the swap costs — not an FX spread.",
			code, code, code),
	}, nil
}

// Definitional reports whether the mid holds by definition rather than
// having been observed.
func (r Rate) Definitional() bool { return r.Kind == KindDefinitional }

// KindOfSource recovers the Kind of a stored reference from its Source, for
// records persisted before (or without) an explicit kind field.
func KindOfSource(source string) Kind {
	if source == DefinitionalSource {
		return KindDefinitional
	}
	return KindObserved
}
