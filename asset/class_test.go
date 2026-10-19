package asset

import "testing"

func TestClassifyNative(t *testing.T) {
	if got := Classify(Native()); got != ClassNative {
		t.Errorf("Classify(Native) = %s, want native", got)
	}
}

func TestClassifySettlement(t *testing.T) {
	if got := Classify(USDC()); got != ClassSettlement {
		t.Errorf("Classify(USDC) = %s, want settlement", got)
	}
}

func TestClassifyUSDCFromWrongIssuerIsNotSettlement(t *testing.T) {
	fake := Stellar("USDC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if got := Classify(fake); got != ClassStellarToken {
		t.Errorf("Classify(fake USDC) = %s, want stellar-token — "+
			"code alone must not confer settlement identity", got)
	}
}

func TestClassifyFiatTokens(t *testing.T) {
	for _, a := range []Asset{NGNC(), GHSC(), KESC()} {
		if got := Classify(a); got != ClassFiatToken {
			t.Errorf("Classify(%s) = %s, want fiat-token", a.Code, got)
		}
	}
}

func TestClassifyUnverifiedTokenIsStellarToken(t *testing.T) {
	other := Stellar("BLND", "GABCDEFGHIJKLMNOPQRSTUVWXYZ234567890ABCDEFGHIJKLMNOP")
	if got := Classify(other); got != ClassStellarToken {
		t.Errorf("Classify(unverified stellar token) = %s, want stellar-token", got)
	}
}

func TestClassifyFiat(t *testing.T) {
	if got := Classify(NGN()); got != ClassFiat {
		t.Errorf("Classify(NGN) = %s, want fiat", got)
	}
}

func TestClassifyZeroValueIsUnknown(t *testing.T) {
	if got := Classify(Asset{}); got != ClassUnknown {
		t.Errorf("Classify(zero) = %s, want unknown", got)
	}
}

func TestClassStringsAreStable(t *testing.T) {
	want := map[Class]string{
		ClassUnknown: "unknown", ClassNative: "native", ClassSettlement: "settlement",
		ClassFiatToken: "fiat-token", ClassStellarToken: "stellar-token", ClassFiat: "fiat",
	}
	for c, s := range want {
		if got := c.String(); got != s {
			t.Errorf("Class(%d).String() = %q, want %q", c, got, s)
		}
	}
}
